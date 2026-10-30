package automata

import (
	"context"
	"errors"
	"slices"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
)

// BundleVersion identifies the automaton result schema.
const BundleVersion = "pouch-automaton-bundle/v1"

// Bundle records a search and its checks against the authority's rule tables.
// Its status is a certification outcome, distinct from Search.Status.
type Bundle struct {
	SchemaVersion   string `json:"schema_version"`
	ProjectVersion  string `json:"project_version"`
	RequestID       string `json:"request_id"`
	AuthoritySHA256 string `json:"authority_sha256"`
	// FOUND, NO_PATH_WITHIN_BOUND, NO_PATH_IN_MODEL, INCONCLUSIVE, or REJECTED.
	Status string `json:"status"`
	// Search and forward and recovery certification finished.
	Complete bool `json:"complete"`
	// model_conditional_conclusion.
	ConclusionKind string             `json:"conclusion_kind"`
	ObservedFact   bool               `json:"observed_fact"`
	Sources        run.SourceManifest `json:"sources"`
	// State bits per cell, and how to lay out cells.
	Bits            int     `json:"bits"`
	Display         Display `json:"display"`
	Search          Result  `json:"search"`
	ForwardVerified bool    `json:"forward_verified"`
	// RETURN_VERIFIED, NO_RETURN_IN_MODEL, or UNKNOWN.
	RecoveryStatus string `json:"recovery_status"`
	// Steps from the endpoint to the baseline, when a run established them. It
	// can exceed the return limit; see Reason.
	ReturnSteps *int `json:"return_steps"`
	// Unknown-recovery explanation.
	Reason      string   `json:"reason,omitempty"`
	Diagnostics []string `json:"diagnostics"`
}

