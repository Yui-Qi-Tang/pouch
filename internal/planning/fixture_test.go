package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/calx/finite"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func fixtureContract(t *testing.T, name string) (validation.Contract, *validation.Authority) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name, "authority.json"))
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
	return c, authority
}

// TestFrozenKeyRotationFormulas keeps Pouch queries byte-identical to formulas
// saved before the encoder moved to Calx. A pin that changes them changes the
// evidence of every run, so it must be a deliberate decision.
func TestFrozenKeyRotationFormulas(t *testing.T) {
	c, _ := fixtureContract(t, "synthetic-key-rotation")
	opts, err := Options{}.defaults()
	if err != nil {
		t.Fatal(err)
	}
	for horizon, want := range map[int]string{
		0: "17fe3a6ddeb77980d8d6a7dd5f77b3e17aca99662f6dcc6f8bc823efdac527d8",
		5: "be5c89c274b2b10922e2ecbc4a660897c281639f5e2c658e3d7bcf4d4bc70373",
	} {
		enc, err := encode(t.Context(), finiteModel(c.Model), c.Start, c.Goal, horizon, nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		formula, err := enc.CNF()
		if err != nil {
			t.Fatal(err)
		}
		var dimacs strings.Builder
		if err := formula.WriteDIMACS(&dimacs); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(dimacs.String()))
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("horizon %d: got %s, want %s", horizon, got, want)
		}
	}
}

// TestProjectMatchesEncoder keeps the matrices saved in bundles equal to the
// rules that Calx encodes, because Pouch projects them separately.
func TestProjectMatchesEncoder(t *testing.T) {
	synthetic, _ := fixtureContract(t, "synthetic-key-rotation")
	django, _ := fixtureContract(t, "django-13344")
	for _, c := range []validation.Contract{twoBits(), synthetic, django} {
		t.Run(c.RequestID, func(t *testing.T) {
			got, err := Project(c)
			if err != nil {
				t.Fatal(err)
			}
			want, err := finite.Project(finiteModel(c.Model))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Fields, want.Fields) || !reflect.DeepEqual(got.ActionIDs, want.ActionIDs) || !reflect.DeepEqual(got.Guard, want.Guard) || !reflect.DeepEqual(got.Effect, want.Effect) || !reflect.DeepEqual(got.Frame, want.Frame) || len(got.Coordinates) != len(want.Coordinates) {
				t.Fatalf("matrix mismatch:\n%+v\n%+v", got, want)
			}
			for i, coordinate := range got.Coordinates {
				if coordinate.Field != want.Coordinates[i].Field || coordinate.Value != want.Coordinates[i].Value {
					t.Fatalf("coordinate %d: %+v, want %+v", i, coordinate, want.Coordinates[i])
				}
			}
		})
	}
}

// TestSearchReplaysOriginalRules runs Search end to end on the migration
// fixtures and checks every result against the original rules, which share no
// code with the encoder, and against the path sets saved with each fixture.
func TestSearchReplaysOriginalRules(t *testing.T) {
	for _, name := range []string{"synthetic-key-rotation", "django-13344"} {
		t.Run(name, func(t *testing.T) {
			c, authority := fixtureContract(t, name)
			result, err := Search(t.Context(), c, truthSolver{}, Options{})
			if err != nil || !result.Complete {
				t.Fatalf("%v %+v", err, result.Status)
			}
			got := []string{}
			for _, path := range result.Forward {
				if _, err := authority.ReplayForward(t.Context(), path.Trace); err != nil {
					t.Fatalf("%s: %v", path.ID, err)
				}
				got = append(got, strings.Join(path.Trace.Actions, "\x00"))
			}
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name, "historical-paths.json"))
			if err != nil {
				t.Fatal(err)
			}
			var historical []struct {
				Actions []string `json:"actions"`
			}
			if err := json.Unmarshal(raw, &historical); err != nil {
				t.Fatal(err)
			}
			want := []string{}
			for _, path := range historical {
				want = append(want, strings.Join(path.Actions, "\x00"))
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("path sets differ: got %q, want %q", got, want)
			}
			for _, back := range result.Returns {
				reach, err := authority.Reachable(t.Context(), back.Endpoint, c.Baseline)
				if err != nil {
					t.Fatal(err)
				}
				if back.Status != "RETURN_FOUND" {
					if back.Status != "NO_RETURN_WITHIN_BOUND" || (reach.Witness != nil && reach.Shortest <= c.ReturnLimit) {
						t.Fatalf("%s, original rules give shortest %d", back.Status, reach.Shortest)
					}
					continue
				}
				if len(back.Trace.Actions) != reach.Shortest {
					t.Fatalf("return costs %d, original rules give %d", len(back.Trace.Actions), reach.Shortest)
				}
				reverse := c
				reverse.Start, reverse.Goal, reverse.ForwardLimit = back.Endpoint, c.Baseline, c.ReturnLimit
				raw, err := json.Marshal(reverse)
				if err != nil {
					t.Fatal(err)
				}
				replayer, err := validation.NewAuthority(raw, validation.Digest(raw))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := replayer.ReplayForward(t.Context(), *back.Trace); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
