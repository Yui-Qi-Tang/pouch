package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func TestSolveRejectsGraphBeforeStartingSolver(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "synthetic-key-rotation")
	raw, err := os.ReadFile(filepath.Join(base, "authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c validation.Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	graph := []byte(`{"schema_version":"canonical-evidence-graph/v1","snapshot_id":"wrong","nodes":[],"edges":[]}`)
	graphFile := filepath.Join(dir, "graph.json")
	if err := os.WriteFile(graphFile, graph, 0600); err != nil {
		t.Fatal(err)
	}
	script := []byte("#!/bin/sh\necho unexpected-tool-invocation\nexit 1\n")
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, script, 0700); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(dir, "result")
	var out, errs bytes.Buffer
	code := Run(t.Context(), []string{"solve", "--authority", filepath.Join(base, "authority.json"),
		"--authority-sha256", validation.Digest(raw), "--request-id", c.RequestID,
		"--sources", filepath.Join(base, "sources.json"), "--out", result,
		"--graph", graphFile, "--graph-sha256", validation.Digest(graph),
		"--solver", tool, "--solver-sha256", validation.Digest(script),
		"--checker", tool, "--checker-sha256", validation.Digest(script)}, &out, &errs)
	if code != 2 || !strings.Contains(errs.String(), "graph selected source") {
		t.Fatalf("exit=%d stderr=%s", code, errs.String())
	}
	for _, name := range []string{"solver", "bundle.json"} {
		if _, err := os.Stat(filepath.Join(result, name)); !os.IsNotExist(err) {
			t.Fatalf("preflight failure created %s: %v", name, err)
		}
	}
}
