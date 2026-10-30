package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/automata"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func TestAutomatonPrepareWritesCheckedAuthority(t *testing.T) {
	zero := strings.Repeat("00", 2)
	spec, err := json.Marshal(automata.Spec{SchemaVersion: automata.SpecVersion, RequestID: "lattice", SourceBinding: map[string]string{"synthetic:rules": strings.Repeat("a", 64)}, Assumptions: map[string]string{}, Lattice: automata.Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, Start: zero, Goal: automata.Pattern{Mask: "0100", Value: "0000"}, Baseline: zero, ForwardLimit: 1, ReturnLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(path, spec, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if got := Run(context.Background(), []string{"automaton-prepare", "--spec", path, "--sha256", validation.Digest(spec)}, &out, &errs); got != 0 {
		t.Fatalf("exit %d: %s", got, errs.String())
	}
	if _, err := automata.NewAuthority(out.Bytes(), validation.Digest(out.Bytes())); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if got := Run(context.Background(), []string{"automaton-prepare", "--spec", path, "--sha256", strings.Repeat("0", 64)}, &out, &errs); got != 2 || out.Len() != 0 {
		t.Fatalf("exit %d, %d bytes", got, out.Len())
	}
}
