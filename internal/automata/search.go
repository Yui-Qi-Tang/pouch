package automata

import (
	"context"
	"errors"
	"fmt"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/pouch/internal/planning"
)

// Trace is a run as bit strings. Choices has one entry per step, holding every
// cell's choice bits in cell order; it is empty text for choice-free systems.
type Trace struct {
	States  []string `json:"states"`
	Choices []string `json:"choices"`
}

// Options caps work without changing the declared query. Zero selects a default.
type Options struct {
	MaxQueries    int `json:"max_queries"`
	MaxVariables  int `json:"max_variables"`
	MaxClauses    int `json:"max_clauses"`
	MaxLiterals   int `json:"max_literals"`
	MaxOrbitSteps int `json:"max_orbit_steps"`
}

func (o Options) defaults() (Options, error) {
	if o.MaxQueries < 0 || o.MaxVariables < 0 || o.MaxClauses < 0 || o.MaxLiterals < 0 || o.MaxOrbitSteps < 0 {
		return o, errors.New("negative search budget")
	}
	if o.MaxQueries == 0 {
		o.MaxQueries = 10000
	}
	if o.MaxVariables == 0 {
		o.MaxVariables = 100000
	}
	if o.MaxClauses == 0 {
		o.MaxClauses = 1000000
	}
	if o.MaxLiterals == 0 {
		o.MaxLiterals = 4000000
	}
	if o.MaxOrbitSteps == 0 {
		o.MaxOrbitSteps = 100000
	}
	return o, nil
}

// Return is a separate shortest search from the forward endpoint to the baseline.
type Return struct {
	// UNKNOWN, RETURN_FOUND, or NO_RETURN_WITHIN_BOUND.
	Status   string   `json:"status"`
	Trace    *Trace   `json:"trace"`
	QueryIDs []string `json:"query_ids"`
}

