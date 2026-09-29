package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Exercise the real process exit and JSON protocol; a receipt alone is not success.
func TestCLI(t *testing.T) {
	zero, one := validation.State{"x": "0"}, validation.State{"x": "1"}
	contract := validation.Contract{
		SchemaVersion: validation.Version, RequestID: "local-request",
		SourceBinding: map[string]string{"file:fixture": validation.Digest([]byte("declared fixture"))},
		Assumptions:   map[string]string{},
		Model: validation.Model{
			Fields: map[string][]string{"x": {"0", "1"}}, Forbidden: []validation.State{},
			Actions: []validation.Action{
				{ID: "set", Guard: zero, Effect: one, Frame: []string{}},
				{ID: "reset", Guard: one, Effect: zero, Frame: []string{}},
			},
		},
		Start: zero, Goal: one, Baseline: zero, ForwardLimit: 1, ReturnLimit: 1,
	}
	authority, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	digest := validation.Digest(authority)
	path := filepath.Join(t.TempDir(), "authority.json")
	if err := os.WriteFile(path, authority, 0600); err != nil {
		t.Fatal(err)
	}
	packet := validation.Package{
		SchemaVersion: validation.Version, RequestID: contract.RequestID,
		AuthoritySHA256: digest, SourceBinding: contract.SourceBinding, Assumptions: contract.Assumptions,
		Forward:      validation.Trace{States: []validation.State{zero, one}, Actions: []string{"set"}, ClaimShortest: true},
		ReturnKind:   "return",
		Return:       &validation.Trace{States: []validation.State{one, zero}, Actions: []string{"reset"}, ClaimShortest: true},
		ClosedStates: []validation.State{},
	}
	body, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, request, file, status, reason string
		body                                []byte
		exit                                int
	}{
		{name: "portable source accepted", request: contract.RequestID, file: path, body: body, status: "ACCEPTED", exit: 0},
		{name: "caller selects another request", request: "another", file: path, body: body, status: "REJECTED", reason: "CALLER_REQUEST_MISMATCH", exit: 1},
		{name: "bad package", request: contract.RequestID, file: path, body: []byte(`{"request_id":null}`), status: "REJECTED", reason: "JSON_NULL", exit: 1},
		{name: "source unavailable", request: contract.RequestID, file: path + ".missing", body: body, status: "INCONCLUSIVE", reason: "INFRASTRUCTURE", exit: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCLIProcess$", "--",
				"--authority", tt.file, "--authority-sha256", digest, "--request-id", tt.request)
			cmd.Env = append(os.Environ(), "POUCH_CLI_TEST_PROCESS=1")
			cmd.Stdin = bytes.NewReader(tt.body)
			var out, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &stderr
			err := cmd.Run()
			exit := 0
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					exit = ee.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != tt.exit {
				t.Fatalf("exit=%d, want=%d; stderr=%s", exit, tt.exit, stderr.String())
			}
			var response struct {
				Status  string              `json:"status"`
				Reason  string              `json:"reason"`
				Receipt *validation.Receipt `json:"receipt"`
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatalf("invalid JSON: %v; stdout=%s", err, out.String())
			}
			if response.Status != tt.status || response.Reason != tt.reason {
				t.Fatalf("unexpected response: %+v", response)
			}
			if tt.exit == 0 {
				r := response.Receipt
				if r == nil || r.AuthoritySHA256 != digest || r.PackageSHA256 != validation.Digest(body) || r.RequestID != contract.RequestID || r.ForwardCost != 1 || r.ReturnCost != 1 || r.ObservedFact || r.NativeAHEReceipt {
					t.Fatalf("receipt lost identity or claim boundary: %+v", r)
				}
			} else if response.Receipt != nil {
				t.Fatal("failed validation emitted an accepted receipt")
			}
		})
	}
}

func TestCLIProcess(t *testing.T) {
	if os.Getenv("POUCH_CLI_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"pouch-validate"}, os.Args[i+1:]...)
			flag.CommandLine = flag.NewFlagSet("pouch-validate", flag.ExitOnError)
			os.Exit(run())
		}
	}
	t.Fatal("missing child argument separator")
}
