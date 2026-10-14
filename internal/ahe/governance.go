package ahe

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

const CurrentActionPolicy = "current-action/v1"
const GroundedActionPolicy = "grounded-current-action/v1"
const BasisVersion = "pouch-ahe-basis/v1"

var ErrGovernanceRejected = errors.New("GOVERNANCE_REJECTED")
var ErrGovernanceUnavailable = errors.New("GOVERNANCE_UNAVAILABLE")

// ActionBasis declares premises; checking it does not prove semantic entailment.
type ActionBasis struct {
	ActionID string   `json:"action_id"`
	BasisIDs []string `json:"basis_ids"`
}

// BasisManifest is separate caller-owned authority for the optional AHE workflow.
// Policy is explicit in these pinned bytes; only the pre-model CLI defaults it.
type BasisManifest struct {
	SchemaVersion   string        `json:"schema_version"`
	AuthoritySHA256 string        `json:"authority_sha256"`
	RequestID       string        `json:"request_id"`
	UsePolicy       string        `json:"use_policy"`
	Actions         []ActionBasis `json:"actions"`
}

// Basis retains an immutable, exact-byte-bound manifest and validated authority.
type Basis struct {
	manifest  BasisManifest
	raw       []byte
	digest    string
	authority *validation.Authority
}

// NewBasis requires one explicit mapping for every model action, including
// recovery actions. It never guesses that all sources support every action.
func NewBasis(authorityRaw []byte, authorityHash, requestID string, raw []byte, hash string) (*Basis, error) {
	a, err := validation.NewAuthority(authorityRaw, authorityHash)
	if err != nil {
		return nil, err
	}
	if len(raw) > validation.MaxBytes || !validDigest(hash) || validation.Digest(raw) != hash {
		return nil, errors.New("action-basis exact byte digest mismatch")
	}
	var m BasisManifest
	if err := decodeGovernance(raw, &m, true); err != nil {
		return nil, err
	}
	c := a.Contract()
	if m.SchemaVersion != BasisVersion || m.AuthoritySHA256 != authorityHash || m.RequestID != requestID || requestID != c.RequestID || !validPolicy(m.UsePolicy) {
		return nil, errors.New("action-basis authority, request or policy mismatch")
	}
	if _, err := canonicalIDs(a.SourceIDs()); err != nil {
		return nil, err
	}
	model := map[string]bool{}
	for _, action := range c.Model.Actions {
		model[action.ID] = true
	}
	if len(m.Actions) != len(model) {
		return nil, errors.New("action-basis must cover every model action")
	}
	for i, action := range m.Actions {
		if !model[action.ActionID] {
			return nil, errors.New("duplicate or unknown action-basis action")
		}
		delete(model, action.ActionID)
		ids, err := canonicalIDs(action.BasisIDs)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, ok := a.SourceDigest(id); !ok {
				return nil, errors.New("action basis outside authority sources")
			}
		}
		m.Actions[i].BasisIDs = ids
	}
	slices.SortFunc(m.Actions, func(a, b ActionBasis) int { return strings.Compare(a.ActionID, b.ActionID) })
	return &Basis{manifest: m, raw: bytes.Clone(raw), digest: hash, authority: a}, nil
}

func (b *Basis) Raw() []byte    { return bytes.Clone(b.raw) }
func (b *Basis) SHA256() string { return b.digest }
func (b *Basis) Policy() string { return b.manifest.UsePolicy }

// GovernanceCheck retains the authoritative response, including native reasons.
// It is a historical read, never permission for future execution or admission.
type GovernanceCheck struct {
	Status         string          `json:"status"`
	Failure        string          `json:"failure,omitempty"`
	PackageSHA256  string          `json:"package_sha256,omitempty"`
	Tool           string          `json:"tool"`
	Policy         string          `json:"policy"`
	BasisSHA256    string          `json:"basis_sha256,omitempty"`
	Raw            []byte          `json:"raw_response_base64,omitempty"`
	Assessment     json.RawMessage `json:"assessment,omitempty"`
	ResponseSHA256 string          `json:"response_sha256,omitempty"`
}

