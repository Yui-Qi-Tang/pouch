package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/ahe"
	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func TestGovernanceCLIProcess(t *testing.T) {
	if os.Getenv("POUCH_CLI_GOVERNANCE_SERVER") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scan.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		var value any
		switch req.Method {
		case "notifications/initialized":
			continue
		case "initialize":
			value = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			list := []any{}
			for _, name := range []string{"get_evidence_record", "get_evidence_use_state", "check_current_evidence_plan"} {
				list = append(list, map[string]any{"name": name, "inputSchema": json.RawMessage(`{"type":"object"}`), "annotations": map[string]bool{"readOnlyHint": true}})
			}
			value = map[string]any{"tools": list}
		case "tools/call":
			f, err := os.OpenFile(os.Getenv("POUCH_CLI_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(2)
			}
			f.WriteString(req.Params.Name + "\n")
			f.Close()
			var data any
			if req.Params.Name == "get_evidence_record" {
				data = map[string]any{"record_ref": map[string]string{"id": "canon-node:basis"}, "canonical_ref": "canon-node:basis", "statement_text": "basis", "admission_outcome": "admitted", "canonical": map[string]string{"node_kind": "source_claim"}}
			} else {
				var in struct {
					Policy string   `json:"policy"`
					IDs    []string `json:"canonical_ids"`
				}
				json.Unmarshal(req.Params.Arguments, &in)
				nodes := []any{}
				for _, id := range in.IDs {
					nodes = append(nodes, map[string]any{"node_id": id, "outcome": "excluded", "reasons": []any{map[string]string{"code": "withdrawn"}}})
				}
				data = map[string]any{"assessment": map[string]any{"projection_version": "evidence-use-projection/v1", "state_digest": "evidence-use:sha256:" + strings.Repeat("a", 64), "policy": in.Policy, "evaluated_at": "2026-10-14T00:00:00Z", "roots": in.IDs, "nodes": nodes}}
			}
			value = map[string]any{"structuredContent": data, "isError": false}
		}
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value}); err != nil {
			os.Exit(3)
		}
	}
	os.Exit(0)
}

func writeFixtureJSON(t *testing.T, path string, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGovernanceCLIStopsBeforeSolver(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "calls")
	pins := map[string]string{}
	for _, name := range []string{"get_evidence_record", "get_evidence_use_state", "check_current_evidence_plan"} {
		pins[name] = validation.Digest([]byte(`{"type":"object"}`))
	}
	config := filepath.Join(dir, "config.json")
	writeFixtureJSON(t, config, connectionConfig{Query: profileConfig{Command: ahe.Command{Path: exe, Args: []string{"-test.run=^TestGovernanceCLIProcess$"}, Env: append(os.Environ(), "POUCH_CLI_GOVERNANCE_SERVER=1", "POUCH_CLI_CALLS="+calls)}, Pins: pins}})
	c := validation.Contract{SchemaVersion: validation.Version, RequestID: "cli", SourceBinding: map[string]string{"canon-node:basis": validation.Digest([]byte("basis"))}, Assumptions: map[string]string{}, Model: validation.Model{Fields: map[string][]string{"x": {"0", "1"}}, Forbidden: []validation.State{}, Actions: []validation.Action{{ID: "set", Guard: validation.State{"x": "0"}, Effect: validation.State{"x": "1"}, Frame: []string{}}}}, Start: validation.State{"x": "0"}, Goal: validation.State{"x": "1"}, Baseline: validation.State{"x": "0"}, ForwardLimit: 1, ReturnLimit: 1}
	authority := filepath.Join(dir, "authority.json")
	raw := writeFixtureJSON(t, authority, c)
	hash := validation.Digest(raw)
	basis := filepath.Join(dir, "basis.json")
	m := writeFixtureJSON(t, basis, ahe.BasisManifest{SchemaVersion: ahe.BasisVersion, AuthoritySHA256: hash, RequestID: c.RequestID, UsePolicy: ahe.CurrentActionPolicy, Actions: []ahe.ActionBasis{{ActionID: "set", BasisIDs: []string{"canon-node:basis"}}}})
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), []byte("basis"), 0600); err != nil {
		t.Fatal(err)
	}
	sources := filepath.Join(dir, "sources.json")
	writeFixtureJSON(t, sources, map[string]any{"schema_version": "pouch-sources/v1", "authority_sha256": hash, "request_id": c.RequestID, "sources": []any{map[string]string{"id": "canon-node:basis", "sha256": c.SourceBinding["canon-node:basis"], "file": "source.txt"}}})
	outputDir := filepath.Join(dir, "result")
	var out, errs bytes.Buffer
	args := []string{"solve", "--authority", authority, "--authority-sha256", hash, "--request-id", c.RequestID, "--sources", sources, "--out", outputDir, "--ahe-config", config, "--action-basis", basis, "--action-basis-sha256", validation.Digest(m), "--solver", "/no-such-solver"}
	if code := Run(t.Context(), args, &out, &errs); code != 2 || !strings.Contains(out.String(), "GOVERNANCE_REJECTED") || strings.Contains(errs.String(), "solver") {
		t.Fatalf("%d %s %s", code, &out, &errs)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "solver")); !os.IsNotExist(err) {
		t.Fatal("solver setup reached")
	}
	var report governanceReport
	data, err := os.ReadFile(filepath.Join(outputDir, "ahe-governance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.After != nil || report.Before.ResponseSHA256 != validation.Digest(report.Before.Raw) || !strings.Contains(string(report.Before.Assessment), "withdrawn") {
		t.Fatal("lost preflight evidence")
	}
	request := filepath.Join(dir, "request.json")
	writeFixtureJSON(t, request, map[string]any{"canonical_ids": []string{"canon-node:basis"}})
	out.Reset()
	errs.Reset()
	if code := Run(t.Context(), []string{"ahe-check-sources", "--config", config, "--request", request}, &out, &errs); code != 2 || !strings.Contains(out.String(), ahe.CurrentActionPolicy) {
		t.Fatal(code, &out, &errs)
	}
}

func TestPlanGateIncludesRecoveryAndBindsArtifacts(t *testing.T) {
	result := planning.Result{Forward: []planning.Path{{Trace: validation.Trace{Actions: []string{"set"}}}}, Returns: []planning.Return{{Trace: &validation.Trace{Actions: []string{"reset"}}}}}
	if got := emittedActions(result); !slices.Equal(got, []string{"set", "reset"}) {
		t.Fatal(got)
	}
	bundle := run.Bundle{Search: result, Paths: []run.PathResult{{PackageFile: "packages/p.json", Receipt: &validation.Receipt{PackageSHA256: strings.Repeat("b", 64)}}}}
	r := governanceReport{}
	if err := r.bindResult(bundle); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if r.SearchSHA256 != validation.Digest(raw) || r.PackageSHA256["packages/p.json"] != strings.Repeat("b", 64) {
		t.Fatal(r)
	}
	bundle.Search.Forward[0].Trace.Actions = []string{"another"}
	changed, _ := json.Marshal(bundle.Search)
	if r.SearchSHA256 == validation.Digest(changed) {
		t.Fatal("changed plan retained same binding")
	}
}
