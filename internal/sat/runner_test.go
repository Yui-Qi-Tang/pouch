package sat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	calxsat "github.com/Yui-Qi-Tang/calx/sat"
)

func pinnedScript(t *testing.T, body string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture requires POSIX")
	}
	path := filepath.Join(t.TempDir(), "tool")
	raw := []byte("#!/bin/sh\n" + body + "\n")
	if err := os.WriteFile(path, raw, 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return path, hex.EncodeToString(hash[:])
}
func fixtureRunner(t *testing.T, solverBody, checkerBody string, timeout time.Duration) *Runner {
	t.Helper()
	solver, solverHash := pinnedScript(t, solverBody)
	checker, checkerHash := pinnedScript(t, checkerBody)
	r, err := New(Config{SolverPath: solver, SolverSHA256: solverHash, CheckerPath: checker, CheckerSHA256: checkerHash, ArtifactDir: filepath.Join(t.TempDir(), "evidence"), Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func tinyCNF() calxsat.CNF {
	return calxsat.CNF{Variables: []calxsat.Variable{{ID: 1}}, Clauses: [][]int{{1}}}
}
func TestCheckedSATAndArtifactIsolation(t *testing.T) {
	r := fixtureRunner(t, "echo 's SATISFIABLE'; echo 'v 1 0'; exit 10", "exit 1", 10*time.Second)
	result, err := r.Solve(context.Background(), "q0", tinyCNF())
	if err != nil || !result.Verified || result.Status != "SAT" {
		t.Fatalf("%+v %v", result, err)
	}
	if len(result.Artifacts) < 5 {
		t.Fatal("missing evidence")
	}
	for _, a := range result.Artifacts {
		if filepath.IsAbs(a.ID) || strings.Contains(a.ID, "..") {
			t.Fatal(a.ID)
		}
	}
	if _, err = r.Solve(context.Background(), "q0", tinyCNF()); err == nil {
		t.Fatal("overwrote query")
	}
	if _, err = r.Solve(context.Background(), "../escape", tinyCNF()); err == nil {
		t.Fatal("accepted traversal")
	}
}
func TestRejectWrongAssignmentAndCheckerExit(t *testing.T) {
	r := fixtureRunner(t, "echo 's SATISFIABLE'; echo 'v -1 0'; exit 10", "exit 1", 10*time.Second)
	result, err := r.Solve(context.Background(), "wrong", tinyCNF())
	if err == nil || result.Verified {
		t.Fatal("accepted invalid assignment")
	}
	r = fixtureRunner(t, "touch proof.drat; echo 's UNSATISFIABLE'; exit 20", "echo 's VERIFIED'; exit 1", 10*time.Second)
	result, err = r.Solve(context.Background(), "wrong", tinyCNF())
	if err == nil || result.Verified {
		t.Fatal("accepted checker VERIFIED with exit 1")
	}
}
func TestTimeoutKillsToolGroupAndPreservesEvidence(t *testing.T) {
	r := fixtureRunner(t, "sleep 30; echo 's SATISFIABLE'; exit 10", "exit 1", 30*time.Millisecond)
	start := time.Now()
	result, err := r.Solve(context.Background(), "timeout", tinyCNF())
	if !errors.Is(err, context.DeadlineExceeded) || result.Verified {
		t.Fatalf("%+v %v", result, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("process cancellation did not complete")
	}
	if len(result.Artifacts) < 5 {
		t.Fatal("missing timeout artifacts")
	}
}
func TestPinsAndPrivateCopy(t *testing.T) {
	solver, hash := pinnedScript(t, "echo 's SATISFIABLE'; echo 'v 1 0'; exit 10")
	checker, checkHash := pinnedScript(t, "exit 1")
	cfg := Config{SolverPath: solver, SolverSHA256: strings.Repeat("0", 64), CheckerPath: checker, CheckerSHA256: checkHash, ArtifactDir: filepath.Join(t.TempDir(), "evidence")}
	if _, err := New(cfg); err == nil {
		t.Fatal("accepted wrong hash")
	}
	cfg.SolverSHA256 = hash
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(solver, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err := r.Solve(context.Background(), "private", tinyCNF())
	if err != nil || !result.Verified {
		t.Fatalf("caller edit changed pinned copy: %v", err)
	}
}
func TestUnknownAndUnverifiedResultsHaveNoAssignment(t *testing.T) {
	r := fixtureRunner(t, "echo 's UNKNOWN'; exit 0", "exit 1", 10*time.Second)
	result, err := r.Solve(context.Background(), "unknown", tinyCNF())
	if !errors.Is(err, calxsat.ErrInconclusive) || result.Status != "INCONCLUSIVE" || result.Verified || result.Assignment != nil {
		t.Fatalf("%+v %v", result, err)
	}
	r = fixtureRunner(t, "echo 's SATISFIABLE'; echo 'v -1 0'; exit 10", "exit 1", 10*time.Second)
	result, err = r.Solve(context.Background(), "wrong", tinyCNF())
	if !errors.Is(err, calxsat.ErrValidation) || result.Status != "SAT" || result.Verified || result.Assignment != nil {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestQueryErrorsOmitHostPaths(t *testing.T) {
	r := fixtureRunner(t, "echo 's UNSATISFIABLE'; exit 20", "exit 1", 10*time.Second)
	_, err := r.Solve(context.Background(), "no-proof", tinyCNF())
	if !errors.Is(err, calxsat.ErrProtocol) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), string(filepath.Separator)) {
		t.Fatalf("error names a path: %v", err)
	}
	if got := portable(fmt.Errorf("query directory must be new: %w", os.ErrExist)); strings.Contains(got.Error(), "file exists") {
		t.Fatalf("uncategorized cause kept: %v", got)
	}
}

// A formula over a Calx limit must reach planning as the limit category, which
// keeps the query inconclusive instead of reading as UNSAT or a tool fault.
func TestFormulaLimitKeepsItsCategory(t *testing.T) {
	solver, solverHash := pinnedScript(t, "exit 1")
	checker, checkerHash := pinnedScript(t, "exit 1")
	r, err := New(Config{SolverPath: solver, SolverSHA256: solverHash, CheckerPath: checker, CheckerSHA256: checkerHash, ArtifactDir: filepath.Join(t.TempDir(), "evidence"), MaxClauses: 1})
	if err != nil {
		t.Fatal(err)
	}
	formula := calxsat.CNF{Variables: []calxsat.Variable{{ID: 1}}, Clauses: [][]int{{1}, {1}}}
	got, err := r.Solve(context.Background(), "q000000", formula)
	if !errors.Is(err, calxsat.ErrLimit) || got.Verified || got.Status != "INCONCLUSIVE" || got.Assignment != nil {
		t.Fatalf("%+v %v", got, err)
	}
}