type useAssessment struct {
	ProjectionVersion string    `json:"projection_version"`
	StateDigest       string    `json:"state_digest"`
	Policy            string    `json:"policy"`
	EvaluatedAt       time.Time `json:"evaluated_at"`
	Roots             []string  `json:"roots"`
	Nodes             []struct {
		NodeID  string `json:"node_id"`
		Outcome string `json:"outcome"`
	} `json:"nodes"`
}

func validPolicy(p string) bool { return p == CurrentActionPolicy || p == GroundedActionPolicy }
func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func canonicalIDs(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > 32 {
		return nil, errors.New("1..32 canonical basis IDs required")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	for i, id := range ids {
		if !strings.HasPrefix(id, "canon-node:") || len(id) <= len("canon-node:") || len(id) > 200 || strings.TrimSpace(id) != id || (i > 0 && id == ids[i-1]) {
			return nil, errors.New("exact distinct canonical basis IDs required")
		}
	}
	return ids, nil
}

func (a useAssessment) eligible(policy string, roots []string) (bool, error) {
	if a.ProjectionVersion != "evidence-use-projection/v1" || a.Policy != policy || a.EvaluatedAt.IsZero() || !strings.HasPrefix(a.StateDigest, "evidence-use:sha256:") || !validDigest(strings.TrimPrefix(a.StateDigest, "evidence-use:sha256:")) {
		return false, errors.New("invalid governance assessment identity")
	}
	got, err := canonicalIDs(a.Roots)
	if err != nil || !slices.Equal(got, roots) {
		return false, errors.New("governance roots mismatch")
	}
	seen := map[string]bool{}
	eligible := true
	for _, n := range a.Nodes {
		if n.NodeID == "" || seen[n.NodeID] {
			return false, errors.New("duplicate or empty assessed node")
		}
		seen[n.NodeID] = true
		switch n.Outcome {
		case "eligible":
		case "blocked", "excluded":
			eligible = false
		default:
			return false, errors.New("unknown governance outcome")
		}
	}
	for _, id := range roots {
		if !seen[id] {
			return false, errors.New("missing assessed root")
		}
	}
	return eligible, nil
}

func checkFailure(c GovernanceCheck, err error) (GovernanceCheck, error) {
	c.Status = "GOVERNANCE_UNAVAILABLE"
	c.Failure = err.Error()
	var native *ToolError
	if errors.As(err, &native) {
		c.Raw = bytes.Clone(native.Payload)
		c.ResponseSHA256 = validation.Digest(c.Raw)
	}
	return c, fmt.Errorf("%w: %w", ErrGovernanceUnavailable, err)
}
func finishCheck(c GovernanceCheck, accepted bool) (GovernanceCheck, error) {
	if !accepted {
		c.Status = "GOVERNANCE_REJECTED"
		return c, ErrGovernanceRejected
	}
	c.Status = "GOVERNANCE_ACCEPTED"
	return c, nil
}

// CheckSources can run before model construction. No cached result is accepted.
func CheckSources(ctx context.Context, query *Profile, policy string, ids []string) (GovernanceCheck, error) {
	c := GovernanceCheck{Tool: "get_evidence_use_state", Policy: policy}
	roots, err := canonicalIDs(ids)
	if err != nil {
		return checkFailure(c, err)
	}
	if !validPolicy(policy) || query == nil || query.kind != ProfileQuery {
		return checkFailure(c, ErrCapabilityUnavailable)
	}
	raw, err := query.call(ctx, c.Tool, map[string]any{"canonical_ids": roots, "policy": policy})
	if err != nil {
		return checkFailure(c, err)
	}
	c.Raw, c.ResponseSHA256 = bytes.Clone(raw), validation.Digest(raw)
	var native map[string]json.RawMessage
	if json.Unmarshal(raw, &native) == nil {
		c.Assessment = bytes.Clone(native["assessment"])
	}
	var response struct {
		Assessment useAssessment `json:"assessment"`
	}
	if err := decodeGovernance(raw, &response, false); err != nil {
		return checkFailure(c, err)
	}
	ok, err := response.Assessment.eligible(policy, roots)
	if err != nil {
		return checkFailure(c, err)
	}
	return finishCheck(c, ok)
}

// Preflight checks every authority source and its exact statement before search.
func (b *Basis) Preflight(ctx context.Context, query *Profile) (GovernanceCheck, error) {
	c := GovernanceCheck{BasisSHA256: b.digest, Policy: b.Policy(), Tool: "get_evidence_use_state"}
	for _, id := range b.authority.SourceIDs() {
		r, err := ReadRecord(ctx, query, id)
		if err != nil {
			return checkFailure(c, err)
		}
		want, _ := b.authority.SourceDigest(id)
		if r.StatementSHA256 != want {
			return checkFailure(c, errors.New("source statement binding mismatch"))
		}
	}
	c, err := CheckSources(ctx, query, b.Policy(), b.authority.SourceIDs())
	c.BasisSHA256 = b.digest
	return c, err
}

// CheckActions rereads the complete declared basis of each distinct emitted
// action. Repeated occurrences use the same caller-owned model action mapping.
func (b *Basis) CheckActions(ctx context.Context, query *Profile, emitted []string) (GovernanceCheck, error) {
	c := GovernanceCheck{Tool: "check_current_evidence_plan", Policy: b.Policy(), BasisSHA256: b.digest}
	if len(emitted) == 0 {
		// The native plan tool requires at least one action. Recheck sources but
		// never manufacture an action or label this as plan acceptance.
		c, err := b.Preflight(ctx, query)
		if err == nil {
			c.Status = "NO_ACTIONS_BASIS_CHECKED"
		}
		return c, err
	}
	ids := slices.Clone(emitted)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	actions := []ActionBasis{}
	roots := []string{}
	for _, id := range ids {
		found := false
		for _, a := range b.manifest.Actions {
			if a.ActionID == id {
				actions = append(actions, a)
				roots = append(roots, a.BasisIDs...)
				found = true
				break
			}
		}
		if !found {
			return checkFailure(c, errors.New("emitted action absent from pinned manifest"))
		}
	}
	slices.Sort(roots)
	roots = slices.Compact(roots)
	if query == nil || query.kind != ProfileQuery {
		return checkFailure(c, ErrCapabilityUnavailable)
	}
	raw, err := query.call(ctx, c.Tool, map[string]any{"policy": b.Policy(), "actions": actions})
	if err != nil {
		return checkFailure(c, err)
	}
	c.Raw, c.ResponseSHA256 = bytes.Clone(raw), validation.Digest(raw)
	var native map[string]json.RawMessage
	if json.Unmarshal(raw, &native) == nil {
		c.Assessment = bytes.Clone(native["assessment"])
	}
	var response struct {
		Assessment useAssessment `json:"assessment"`
		Actions    []ActionBasis `json:"actions"`
		Accepted   *bool         `json:"accepted"`
	}
	if err := decodeGovernance(raw, &response, false); err != nil {
		return checkFailure(c, err)
	}
	if response.Accepted == nil || len(response.Actions) != len(actions) {
		return checkFailure(c, errors.New("missing complete plan assessment"))
	}
	for i, action := range response.Actions {
		if action.ActionID != actions[i].ActionID || !slices.Equal(action.BasisIDs, actions[i].BasisIDs) {
			return checkFailure(c, errors.New("assessed action-basis mapping mismatch"))
		}
	}
	ok, err := response.Assessment.eligible(b.Policy(), roots)
	if err != nil {
		return checkFailure(c, err)
	}
	if *response.Accepted && !ok {
		return checkFailure(c, errors.New("accepted plan has ineligible basis"))
	}
	return finishCheck(c, *response.Accepted && ok)
}

// Reject ambiguous JSON at both caller authority and provider response boundaries.
// Responses retain unknown native provenance fields; manifest fields are strict.
func decodeGovernance(raw []byte, out any, strict bool) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("governance JSON nesting limit")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return err
					}
					s, ok := key.(string)
					if !ok {
						return errors.New("invalid JSON key")
					}
					s = strings.ToLower(s)
					if seen[s] {
						return errors.New("ambiguous governance JSON key")
					}
					seen[s] = true
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err := value(depth + 1); err != nil {
						return err
					}
				}
			default:
				return errors.New("invalid JSON delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("one governance JSON value required")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	if strict {
		d.DisallowUnknownFields()
	}
	return d.Decode(out)
}