// Result keeps solver evidence on interruption; Complete never survives an error.
type Result struct {
	// pouch-automaton-search/v1.
	SchemaVersion string  `json:"schema_version"`
	Policy        string  `json:"policy"`
	Options       Options `json:"options"`
	ForwardLimit  int     `json:"forward_limit"`
	ReturnLimit   int     `json:"return_limit"`
	// A deterministic system has one run, so Forward is the only path. With
	// choices, Forward is one shortest witness and others may exist.
	Deterministic bool `json:"deterministic"`
	// Nil means no run reached the goal within the checked lengths.
	Forward *Trace  `json:"forward"`
	Return  *Return `json:"return"`
	// Attempted solver calls, including failed calls.
	Queries []planning.Query `json:"queries"`
	// Every shorter length, or every allowed length, ended in verified UNSAT.
	ForwardComplete bool `json:"forward_complete"`
	Complete        bool `json:"complete"`
	// FOUND, NO_PATH_WITHIN_BOUND, or INCONCLUSIVE.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Search finds the shortest run from the start to the goal, then the shortest run
// from its endpoint to the baseline. Shortest implies first hit: an earlier hit
// would be a shorter run. It uses no simulation or recorded answer as input.
func Search(ctx context.Context, a *Authority, solver planning.Solver, options Options) (result Result, err error) {
	result = Result{SchemaVersion: "pouch-automaton-search/v1", Policy: "shortest-run/" + automaton.Profile, Status: "INCONCLUSIVE", ForwardLimit: a.contract.ForwardLimit, ReturnLimit: a.contract.ReturnLimit, Deterministic: a.system.totalChoices == 0, Queries: []planning.Query{}}
	defer func() {
		if err != nil {
			result.Complete = false
			result.Status = "INCONCLUSIVE"
			result.Error = err.Error()
		}
	}()
	opts, err := options.defaults()
	if err != nil {
		return result, err
	}
	result.Options = opts
	if solver == nil {
		return result, errors.New("missing solver")
	}
	solve := func(direction string, h int, start automaton.State, goal automaton.Pattern) (*Trace, string, error) {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		if len(result.Queries) >= opts.MaxQueries {
			return nil, "", planning.ErrQueryLimit
		}
		enc, err := automaton.Compile(ctx, a.system, automaton.Query{Steps: h, Start: a.system.pattern(start), Goal: goal}, automaton.WithMaxVariables(opts.MaxVariables), automaton.WithMaxClauses(opts.MaxClauses), automaton.WithMaxLiterals(opts.MaxLiterals))
		if errors.Is(err, automaton.ErrEncodingLimit) {
			return nil, "", planning.ErrEncodingLimit
		}
		if err != nil {
			return nil, "", err
		}
		formula, err := enc.CNF()
		if err != nil {
			return nil, "", err
		}
		id := fmt.Sprintf("q%06d", len(result.Queries))
		response, err := solver.Solve(ctx, id, formula)
		q := planning.Query{ID: id, Direction: direction, Horizon: h, Status: response.Status, Verified: response.Verified, Artifacts: response.Artifacts}
		if err != nil {
			q.Error = err.Error()
		}
		result.Queries = append(result.Queries, q)
		if err != nil {
			return nil, id, err
		}
		if !response.Verified || (response.Status != "SAT" && response.Status != "UNSAT") {
			return nil, id, errors.New("solver result lacks verified status")
		}
		if response.Status == "UNSAT" {
			return nil, id, nil
		}
		trajectory, err := enc.Decode(response.Assignment)
		if err != nil {
			return nil, id, err
		}
		trace := encodeTrace(trajectory)
		return &trace, id, nil
	}
	for h := 0; h <= a.contract.ForwardLimit && result.Forward == nil; h++ {
		if result.Forward, _, err = solve("forward", h, a.start, a.goal); err != nil {
			return result, err
		}
	}
	result.ForwardComplete = true
	if result.Forward == nil {
		result.Complete = true
		result.Status = "NO_PATH_WITHIN_BOUND"
		return result, nil
	}
	endpoint, ok := decodeBits(result.Forward.States[len(result.Forward.States)-1], len(a.start))
	if !ok {
		return result, errors.New("decoded endpoint has the wrong size")
	}
	result.Return = &Return{Status: "UNKNOWN", QueryIDs: []string{}}
	baseline := a.system.pattern(a.baseline)
	for h := 0; h <= a.contract.ReturnLimit && result.Return.Trace == nil; h++ {
		trace, id, err := solve("return", h, endpoint, baseline)
		if id != "" {
			result.Return.QueryIDs = append(result.Return.QueryIDs, id)
		}
		if err != nil {
			return result, err
		}
		result.Return.Trace = trace
	}
	result.Return.Status = "NO_RETURN_WITHIN_BOUND"
	if result.Return.Trace != nil {
		result.Return.Status = "RETURN_FOUND"
	}
	result.Complete = true
	result.Status = "FOUND"
	return result, nil
}

func (s *system) pattern(state automaton.State) automaton.Pattern {
	p := make(automaton.Pattern, len(state))
	for i, set := range state {
		p[automaton.Bit{Cell: i / s.bits, Index: i % s.bits}] = set
	}
	return p
}

func encodeTrace(t automaton.Trajectory) Trace {
	out := Trace{States: make([]string, len(t.States)), Choices: make([]string, len(t.Choices))}
	for i, state := range t.States {
		out.States[i] = encodeBits(state)
	}
	for i, choices := range t.Choices {
		out.Choices[i] = encodeBits(choices)
	}
	return out
}

func (a *Authority) decodeTrace(t Trace) (automaton.Trajectory, error) {
	out := automaton.Trajectory{States: make([]automaton.State, len(t.States)), Choices: make([][]bool, len(t.Choices))}
	for i, text := range t.States {
		state, ok := decodeBits(text, len(a.start))
		if !ok {
			return out, reject("TRAJECTORY_SHAPE")
		}
		out.States[i] = state
	}
	for i, text := range t.Choices {
		choices, ok := decodeBits(text, a.system.totalChoices)
		if !ok {
			return out, reject("TRAJECTORY_SHAPE")
		}
		out.Choices[i] = choices
	}
	return out, nil
}
