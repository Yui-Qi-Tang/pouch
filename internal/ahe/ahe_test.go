package ahe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

const stubSchema = `{"type":"object"}`

// This subprocess exercises the real stdio client; it is a protocol fake, not
// evidence of native AHE database integration.
func TestMCPServerProcess(t *testing.T) {
	if os.Getenv("POUCH_MCP_TEST_SERVER") != "1" {
		return
	}
	mode := os.Getenv("POUCH_MCP_TEST_MODE")
	scan := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	statement := ""
	var reviewed ReviewRequest
	var admitted map[string]any
	for scan.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			os.Exit(2)
		}
		var value any
		switch req.Method {
		case "notifications/initialized":
			continue
		case "initialize":
			v := protocolVersion
			if mode == "wrong-protocol" {
				v = "old"
			}
			value = map[string]any{"protocolVersion": v, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			var list []any
			for _, name := range []string{"get_evidence_record", "open_canonical_read_view", "submit_external_source", "submit_extractor_output", "get_endpoint_review", "admit_reviewed_endpoint"} {
				list = append(list, map[string]any{"name": name, "inputSchema": json.RawMessage(stubSchema), "annotations": map[string]any{"readOnlyHint": name == "get_evidence_record" || name == "open_canonical_read_view" || name == "get_endpoint_review"}})
			}
			value = map[string]any{"tools": list}
		case "tools/call":
			if path := os.Getenv("POUCH_MCP_TEST_CALLS"); path != "" {
				f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
				fmt.Fprintln(f, req.Params.Name)
				f.Close()
			}
			var data map[string]any
			switch req.Params.Name {
			case "get_evidence_record":
				var in struct {
					ID string `json:"canonical_id"`
				}
				json.Unmarshal(req.Params.Arguments, &in)
				text := "basis"
				kind := "observed_fact"
				if mode == "wrong-source" {
					text = "changed"
				}
				if in.ID == "canon-node:result" {
					text = statement
					kind = "derived_claim"
					if mode == "wrong-readback" {
						text = "wrong"
					}
				}
				data = map[string]any{"record_ref": map[string]string{"id": in.ID}, "canonical_ref": in.ID, "statement_text": text, "admission_outcome": "admitted", "canonical": map[string]string{"node_kind": kind}}
				if in.ID == "canon-node:result" {
					data["endpoint_admission"] = admitted
				}
			case "open_canonical_read_view":
				var in ReadRequest
				json.Unmarshal(req.Params.Arguments, &in)
				depth := in.MaxDepth
				if mode == "wrong-scope" {
					depth++
				}
				data = map[string]any{"schema_version": "canonical-read-view-query-v1", "view": map[string]any{"handle": "view:test", "snapshot_id": "snapshot:test", "root_node_ids": in.RootNodeIDs, "relations": in.Relations, "max_depth": depth, "max_nodes": in.MaxNodes, "max_edges": in.MaxEdges, "node_count": 1, "edge_count": 0, "truncated": mode == "truncated", "global_absence_inference_allowed": false}, "artifact": map[string]any{"schema_version": "canonical-evidence-graph/v1", "snapshot_id": "snapshot:test", "nodes": []any{map[string]string{"id": "canon-node:basis"}}, "edges": []any{}}}
			case "submit_external_source":
				var in struct {
					Content string `json:"content"`
				}
				json.Unmarshal(req.Params.Arguments, &in)
				statement = in.Content
				data = map[string]any{"source_snapshot_id": "source:test", "extraction_view_id": "extraction:test", "raw_content_hash": "sha256:" + validation.Digest([]byte(statement)), "spans": []any{map[string]string{"span_id": "span:test"}}}
			case "submit_extractor_output":
				data = map[string]any{"proposal_occurrence_id": "occ:test", "proposal_count": 1, "status": "pending"}
			case "get_endpoint_review":
				json.Unmarshal(req.Params.Arguments, &reviewed)
				// Separate query/intake/reviewer profiles can use separate processes. The
				// exact statement is passed through an operator fixture for this test only.
				if statement == "" {
					statement = os.Getenv("POUCH_MCP_TEST_STATEMENT")
				}
				text := statement
				if mode == "wrong-statement" {
					text = "wrong"
				}
				data = map[string]any{"subject": "endpoint:test", "lifecycle": map[string]string{"mode": "pending_admission"}, "display": map[string]any{"contract_version": "endpoint-review/v1", "request": reviewed, "proposal": map[string]string{"ProposalOccurrenceID": "occ:test", "StatementText": text, "AdmissionOutcome": "pending"}, "ancestors": []any{}, "source_leaves": []any{}}}
			case "admit_reviewed_endpoint":
				if mode == "rpc-write-error" {
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32603, "message": "internal error after processing"}})
					continue
				}
				if mode == "lost-write" {
					os.Exit(0)
				}
				if mode == "slow-write" {
					time.Sleep(time.Second)
				}
				var in struct {
					RequestID       string `json:"request_id"`
					ExpectedSubject string `json:"expected_subject"`
					Reason          string `json:"decision_reason"`
				}
				json.Unmarshal(req.Params.Arguments, &in)
				admitted = map[string]any{"contract_version": "reviewed-endpoint-admission/v1", "request_id": in.RequestID, "review_subject": in.ExpectedSubject, "reviewer_id": "TEST APPROVAL STUB", "decision_reason": in.Reason, "admission": map[string]any{"ProposalOccurrenceID": "occ:test", "AdmissionOutcome": "admitted", "CanonicalRef": "canon-node:result", "ParentNodeIDs": reviewed.Derivation.ParentNodeIDs}}
				data = admitted
			}
			value = map[string]any{"structuredContent": data, "isError": false}
		}
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value}); err != nil {
			os.Exit(3)
		}
	}
	os.Exit(0)
}
func testClient(t *testing.T, mode string) (*Client, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(t.TempDir(), "calls")
	c, err := Start(t.Context(), Command{Path: exe, Args: []string{"-test.run=^TestMCPServerProcess$"}, Env: append(os.Environ(), "POUCH_MCP_TEST_SERVER=1", "POUCH_MCP_TEST_MODE="+mode, "POUCH_MCP_TEST_CALLS="+calls)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, calls
}
func testProfile(t *testing.T, c *Client, kind ProfileKind, names ...string) *Profile {
	t.Helper()
	pins := map[string]string{}
	for _, name := range names {
		pins[name] = validation.Digest([]byte(stubSchema))
	}
	p, err := c.Bind(kind, pins)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func testAuthority(t *testing.T) ([]byte, []byte) {
	t.Helper()
	contract := validation.Contract{SchemaVersion: validation.Version, RequestID: "test", SourceBinding: map[string]string{"canon-node:basis": validation.Digest([]byte("basis"))}, Assumptions: map[string]string{}, Model: validation.Model{Fields: map[string][]string{"x": {"0", "1"}}, Forbidden: []validation.State{}, Actions: []validation.Action{{ID: "set", Guard: validation.State{"x": "0"}, Effect: validation.State{"x": "1"}, Frame: []string{}}, {ID: "reset", Guard: validation.State{"x": "1"}, Effect: validation.State{"x": "0"}, Frame: []string{}}}}, Start: validation.State{"x": "0"}, Goal: validation.State{"x": "1"}, Baseline: validation.State{"x": "0"}, ForwardLimit: 1, ReturnLimit: 1}
	a, _ := json.Marshal(contract)
	p, _ := json.Marshal(validation.Package{SchemaVersion: validation.Version, RequestID: "test", AuthoritySHA256: validation.Digest(a), SourceBinding: contract.SourceBinding, Assumptions: map[string]string{}, Forward: validation.Trace{States: []validation.State{{"x": "0"}, {"x": "1"}}, Actions: []string{"set"}, ClaimShortest: true}, ReturnKind: "return", Return: &validation.Trace{States: []validation.State{{"x": "1"}, {"x": "0"}}, Actions: []string{"reset"}, ClaimShortest: true}, ClosedStates: []validation.State{}})
	return a, p
}
func TestDiscoveryPinningAndProfileIsolation(t *testing.T) {
	c, calls := testClient(t, "")
	if _, err := c.Bind(ProfileQuery, map[string]string{"get_evidence_record": "wrong"}); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatal(err)
	}
	if _, err := c.Bind(ProfileQuery, map[string]string{"admit_reviewed_endpoint": validation.Digest([]byte(stubSchema))}); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(calls)
	if len(raw) != 0 {
		t.Fatal("discovery or denied binding called a tool")
	}
	p := testProfile(t, c, ProfileQuery, "get_evidence_record")
	if _, err := p.call(t.Context(), "admit_reviewed_endpoint", map[string]any{}); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatal(err)
	}
}
func TestReadSnapshotScopeAndTruncation(t *testing.T) {
	for _, mode := range []string{"", "truncated", "wrong-scope"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := testClient(t, mode)
			q := testProfile(t, c, ProfileQuery, "open_canonical_read_view")
			s, err := ReadSnapshot(t.Context(), q, ReadRequest{RootNodeIDs: []string{"canon-node:basis"}, Relations: []string{}, MaxNodes: 2})
			if mode == "wrong-scope" {
				if err == nil {
					t.Fatal("scope mismatch accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if *s.View.Truncated != (mode == "truncated") || s.ResponseSHA256 != validation.Digest(s.RawResponse) {
				t.Fatal("lost scope or exact response bytes")
			}
		})
	}
}
func TestPrepareRejectsSourceBinding(t *testing.T) {
	c, calls := testClient(t, "wrong-source")
	q := testProfile(t, c, ProfileQuery, "get_evidence_record")
	a, p := testAuthority(t)
	if _, err := Prepare(t.Context(), q, a, validation.Digest(a), "test", p); err == nil {
		t.Fatal("wrong source accepted")
	}
	raw, _ := os.ReadFile(calls)
	if strings.Contains(string(raw), "admit") {
		t.Fatal("validation wrote to AHE")
	}
}
func TestPublicationAndFaults(t *testing.T) {
	for _, mode := range []string{"", "wrong-statement", "wrong-readback", "lost-write", "slow-write", "rpc-write-error"} {
		t.Run(mode, func(t *testing.T) {
			c, calls := testClient(t, mode)
			q := testProfile(t, c, ProfileQuery, "get_evidence_record")
			i := testProfile(t, c, ProfileIntake, "submit_external_source", "submit_extractor_output")
			e := testProfile(t, c, ProfileEndpointReviewer, "get_endpoint_review", "admit_reviewed_endpoint")
			a, pkg := testAuthority(t)
			p, err := Prepare(t.Context(), q, a, validation.Digest(a), "test", pkg)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := p.Submit(t.Context(), i, "test-result", time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			r, err := p.Review(t.Context(), e, pending.ProposalID)
			if mode == "wrong-statement" {
				if err == nil {
					t.Fatal("wrong proposal accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			approve := Approval{RequestID: "test-admit", Subject: r.Subject, DisplaySHA256: r.DisplaySHA256, PackageSHA256: r.PackageSHA256, Reason: "TEST APPROVAL STUB: exact synthetic display"}
			changed := approve
			changed.PackageSHA256 = "wrong"
			if _, err := p.Admit(t.Context(), e, q, r, changed); err == nil {
				t.Fatal("changed approval accepted")
			}
			before, _ := os.ReadFile(calls)
			if strings.Contains(string(before), "admit_reviewed_endpoint") {
				t.Fatal("unapproved write occurred")
			}
			ctx := t.Context()
			if mode == "slow-write" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			result, err := p.Admit(ctx, e, q, r, approve)
			if mode == "lost-write" || mode == "slow-write" || mode == "rpc-write-error" {
				if !errors.Is(err, ErrDeliveryUnknown) {
					t.Fatal(err)
				}
			} else if mode == "wrong-readback" {
				if err == nil || len(result.Admission) == 0 {
					t.Fatal("readback error lost admission receipt")
				}
			} else if err != nil || result.CanonicalID != "canon-node:result" {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(calls)
			if strings.Count(string(after), "admit_reviewed_endpoint") != 1 {
				t.Fatal("write was retried")
			}
		})
	}
}
func TestAmbiguousJSONRejected(t *testing.T) {
	for _, s := range []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{} {}`, `[`} {
		if uniqueJSON([]byte(s)) == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
