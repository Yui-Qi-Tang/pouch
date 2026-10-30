package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/pouch/internal/automata"
	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
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

func TestSolveAutomatonRejectsInputsBeforeOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "result")
	var out, errs bytes.Buffer
	if got := Run(context.Background(), []string{"solve-automaton", "--out", dir}, &out, &errs); got != 2 {
		t.Fatalf("exit %d", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("created output: %v", err)
	}
}

// TestSolveAutomatonWithPinnedTools needs the tools of the sat integration test.
func TestSolveAutomatonWithPinnedTools(t *testing.T) {
	solver, checker := os.Getenv("POUCH_TEST_CADICAL"), os.Getenv("POUCH_TEST_DRAT_TRIM")
	if solver == "" || checker == "" {
		t.Skip("set POUCH_TEST_CADICAL and POUCH_TEST_DRAT_TRIM with their SHA256 pins for real tool integration")
	}
	dir := t.TempDir()
	source := []byte("synthetic rule\n")
	sourceDigest := validation.Digest(source)
	if err := os.WriteFile(filepath.Join(dir, "rule.txt"), source, 0600); err != nil {
		t.Fatal(err)
	}
	authority, err := json.Marshal(automata.Contract{SchemaVersion: automata.Version, RequestID: "merge", SourceBinding: map[string]string{"synthetic:rule": sourceDigest}, Assumptions: map[string]string{}, Model: automata.Model{Bits: 1, Rules: []automata.Rule{{Inputs: 2, Table: "0e"}}, Cells: []automata.Cell{{Rule: 0, Inputs: []int{0, 1}}, {Rule: 0, Inputs: []int{0, 1}}}}, Start: "01", Goal: automata.Pattern{Mask: "03", Value: "03"}, Baseline: "01", ForwardLimit: 2, ReturnLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	digest := validation.Digest(authority)
	manifest, err := json.Marshal(run.SourceManifest{SchemaVersion: "pouch-sources/v1", AuthoritySHA256: digest, RequestID: "merge", Sources: []run.Source{{ID: "synthetic:rule", File: "rule.txt", SHA256: sourceDigest}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"authority.json": authority, "sources.json": manifest} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	result := filepath.Join(dir, "result")
	var out, errs bytes.Buffer
	if got := Run(context.Background(), []string{"solve-automaton", "--authority", filepath.Join(dir, "authority.json"), "--authority-sha256", digest, "--request-id", "merge", "--sources", filepath.Join(dir, "sources.json"), "--out", result, "--solver", solver, "--solver-sha256", os.Getenv("POUCH_TEST_CADICAL_SHA256"), "--checker", checker, "--checker-sha256", os.Getenv("POUCH_TEST_DRAT_TRIM_SHA256")}, &out, &errs); got != 0 {
		t.Fatalf("exit %d: %s", got, errs.String())
	}
	raw, err := os.ReadFile(filepath.Join(result, "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle automata.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Status != "FOUND" || !bundle.ForwardVerified || bundle.RecoveryStatus != "NO_RETURN_IN_MODEL" || !bundle.Complete {
		t.Fatalf("%+v", bundle)
	}
}

func TestRenderSelectsTheAutomatonViewer(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(automata.Bundle{SchemaVersion: automata.BundleVersion, RequestID: "fixture", Diagnostics: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	bundle, target := filepath.Join(dir, "bundle.json"), filepath.Join(dir, "replay.html")
	if err := os.WriteFile(bundle, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if got := Run(context.Background(), []string{"render", "--bundle", bundle, "--out", target}, &out, &errs); got != 0 {
		t.Fatalf("exit %d: %s", got, errs.String())
	}
	page, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte("Pouch automaton replay")) {
		t.Fatal("wrong viewer")
	}
}

func TestAutomatonVerifyExitCodes(t *testing.T) {
	dir := t.TempDir()
	authority, err := json.Marshal(automata.Contract{SchemaVersion: automata.Version, RequestID: "merge", SourceBinding: map[string]string{"synthetic:rule": strings.Repeat("a", 64)}, Assumptions: map[string]string{}, Model: automata.Model{Bits: 1, Rules: []automata.Rule{{Inputs: 2, Table: "0e"}}, Cells: []automata.Cell{{Rule: 0, Inputs: []int{0, 1}}, {Rule: 0, Inputs: []int{0, 1}}}}, Start: "01", Goal: automata.Pattern{Mask: "03", Value: "03"}, Baseline: "01", ForwardLimit: 1, ReturnLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	digest := validation.Digest(authority)
	a, err := automata.NewAuthority(authority, digest)
	if err != nil {
		t.Fatal(err)
	}
	queries := []planning.Query{{Direction: "forward", Horizon: 0, Status: "UNSAT", Verified: true}, {Direction: "forward", Horizon: 1, Status: "SAT", Verified: true}, {Direction: "return", Horizon: 0, Status: "UNSAT", Verified: true}, {Direction: "return", Horizon: 1, Status: "UNSAT", Verified: true}}
	result := automata.Result{Policy: "shortest-run/" + automaton.Profile, Options: automata.Options{MaxOrbitSteps: 10}, ForwardLimit: 1, ReturnLimit: 1, Deterministic: true, Forward: &automata.Trace{States: []string{"01", "03"}, Choices: []string{""}}, Return: &automata.Return{Status: "NO_RETURN_WITHIN_BOUND", QueryIDs: []string{}}, Queries: queries, ForwardComplete: true, Complete: true, Status: "FOUND"}
	bundle, err := automata.Certify(context.Background(), a, digest, run.SourceManifest{AuthoritySHA256: digest, RequestID: "merge", Sources: []run.Source{}}, result)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	authorityFile, bundleFile, tampered := filepath.Join(dir, "authority.json"), filepath.Join(dir, "bundle.json"), filepath.Join(dir, "tampered.json")
	for name, content := range map[string][]byte{authorityFile: authority, bundleFile: raw, tampered: bytes.Replace(raw, []byte(`"NO_RETURN_IN_MODEL"`), []byte(`"RETURN_VERIFIED"`), 1)} {
		if err := os.WriteFile(name, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := func(file string) []string {
		return []string{"automaton-verify", "--authority", authorityFile, "--authority-sha256", digest, "--request-id", "merge", "--bundle", file}
	}
	var out, errs bytes.Buffer
	if got := Run(context.Background(), args(bundleFile), &out, &errs); got != 0 {
		t.Fatalf("exit %d: %s", got, errs.String())
	}
	if !strings.Contains(out.String(), automata.ReceiptVersion) {
		t.Fatal(out.String())
	}
	out.Reset()
	if got := Run(context.Background(), args(tampered), &out, &errs); got != 1 || out.Len() != 0 {
		t.Fatalf("tampered: exit %d, %d bytes", got, out.Len())
	}
	if got := Run(context.Background(), args(filepath.Join(dir, "missing.json")), &out, &errs); got != 2 {
		t.Fatalf("missing: exit %d", got)
	}
	if got := Run(context.Background(), []string{"automaton-verify"}, &out, &errs); got != 2 {
		t.Fatalf("usage: exit %d", got)
	}
}