// Certify checks the search result against the rule tables, without the CNF. For
// a choice-free system it also walks the only run, which independently confirms
// every UNSAT answer the shortest claims depend on, and can prove there is no
// return when the run closes without reaching the baseline.
func Certify(ctx context.Context, a *Authority, digest string, sources run.SourceManifest, result Result) (Bundle, error) {
	b := Bundle{SchemaVersion: BundleVersion, ProjectVersion: "0.1.0-dev", RequestID: a.RequestID(), AuthoritySHA256: digest, Status: "INCONCLUSIVE", ConclusionKind: "model_conditional_conclusion", Sources: sources, Bits: a.system.bits, Display: a.display, Search: result, RecoveryStatus: "UNKNOWN", Diagnostics: []string{}}
	refuse := func(err error) (Bundle, error) {
		b.Status = "REJECTED"
		return b, err
	}
	meets := func(state automaton.State) bool {
		for bit, value := range a.goal {
			if state[bit.Cell*a.system.bits+bit.Index] != value {
				return false
			}
		}
		return true
	}
	if result.ForwardLimit != a.contract.ForwardLimit || result.ReturnLimit != a.contract.ReturnLimit || result.Deterministic != (a.system.totalChoices == 0) {
		return refuse(errors.New("search does not match the authority"))
	}
	limit := result.Options.MaxOrbitSteps
	if result.Forward == nil {
		if !result.ForwardComplete {
			b.Diagnostics = append(b.Diagnostics, "RUN_NOT_FULLY_VERIFIED")
			return b, nil
		}
		if err := result.checkLedger("forward", result.ForwardLimit, false); err != nil {
			return refuse(err)
		}
		b.Status = "NO_PATH_WITHIN_BOUND"
		if !result.Deterministic {
			b.Complete = result.Complete
			return b, nil
		}
		hit, closed, err := a.system.walk(ctx, a.start, meets, limit)
		switch {
		case err != nil:
			b.Status = "INCONCLUSIVE"
			return b, err
		case hit >= 0 && hit <= result.ForwardLimit:
			return refuse(errors.New("false forward exhaustion"))
		case closed:
			b.Status = "NO_PATH_IN_MODEL"
		}
		b.Complete = result.Complete
		return b, nil
	}
	forward, err := a.decodeTrace(*result.Forward)
	if err != nil {
		return refuse(err)
	}
	if err := a.Replay(forward); err != nil {
		return refuse(err)
	}
	endpoint := forward.States[len(forward.States)-1]
	if !slices.Equal(forward.States[0], a.start) || !meets(endpoint) {
		return refuse(errors.New("forward run misses start or goal"))
	}
	if err := result.checkLedger("forward", len(forward.Choices), true); err != nil {
		return refuse(err)
	}
	if result.Deterministic {
		hit, _, err := a.system.walk(ctx, a.start, meets, len(forward.Choices))
		if err != nil {
			return b, err
		}
		if hit != len(forward.Choices) {
			return refuse(errors.New("false shortest forward claim"))
		}
	}
	b.ForwardVerified = true
	b.Status = "FOUND"
	reached := func(state automaton.State) bool { return slices.Equal(state, a.baseline) }
	switch {
	case result.Return == nil || result.Return.Status == "UNKNOWN":
		b.Reason = "RETURN_SEARCH_INCOMPLETE"
	case result.Return.Status == "RETURN_FOUND":
		if result.Return.Trace == nil {
			return refuse(errors.New("return witness missing"))
		}
		back, err := a.decodeTrace(*result.Return.Trace)
		if err != nil {
			return refuse(err)
		}
		if err := a.Replay(back); err != nil {
			return refuse(err)
		}
		if !slices.Equal(back.States[0], endpoint) || !reached(back.States[len(back.States)-1]) {
			return refuse(errors.New("return run misses endpoint or baseline"))
		}
		cost := len(back.Choices)
		if err := result.checkLedger("return", cost, true); err != nil {
			return refuse(err)
		}
		if result.Deterministic {
			hit, _, err := a.system.walk(ctx, endpoint, reached, cost)
			if err != nil {
				return b, err
			}
			if hit != cost {
				return refuse(errors.New("false shortest return claim"))
			}
		}
		b.RecoveryStatus, b.ReturnSteps = "RETURN_VERIFIED", &cost
	case !result.Deterministic:
		if err := result.checkLedger("return", result.ReturnLimit, false); err != nil {
			return refuse(err)
		}
		b.Reason = "CHOICES_NOT_ENUMERATED"
	default:
		if err := result.checkLedger("return", result.ReturnLimit, false); err != nil {
			return refuse(err)
		}
		hit, closed, err := a.system.walk(ctx, endpoint, reached, limit)
		switch {
		case err != nil:
			return b, err
		case hit >= 0 && hit <= result.ReturnLimit:
			return refuse(errors.New("false return exhaustion"))
		case hit >= 0:
			b.ReturnSteps, b.Reason = &hit, "RETURN_EXISTS_BEYOND_SEARCH_BOUND"
		case closed:
			b.RecoveryStatus = "NO_RETURN_IN_MODEL"
		default:
			b.Reason = "ORBIT_NOT_CLOSED"
		}
	}
	b.Complete = result.Complete && b.RecoveryStatus != "UNKNOWN"
	if !b.Complete {
		b.Diagnostics = append(b.Diagnostics, "RUN_NOT_FULLY_VERIFIED")
	}
	return b, nil
}

// checkLedger requires the recorded queries of one direction to support a claim:
// verified UNSAT at every length before last, then a verified answer at last,
// SAT when found and UNSAT otherwise. A system with choices has no walk, so this
// is what ties its shortest claims to the solver's checked answers.
func (r Result) checkLedger(direction string, last int, found bool) error {
	horizon := 0
	for _, q := range r.Queries {
		if q.Direction != direction {
			continue
		}
		want := "UNSAT"
		if found && horizon == last {
			want = "SAT"
		}
		if q.Horizon != horizon || horizon > last || q.Status != want || !q.Verified {
			return errors.New("query ledger does not support the claim")
		}
		horizon++
	}
	if horizon != last+1 {
		return errors.New("query ledger does not support the claim")
	}
	return nil
}
