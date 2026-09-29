package sat_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/sat"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func externalRunner(t *testing.T) *sat.Runner {
	t.Helper()
	solver, checker := os.Getenv("POUCH_TEST_CADICAL"), os.Getenv("POUCH_TEST_DRAT_TRIM")
	if solver == "" || checker == "" {
		t.Skip("set POUCH_TEST_CADICAL and POUCH_TEST_DRAT_TRIM with their SHA256 pins for real tool integration")
	}
	runner, err := sat.New(sat.Config{SolverPath: solver, SolverSHA256: os.Getenv("POUCH_TEST_CADICAL_SHA256"), CheckerPath: checker, CheckerSHA256: os.Getenv("POUCH_TEST_DRAT_TRIM_SHA256"), ArtifactDir: filepath.Join(t.TempDir(), "evidence")})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}
func TestPinnedToolsIntegration(t *testing.T) {
	runner := externalRunner(t)
	c := validation.Contract{SchemaVersion: validation.Version, RequestID: "real-two-orders", SourceBinding: map[string]string{"synthetic:rules": strings.Repeat("a", 64)}, Assumptions: map[string]string{}, Model: validation.Model{Fields: map[string][]string{"a": {"0", "1"}, "b": {"0", "1"}}, Forbidden: []validation.State{}, Actions: []validation.Action{
		{ID: "a-on", Guard: validation.State{"a": "0"}, Effect: validation.State{"a": "1"}, Frame: []string{"b"}},
		{ID: "b-on", Guard: validation.State{"b": "0"}, Effect: validation.State{"b": "1"}, Frame: []string{"a"}},
		{ID: "a-off", Guard: validation.State{"a": "1"}, Effect: validation.State{"a": "0"}, Frame: []string{"b"}},
		{ID: "b-off", Guard: validation.State{"b": "1"}, Effect: validation.State{"b": "0"}, Frame: []string{"a"}},
	}}, Start: validation.State{"a": "0", "b": "0"}, Goal: validation.State{"a": "1", "b": "1"}, Baseline: validation.State{"a": "0", "b": "0"}, ForwardLimit: 3, ReturnLimit: 2}
	result, err := planning.Search(context.Background(), c, runner, planning.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Forward) != 2 || len(result.Returns) != 1 || result.Returns[0].Status != "RETURN_FOUND" {
		t.Fatalf("bad search result %+v", result)
	}
	satCount, unsatCount := 0, 0
	for _, q := range result.Queries {
		if !q.Verified {
			t.Fatalf("unverified query %+v", q)
		}
		if q.Status == "SAT" {
			satCount++
		} else {
			unsatCount++
		}
	}
	t.Logf("real pinned tools: %d SAT, %d verified UNSAT, two same-length forward paths and one shortest return", satCount, unsatCount)
}

// Optional migration fixtures are read only after search to compare the saved
// historical path set. Fixture packages never enter the planner or SAT encoder.
func TestExistingAuthorityFixtures(t *testing.T) {
	dir := os.Getenv("POUCH_TEST_AUTHORITY_DIR")
	if dir == "" {
		t.Skip("set POUCH_TEST_AUTHORITY_DIR for the two existing migration fixtures")
	}
	for _, name := range []string{"synthetic-key-rotation", "django-13344"} {
		t.Run(name, func(t *testing.T) {
			runner := externalRunner(t)
			raw, err := os.ReadFile(filepath.Join(dir, name, "authority.json"))
			if err != nil {
				t.Fatal(err)
			}
			authority, err := validation.NewAuthority(raw, validation.Digest(raw))
			if err != nil {
				t.Fatal(err)
			}
			var c validation.Contract
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			result, err := planning.Search(context.Background(), c, runner, planning.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Complete {
				t.Fatal("incomplete search")
			}
			got := []string{}
			for _, p := range result.Forward {
				got = append(got, strings.Join(p.Trace.Actions, "\x00"))
				certificate := validation.Package{SchemaVersion: validation.Version, RequestID: c.RequestID, AuthoritySHA256: validation.Digest(raw), SourceBinding: c.SourceBinding, Assumptions: c.Assumptions, Forward: p.Trace, ClosedStates: []validation.State{}}
				returned := result.Returns[p.ReturnIndex]
				if returned.Trace != nil {
					certificate.ReturnKind = "return"
					certificate.Return = returned.Trace
				} else {
					reach, err := authority.Reachable(context.Background(), returned.Endpoint, c.Baseline)
					if err != nil {
						t.Fatal(err)
					}
					if reach.Shortest >= 0 {
						t.Fatalf("existing fixture has return beyond declared bound: %d", reach.Shortest)
					}
					certificate.ReturnKind = "no_return"
					certificate.ClosedStates = reach.States
				}
				packageBytes, err := json.Marshal(certificate)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := authority.Validate(context.Background(), c.RequestID, packageBytes); err != nil {
					t.Fatal(err)
				}
			}
			files, err := filepath.Glob(filepath.Join(dir, name, "package-*.json"))
			if err != nil {
				t.Fatal(err)
			}
			want := []string{}
			for _, file := range files {
				raw, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var p validation.Package
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Fatal(err)
				}
				want = append(want, strings.Join(p.Forward.Actions, "\x00"))
			}
			slices.Sort(got)
			slices.Sort(want)
			if len(want) == 0 || !slices.Equal(got, want) {
				t.Fatalf("path-set mismatch: got %q want %q", got, want)
			}
			t.Logf("%d paths, %d endpoint return searches, %d solver queries; all original-authority certificates accepted", len(result.Forward), len(result.Returns), len(result.Queries))
		})
	}
}
