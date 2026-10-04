package candidate

import (
	"encoding/json"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeBindingAndRefusals(t *testing.T) {
	_, _, p, pkg := fixture(t)
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "source.txt"), []byte("broken\n"), 0644)
	out := filepath.Join(t.TempDir(), "result")
	m, err := p.Materialize(t.Context(), pkg, base, out)
	if err != nil {
		t.Fatal(err)
	}
	mr, _ := os.ReadFile(filepath.Join(out, "materialization.json"))
	logs := []byte("recorded log")
	os.WriteFile(filepath.Join(out, "test.log"), logs, 0644)
	tool := []byte("test runner and parser provenance fixture")
	os.WriteFile(filepath.Join(out, "producer.txt"), tool, 0644)
	before := Observation{Tests: map[string]string{"test": "FAILED"}, Files: map[string]FileState{"source.txt": {validation.Digest([]byte("broken\n")), 0644}}, LogFiles: []string{"test.log"}}
	after := Observation{Tests: map[string]string{"test": "PASSED"}, Files: map[string]FileState{"source.txt": {validation.Digest([]byte("fixed\n")), 0644}}, LogFiles: []string{"test.log"}}
	e := RuntimeEvidence{SchemaVersion: "pouch-runtime-evidence/v1", MaterializationSHA256: validation.Digest(mr), PackageSHA256: m.PackageSHA256, RunnerSHA256: validation.Digest(tool), ParserSHA256: validation.Digest(tool), ExpectedTests: []string{"test"}, PatchFile: "candidate.patch", Artifacts: map[string]string{"candidate.patch": m.PatchSHA256, "test.log": validation.Digest(logs), "producer.txt": validation.Digest(tool)}, Baseline: before, Patched: after, Restored: before}
	raw, _ := json.Marshal(e)
	bound, err := p.BindRuntime(t.Context(), pkg, mr, raw, validation.Digest(raw), out)
	if err != nil {
		t.Fatal(err)
	}
	if !bound.BaselineDiscriminating || bound.Status != "RECORDED_CHECKS_PASS" {
		t.Fatal(bound)
	}
	for _, mut := range []func(*RuntimeEvidence){
		func(e *RuntimeEvidence) { e.PackageSHA256 = validation.Digest([]byte("old")) },
		func(e *RuntimeEvidence) { e.Patched.Tests["test"] = "FAILED" },
		func(e *RuntimeEvidence) { delete(e.Patched.Tests, "test") },
		func(e *RuntimeEvidence) { e.Restored.Files["source.txt"] = e.Patched.Files["source.txt"] },
		func(e *RuntimeEvidence) { e.Artifacts["test.log"] = validation.Digest([]byte("changed")) },
		func(e *RuntimeEvidence) { e.Artifacts["../outside"] = validation.Digest(logs) },
		func(e *RuntimeEvidence) { e.RunnerSHA256 = validation.Digest([]byte("unprovided")) },
	} {
		var altered RuntimeEvidence
		json.Unmarshal(raw, &altered)
		mut(&altered)
		bad, _ := json.Marshal(altered)
		if _, err := p.BindRuntime(t.Context(), pkg, mr, bad, validation.Digest(bad), out); err == nil {
			t.Fatal("bad runtime record accepted")
		}
	}
}

func TestOpaqueTestIDsKeepCase(t *testing.T) {
	var o Observation
	raw := []byte(`{"tests":{"test[A]":"PASSED","test[a]":"FAILED","test[證據]":"PASSED"},"files":{},"log_files":[]}`)
	if err := decode(raw, &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Tests) != 3 || o.Tests["test[A]"] == o.Tests["test[a]"] {
		t.Fatal("opaque test identities collapsed")
	}
	if err := decode([]byte(`{"Tests":{},"files":{},"log_files":[]}`), &o); err == nil {
		t.Fatal("struct alias accepted")
	}
	if err := decode([]byte(`{"tests":{"same":"PASSED","same":"FAILED"},"files":{},"log_files":[]}`), &o); err == nil {
		t.Fatal("duplicate key accepted")
	}
}
