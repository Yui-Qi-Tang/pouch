package ahe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// These fixtures explicitly declare their original whole-contract AND basis
// for each action. Production code must never infer this mapping.
func fixtureManifest(t *testing.T, raw []byte) BasisManifest {
	t.Helper()
	var c validation.Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for id := range c.SourceBinding {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	m := BasisManifest{SchemaVersion: BasisVersion, AuthoritySHA256: validation.Digest(raw), RequestID: c.RequestID, UsePolicy: CurrentActionPolicy, Actions: []ActionBasis{}}
	for _, a := range c.Model.Actions {
		m.Actions = append(m.Actions, ActionBasis{ActionID: a.ID, BasisIDs: slices.Clone(ids)})
	}
	return m
}
func fixtureBasis(t *testing.T, authority []byte) *Basis {
	t.Helper()
	m := fixtureManifest(t, authority)
	raw, _ := json.Marshal(m)
	b, err := NewBasis(authority, m.AuthoritySHA256, m.RequestID, raw, validation.Digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Protocol responses only; these tests make no native database acceptance claim.
func governanceStub(policy string, ids []string, actions []ActionBasis, mode string) map[string]any {
	plan := actions != nil
	for _, action := range actions {
		ids = append(ids, action.BasisIDs...)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	nodes := []any{}
	ok := !slices.Contains([]string{"withdrawn", "contradicted", "source-failed", "unknown", "historical_derivation", "unknown_derivation_use_class", "unclassified_currentness"}, mode)
	for _, id := range ids {
		outcome := "eligible"
		if !ok {
			outcome = "blocked"
		}
		if mode == "withdrawn" || mode == "historical_derivation" {
			outcome = "excluded"
		}
		nodes = append(nodes, map[string]any{"node_id": id, "outcome": outcome, "reasons": []any{map[string]string{"code": mode}}})
	}
	assessment := map[string]any{"projection_version": "evidence-use-projection/v1", "state_digest": "evidence-use:sha256:" + strings.Repeat("a", 64), "policy": policy, "evaluated_at": "2026-10-14T00:00:00Z", "roots": ids, "nodes": nodes}
	data := map[string]any{"assessment": assessment}
	if plan {
		data["actions"] = actions
		data["accepted"] = ok
	}
	switch mode {
	case "projection-v2", "historical_derivation", "unknown_derivation_use_class":
		assessment["projection_version"] = "evidence-use-projection/v2"
	case "unknown-projection":
		assessment["projection_version"] = "evidence-use-projection/v3"
	case "wrong-policy":
		assessment["policy"] = "historical/v1"
	case "missing-node":
		assessment["nodes"] = []any{}
	case "wrong-root":
		assessment["roots"] = []string{"canon-node:other"}
	case "missing-time":
		delete(assessment, "evaluated_at")
	case "missing-accepted":
		if plan {
			delete(data, "accepted")
		}
	case "wrong-mapping":
		if plan {
			data["actions"] = []ActionBasis{{ActionID: "reset", BasisIDs: []string{"canon-node:other"}}}
		}
	case "contradictory-accept":
		if plan {
			nodes[0].(map[string]any)["outcome"] = "excluded"
			data["accepted"] = true
		}
	}
	return data
}

func TestBasisManifestBindsCallerInputs(t *testing.T) {
	a, _ := testAuthority(t)
	for _, tc := range []struct {
		name   string
		mutate func(*BasisManifest)
	}{
		{"missing-recovery", func(m *BasisManifest) { m.Actions = m.Actions[:1] }},
		{"unknown-action", func(m *BasisManifest) { m.Actions[0].ActionID = "unknown" }},
		{"duplicate-action", func(m *BasisManifest) { m.Actions[0].ActionID = m.Actions[1].ActionID }},
		{"empty-basis", func(m *BasisManifest) { m.Actions[0].BasisIDs = nil }},
		{"extra-source", func(m *BasisManifest) { m.Actions[0].BasisIDs = []string{"canon-node:other"} }},
		{"duplicate-basis", func(m *BasisManifest) { m.Actions[0].BasisIDs = []string{"canon-node:basis", "canon-node:basis"} }},
		{"request", func(m *BasisManifest) { m.RequestID = "other" }},
		{"authority", func(m *BasisManifest) { m.AuthoritySHA256 = strings.Repeat("a", 64) }},
		{"policy", func(m *BasisManifest) { m.UsePolicy = "" }},
		{"historical-policy", func(m *BasisManifest) { m.UsePolicy = "historical/v1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixtureManifest(t, a)
			tc.mutate(&m)
			raw, _ := json.Marshal(m)
			if _, err := NewBasis(a, validation.Digest(a), "test", raw, validation.Digest(raw)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	b := fixtureBasis(t, a)
	if _, err := NewBasis(a, validation.Digest(a), "test", append(b.Raw(), ' '), b.SHA256()); err == nil {
		t.Fatal("changed bytes accepted")
	}
	for _, raw := range []string{`{"use_policy":"a","use_policy":"b"}`, `{"use_policy":"a","Use_Policy":"b"}`, `{} {}`} {
		var m BasisManifest
		if err := decodeGovernance([]byte(raw), &m, true); err == nil {
			t.Fatal("ambiguous input accepted")
		}
	}
}

func TestGovernanceFailClosedAndPolicyBinding(t *testing.T) {
	for _, mode := range []string{"", "projection-v2", "withdrawn", "contradicted", "source-failed", "unknown", "historical_derivation", "unknown_derivation_use_class", "unknown-projection", "wrong-policy", "missing-node", "wrong-root", "missing-time", "missing-accepted", "wrong-mapping", "contradictory-accept"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := testClient(t, mode)
			q := testProfile(t, c, ProfileQuery, "get_evidence_record", "get_evidence_use_state", "check_current_evidence_plan")
			a, _ := testAuthority(t)
			b := fixtureBasis(t, a)
			check, err := b.CheckActions(t.Context(), q, []string{"set", "reset", "set"})
			if mode == "" || mode == "projection-v2" {
				if err != nil || check.Status != "GOVERNANCE_ACCEPTED" {
					t.Fatal(check, err)
				}
			} else if err == nil {
				t.Fatal("invalid or rejected response accepted")
			}
			if check.ResponseSHA256 != validation.Digest(check.Raw) {
				t.Fatal("raw response not retained")
			}
			if slices.Contains([]string{"withdrawn", "contradicted", "source-failed", "unknown", "historical_derivation", "unknown_derivation_use_class"}, mode) && !errors.Is(err, ErrGovernanceRejected) {
				t.Fatal(err)
			}
			if mode == "unknown-projection" && (!errors.Is(err, ErrGovernanceUnavailable) || errors.Is(err, ErrGovernanceRejected)) {
				t.Fatal(err)
			}
		})
	}
	c, _ := testClient(t, "")
	q := testProfile(t, c, ProfileQuery, "get_evidence_use_state")
	check, err := CheckSources(t.Context(), q, GroundedActionPolicy, []string{"canon-node:basis"})
	if err != nil || check.Policy != GroundedActionPolicy {
		t.Fatal(check, err)
	}
	for mode, want := range map[string]string{"projection-v2": "GOVERNANCE_ACCEPTED", "historical_derivation": "GOVERNANCE_REJECTED", "unknown_derivation_use_class": "GOVERNANCE_REJECTED", "unknown-projection": "GOVERNANCE_UNAVAILABLE"} {
		c, _ := testClient(t, mode)
		q := testProfile(t, c, ProfileQuery, "get_evidence_use_state")
		if check, _ := CheckSources(t.Context(), q, CurrentActionPolicy, []string{"canon-node:basis"}); check.Status != want {
			t.Fatal(mode, check)
		}
	}
}

func TestGovernanceRequiredBeforeWritesAndRechecked(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("POUCH_MCP_TEST_GOVERNANCE_FILE", state)
	c, calls := testClient(t, "")
	q := testProfile(t, c, ProfileQuery, "get_evidence_record", "get_evidence_use_state", "check_current_evidence_plan")
	i := testProfile(t, c, ProfileIntake, "submit_external_source", "submit_extractor_output")
	e := testProfile(t, c, ProfileEndpointReviewer, "get_endpoint_review", "admit_reviewed_endpoint")
	a, pkg := testAuthority(t)
	b := fixtureBasis(t, a)
	p, err := Prepare(t.Context(), q, a, validation.Digest(a), "test", pkg, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte("withdrawn"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Submit(t.Context(), i, "r", time.Now()); !errors.Is(err, ErrGovernanceRejected) {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(calls)
	if strings.Contains(string(log), "submit_") {
		t.Fatal("rejected basis reached intake")
	}
	if err := os.WriteFile(state, nil, 0600); err != nil {
		t.Fatal(err)
	}
	pending, err := p.Submit(t.Context(), i, "r", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Review(t.Context(), e, pending.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if r.request.Derivation.UsePolicy != CurrentActionPolicy || !strings.Contains(r.request.Derivation.Method, b.SHA256()) {
		t.Fatal("review missing policy or manifest binding")
	}
	approval := Approval{RequestID: "admit", Subject: r.Subject, DisplaySHA256: r.DisplaySHA256, PackageSHA256: r.PackageSHA256, Reason: "TEST APPROVAL STUB"}
	if err := os.WriteFile(state, []byte("contradicted"), 0600); err != nil {
		t.Fatal(err)
	}
	publication, err := p.Admit(t.Context(), e, q, r, approval)
	if !errors.Is(err, ErrGovernanceRejected) || publication.Governance.PackageSHA256 != validation.Digest(pkg) {
		t.Fatal(publication, err)
	}
	log, _ = os.ReadFile(calls)
	if strings.Contains(string(log), "admit_reviewed_endpoint") {
		t.Fatal("stale pass reached admission")
	}
}

func TestZeroActionsAndMissingCapabilities(t *testing.T) {
	c, calls := testClient(t, "")
	a, _ := testAuthority(t)
	b := fixtureBasis(t, a)
	q := testProfile(t, c, ProfileQuery, "get_evidence_record", "get_evidence_use_state")
	check, err := b.CheckActions(t.Context(), q, nil)
	if err != nil || check.Status != "NO_ACTIONS_BASIS_CHECKED" {
		t.Fatal(check, err)
	}
	log, _ := os.ReadFile(calls)
	if strings.Contains(string(log), "check_current_evidence_plan") {
		t.Fatal("invented an action")
	}
	if _, err := b.CheckActions(t.Context(), q, []string{"set"}); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatal(err)
	}
	if _, err := b.CheckActions(t.Context(), q, []string{"undeclared"}); !errors.Is(err, ErrGovernanceUnavailable) {
		t.Fatal(err)
	}
}

func TestGovernanceResponseBytesSurviveFormatting(t *testing.T) {
	raw := []byte("{\n  \"assessment\": { \"policy\": \"current-action/v1\" }\n}")
	c := GovernanceCheck{Raw: raw, ResponseSHA256: validation.Digest(raw)}
	encoded, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var decoded GovernanceCheck
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Raw) != string(raw) || validation.Digest(decoded.Raw) != decoded.ResponseSHA256 {
		t.Fatal("formatted wrapper changed native bytes")
	}
	native := &ToolError{Payload: []byte(`{"code":"read_scope_incomplete"}`)}
	failed, err := checkFailure(GovernanceCheck{}, native)
	if !errors.Is(err, ErrGovernanceUnavailable) || string(failed.Raw) != string(native.Payload) {
		t.Fatal("lost native failure", failed, err)
	}
}

func TestSchemaRepinningDoesNotSupplyPolicy(t *testing.T) {
	c, _ := testClient(t, "")
	if _, err := c.Bind(ProfileEndpointReviewer, map[string]string{"get_endpoint_review": strings.Repeat("0", 64)}); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatal(err)
	}
	p := testProfile(t, c, ProfileEndpointReviewer, "get_endpoint_review")
	_, err := p.call(t.Context(), "get_endpoint_review", map[string]any{"kind": "derived_spec", "proposal_occurrence_id": "occ:test", "derivation": map[string]any{"parent_node_ids": []string{"canon-node:basis"}, "method": "test", "producer": "test", "trace_ref": "test"}})
	var native *ToolError
	if !errors.As(err, &native) {
		t.Fatal("repinning unexpectedly made an old request valid", err)
	}
}

// The readable list is a copy of the assessment: it names each non-eligible
// node with AHE's reason codes and leaves the raw response and status alone.
func TestIneligibleNodesNameNodeAndReason(t *testing.T) {
	c, _ := testClient(t, "unclassified_currentness")
	q := testProfile(t, c, ProfileQuery, "get_evidence_use_state")
	check, err := CheckSources(t.Context(), q, CurrentActionPolicy, []string{"canon-node:basis"})
	if !errors.Is(err, ErrGovernanceRejected) || check.Status != "GOVERNANCE_REJECTED" {
		t.Fatal(check, err)
	}
	want := []IneligibleNode{{NodeID: "canon-node:basis", Outcome: "blocked", ReasonCodes: []string{"unclassified_currentness"}}}
	if !slices.EqualFunc(check.IneligibleNodes, want, func(a, b IneligibleNode) bool {
		return a.NodeID == b.NodeID && a.Outcome == b.Outcome && slices.Equal(a.ReasonCodes, b.ReasonCodes)
	}) {
		t.Fatal(check.IneligibleNodes)
	}
	if check.ResponseSHA256 != validation.Digest(check.Raw) || !strings.Contains(string(check.Assessment), "unclassified_currentness") {
		t.Fatal("raw response or assessment lost")
	}

	for _, mode := range []string{"", "projection-v2"} {
		c, _ := testClient(t, mode)
		q := testProfile(t, c, ProfileQuery, "get_evidence_use_state")
		check, err := CheckSources(t.Context(), q, CurrentActionPolicy, []string{"canon-node:basis"})
		if err != nil || check.Status != "GOVERNANCE_ACCEPTED" || len(check.IneligibleNodes) != 0 {
			t.Fatal(mode, check, err)
		}
	}
}

func TestIneligibleListsOnlyNonEligibleNodes(t *testing.T) {
	raw := `{"nodes":[
		{"node_id":"canon-node:ok","outcome":"eligible","reasons":[]},
		{"node_id":"canon-node:blocked","outcome":"blocked","reasons":[{"code":"unclassified_currentness","related_id":"canon-node:blocked"},{"code":"missing_node"}]},
		{"node_id":"canon-node:gone","outcome":"excluded"}]}`
	var a useAssessment
	if err := decodeGovernance([]byte(raw), &a, false); err != nil {
		t.Fatal(err)
	}
	got := a.ineligible()
	if len(got) != 2 || got[0].NodeID != "canon-node:blocked" || got[0].Outcome != "blocked" || !slices.Equal(got[0].ReasonCodes, []string{"unclassified_currentness", "missing_node"}) {
		t.Fatal(got)
	}
	if got[1].NodeID != "canon-node:gone" || got[1].Outcome != "excluded" || got[1].ReasonCodes == nil || len(got[1].ReasonCodes) != 0 {
		t.Fatal(got)
	}
}

func TestSourceIDLimitIs64(t *testing.T) {
	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("canon-node:%03d", i)
		}
		return out
	}
	if got, err := canonicalIDs(ids(64)); err != nil || len(got) != 64 {
		t.Fatal(len(got), err)
	}
	if _, err := canonicalIDs(ids(65)); err == nil {
		t.Fatal("65 IDs accepted")
	}
	// The same bound applies to an assessment's roots, which must match exactly.
	a := useAssessment{Roots: ids(64)}
	if got, err := canonicalIDs(a.Roots); err != nil || !slices.Equal(got, ids(64)) {
		t.Fatal(err)
	}
	c, calls := testClient(t, "")
	q := testProfile(t, c, ProfileQuery, "open_canonical_read_view")
	if _, err := ReadSnapshot(t.Context(), q, ReadRequest{RootNodeIDs: ids(65), Relations: []string{}, MaxNodes: 65}); err == nil {
		t.Fatal("65 roots accepted")
	}
	if raw, _ := os.ReadFile(calls); len(raw) != 0 {
		t.Fatal("oversized read reached AHE")
	}
	if _, err := ReadSnapshot(t.Context(), q, ReadRequest{RootNodeIDs: ids(64), Relations: []string{}, MaxNodes: 64}); err != nil && strings.Contains(err.Error(), "invalid bounded read scope") {
		t.Fatal(err)
	}
}

// A request Pouch refuses itself must not read like an AHE outage, while the
// status stays GOVERNANCE_UNAVAILABLE.
func TestInvalidRequestIsNamedInFailure(t *testing.T) {
	c, calls := testClient(t, "")
	q := testProfile(t, c, ProfileQuery, "get_evidence_use_state")
	ids := make([]string, 65)
	for i := range ids {
		ids[i] = fmt.Sprintf("canon-node:%03d", i)
	}
	check, err := CheckSources(t.Context(), q, CurrentActionPolicy, ids)
	if !errors.Is(err, ErrGovernanceUnavailable) || check.Status != "GOVERNANCE_UNAVAILABLE" || !strings.HasPrefix(check.Failure, "invalid input: ") {
		t.Fatal(check.Status, check.Failure, err)
	}
	if raw, _ := os.ReadFile(calls); len(raw) != 0 {
		t.Fatal("invalid request reached AHE")
	}
	// An unusable AHE answer is not an input error.
	c, _ = testClient(t, "unknown-projection")
	q = testProfile(t, c, ProfileQuery, "get_evidence_use_state", "check_current_evidence_plan", "get_evidence_record")
	check, err = CheckSources(t.Context(), q, CurrentActionPolicy, []string{"canon-node:basis"})
	if !errors.Is(err, ErrGovernanceUnavailable) || check.Failure == "" || strings.HasPrefix(check.Failure, "invalid input") {
		t.Fatal(check.Failure, err)
	}
	// An emitted action the manifest does not know is the caller's input.
	a, _ := testAuthority(t)
	check, err = fixtureBasis(t, a).CheckActions(t.Context(), q, []string{"no-such-action"})
	if !errors.Is(err, ErrGovernanceUnavailable) || !strings.HasPrefix(check.Failure, "invalid input: ") {
		t.Fatal(check.Failure, err)
	}
}
