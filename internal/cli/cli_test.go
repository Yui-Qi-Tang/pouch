package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/ahe"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func TestPublicationStatusKeepsWriteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    ahe.Publication
		err  error
		want string
	}{
		{"success", ahe.Publication{}, nil, "ADMITTED_AND_READ_BACK"},
		{"prewrite", ahe.Publication{}, errors.New("approval mismatch"), "NOT_ADMITTED"},
		{"lostack", ahe.Publication{}, ahe.ErrDeliveryUnknown, "DELIVERY_UNKNOWN"},
		{"readback", ahe.Publication{Admission: json.RawMessage(`{}`)}, errors.New("readback failed"), "ADMISSION_RECEIVED_READBACK_INCOMPLETE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicationStatus(tc.p, tc.err); got != tc.want {
				t.Fatalf("%s != %s", got, tc.want)
			}
		})
	}
}
func TestCLIUsageDoesNotCreateOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "result")
	var out, errs bytes.Buffer
	if got := Run(context.Background(), []string{"solve", "--out", dir}, &out, &errs); got != 2 {
		t.Fatalf("exit %d", got)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output created: %v", err)
	}
}
func TestExternalApprovalRejectsUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(p, []byte(`{"request_id":"r","subject":"s","display_sha256":"d","package_sha256":"p","reason":"reviewed","automatic":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	var a ahe.Approval
	if err := configJSON(p, &a); err == nil {
		t.Fatal("unknown approval authority field accepted")
	}
}

func TestExternalApprovalRejectsAmbiguousKeys(t *testing.T) {
	for _, raw := range []string{`{"subject":"approved","subject":"different"}`, `{"subject":"approved","Subject":"different"}`} {
		if err := uniqueConfigKeys([]byte(raw)); err == nil {
			t.Fatal("ambiguous review key accepted")
		}
	}
	if err := uniqueConfigKeys([]byte(`{"subject":"x","nested":{"a":[1,2]}}`)); err != nil {
		t.Fatal(err)
	}
}

func pinnedTool(t *testing.T, body string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture requires POSIX")
	}
	path := filepath.Join(t.TempDir(), "tool")
	raw := []byte("#!/bin/sh\n" + body + "\n")
	if err := os.WriteFile(path, raw, 0700); err != nil {
		t.Fatal(err)
	}
	return path, validation.Digest(raw)
}

// A proof over --max-proof-bytes leaves the search inconclusive and names the
// limit, rather than the 64 MiB default deciding.
func TestSolveProofLimit(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "django-13344")
	authority := filepath.Join(fixture, "authority.json")
	raw, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	var contract validation.Contract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	solver, solverHash := pinnedTool(t, "printf '1 0\\n' > proof.drat; echo 's UNSATISFIABLE'; exit 20")
	checker, checkerHash := pinnedTool(t, "echo 's VERIFIED'; exit 0")
	dir := filepath.Join(t.TempDir(), "run")
	var out, errs bytes.Buffer
	Run(context.Background(), []string{"solve", "--authority", authority, "--authority-sha256", validation.Digest(raw), "--request-id", contract.RequestID, "--sources", filepath.Join(fixture, "sources.json"), "--solver", solver, "--solver-sha256", solverHash, "--checker", checker, "--checker-sha256", checkerHash, "--out", dir, "--max-proof-bytes", "3"}, &out, &errs)
	saved, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatalf("%v: %s", err, errs.String())
	}
	var bundle run.Bundle
	if err := json.Unmarshal(saved, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Search.Error != "resolver limit exceeded: proof size" || bundle.Complete {
		t.Fatalf("search %q, complete %v", bundle.Search.Error, bundle.Complete)
	}
}

func TestSearchTimeoutLeavesTimeToCertify(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "django-13344")
	authority := filepath.Join(fixture, "authority.json")
	raw, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	var contract validation.Contract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	solver, solverHash := pinnedTool(t, "sleep 30")
	checker, checkerHash := pinnedTool(t, "exit 1")
	dir := filepath.Join(t.TempDir(), "run")
	var out, errs bytes.Buffer
	got := Run(context.Background(), []string{"solve", "--authority", authority, "--authority-sha256", validation.Digest(raw), "--request-id", contract.RequestID, "--sources", filepath.Join(fixture, "sources.json"), "--solver", solver, "--solver-sha256", solverHash, "--checker", checker, "--checker-sha256", checkerHash, "--out", dir, "--timeout", "2s", "--query-timeout", "1m"}, &out, &errs)
	if got != 2 {
		t.Fatalf("exit %d: %s", got, errs.String())
	}
	saved, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle run.Bundle
	if err := json.Unmarshal(saved, &bundle); err != nil {
		t.Fatal(err)
	}
	// Certification runs after search, so a finished set check proves it had time.
	if bundle.Search.Error == "" || bundle.Complete || !bundle.SetCheck.Complete || slices.Contains(bundle.Diagnostics, "REFERENCE_REACHABILITY_INCOMPLETE") {
		t.Fatalf("search %q, complete %v, set check %+v, diagnostics %v", bundle.Search.Error, bundle.Complete, bundle.SetCheck, bundle.Diagnostics)
	}
}

// With --discard-sat-proof a verified SAT query keeps no proof, an UNSAT query
// keeps and checks its proof, and the result says so.
func TestSolveDiscardSATProof(t *testing.T) {
	solver, checker := os.Getenv("POUCH_TEST_CADICAL"), os.Getenv("POUCH_TEST_DRAT_TRIM")
	if solver == "" || checker == "" {
		t.Skip("set POUCH_TEST_CADICAL and POUCH_TEST_DRAT_TRIM with their SHA256 pins for real tool integration")
	}
	fixture := filepath.Join("..", "..", "testdata", "django-13344")
	authority := filepath.Join(fixture, "authority.json")
	raw, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	var contract validation.Contract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "run")
	var out, errs bytes.Buffer
	Run(context.Background(), []string{"solve", "--discard-sat-proof", "--authority", authority, "--authority-sha256", validation.Digest(raw), "--request-id", contract.RequestID, "--sources", filepath.Join(fixture, "sources.json"), "--solver", solver, "--solver-sha256", os.Getenv("POUCH_TEST_CADICAL_SHA256"), "--checker", checker, "--checker-sha256", os.Getenv("POUCH_TEST_DRAT_TRIM_SHA256"), "--out", dir}, &out, &errs)
	saved, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatalf("%v: %s", err, errs.String())
	}
	var bundle run.Bundle
	if err := json.Unmarshal(saved, &bundle); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.EvidenceScope, satProofNote) {
		t.Fatal(bundle.EvidenceScope)
	}
	sawSAT, sawUNSAT := false, false
	for _, q := range bundle.Search.Queries {
		_, statErr := os.Stat(filepath.Join(dir, "solver", q.ID, "proof.drat"))
		switch q.Status {
		case "SAT":
			sawSAT = true
			if statErr == nil {
				t.Fatalf("SAT query %s kept a proof", q.ID)
			}
		case "UNSAT":
			sawUNSAT = true
			if statErr != nil || !q.Verified {
				t.Fatalf("UNSAT query %s: %v verified=%v", q.ID, statErr, q.Verified)
			}
		}
	}
	if !sawSAT || !sawUNSAT {
		t.Skipf("fixture gave SAT=%v UNSAT=%v", sawSAT, sawUNSAT)
	}
}
