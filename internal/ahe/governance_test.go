package ahe

import (
	"encoding/json"
	"errors"
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
	ok := mode != "withdrawn" && mode != "contradicted" && mode != "source-failed" && mode != "unknown"
	for _, id := range ids {
		outcome := "eligible"
		if !ok {
			outcome = "blocked"
		}
		if mode == "withdrawn" {
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
	for _, mode := range []string{"", "withdrawn", "contradicted", "source-failed", "unknown", "wrong-policy", "missing-node", "wrong-root", "missing-time", "missing-accepted", "wrong-mapping", "contradictory-accept"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := testClient(t, mode)
			q := testProfile(t, c, ProfileQuery, "get_evidence_record", "get_evidence_use_state", "check_current_evidence_plan")
			a, _ := testAuthority(t)
			b := fixtureBasis(t, a)
			check, err := b.CheckActions(t.Context(), q, []string{"set", "reset", "set"})
			if mode == "" {
				if err != nil || check.Status != "GOVERNANCE_ACCEPTED" {
					t.Fatal(check, err)
				}
			} else if err == nil {
				t.Fatal("invalid or rejected response accepted")
			}
			if check.ResponseSHA256 != validation.Digest(check.Raw) {
				t.Fatal("raw response not retained")
			}
			if slices.Contains([]string{"withdrawn", "contradicted", "source-failed", "unknown"}, mode) && !errors.Is(err, ErrGovernanceRejected) {
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
