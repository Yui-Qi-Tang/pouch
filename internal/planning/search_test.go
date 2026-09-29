package planning

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func twoBits() validation.Contract {
	return validation.Contract{SchemaVersion: validation.Version, RequestID: "two-bits", SourceBinding: map[string]string{"synthetic:rules": strings.Repeat("a", 64)}, Assumptions: map[string]string{}, Model: validation.Model{Fields: map[string][]string{"a": {"0", "1"}, "b": {"0", "1"}}, Forbidden: []validation.State{}, Actions: []validation.Action{
		{ID: "a-on", Guard: validation.State{"a": "0"}, Effect: validation.State{"a": "1"}, Frame: []string{"b"}},
		{ID: "b-on", Guard: validation.State{"b": "0"}, Effect: validation.State{"b": "1"}, Frame: []string{"a"}},
		{ID: "a-off", Guard: validation.State{"a": "1"}, Effect: validation.State{"a": "0"}, Frame: []string{"b"}},
		{ID: "b-off", Guard: validation.State{"b": "1"}, Effect: validation.State{"b": "0"}, Frame: []string{"a"}},
	}}, Start: validation.State{"a": "0", "b": "0"}, Goal: validation.State{"a": "1", "b": "1"}, Baseline: validation.State{"a": "0", "b": "0"}, ForwardLimit: 3, ReturnLimit: 2}
}

// This small exhaustive test oracle is deliberately test-only. Production uses
// external pinned solvers and never this recursive propositional evaluator.
type truthSolver struct{}

func (truthSolver) Solve(_ context.Context, _ string, c CNF) (SolveResult, error) {
	values, ok := solveTruth(c.Clauses, make(map[int]bool), len(c.Variables))
	if !ok {
		return SolveResult{Status: "UNSAT", Verified: true}, nil
	}
	return SolveResult{Status: "SAT", Verified: true, Assignment: values}, nil
}
func solveTruth(clauses [][]int, values map[int]bool, n int) (map[int]bool, bool) {
	for {
		changed := false
		for _, clause := range clauses {
			satisfied, unassigned, last := false, 0, 0
			for _, lit := range clause {
				id := lit
				if id < 0 {
					id = -id
				}
				value, ok := values[id]
				if !ok {
					unassigned++
					last = lit
				} else if value == (lit > 0) {
					satisfied = true
					break
				}
			}
			if satisfied {
				continue
			}
			if unassigned == 0 {
				return nil, false
			}
			if unassigned == 1 {
				id := last
				if id < 0 {
					id = -id
				}
				values[id] = last > 0
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	if len(values) == n {
		return values, true
	}
	var next int
	for next = 1; next <= n; next++ {
		if _, ok := values[next]; !ok {
			break
		}
	}
	for _, v := range []bool{false, true} {
		branch := maps.Clone(values)
		branch[next] = v
		if result, ok := solveTruth(clauses, branch, n); ok {
			return result, true
		}
	}
	return nil, false
}
func TestAllSameLengthPathsAndReturn(t *testing.T) {
	result, err := Search(context.Background(), twoBits(), truthSolver{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || !result.ForwardComplete || len(result.Forward) != 2 || len(result.Returns) != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	got := []string{}
	for _, p := range result.Forward {
		got = append(got, strings.Join(p.Trace.Actions, ","))
		if !p.Trace.ClaimShortest || p.ReturnIndex != 0 {
			t.Fatalf("bad path metadata %+v", p)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"a-on,b-on", "b-on,a-on"}) {
		t.Fatal(got)
	}
	ret := result.Returns[0]
	if ret.Status != "RETURN_FOUND" || len(ret.Trace.Actions) != 2 || !ret.Trace.ClaimShortest {
		t.Fatalf("bad return %+v", ret)
	}
	for _, q := range result.Queries {
		if !q.Verified {
			t.Fatal("unverified query")
		}
	}
}
func TestZeroStepAndExactPathBudget(t *testing.T) {
	c := twoBits()
	c.Goal = maps.Clone(c.Start)
	r, err := Search(context.Background(), c, truthSolver{}, Options{MaxPaths: 1})
	if err != nil || !r.Complete || len(r.Forward) != 1 || len(r.Forward[0].Trace.Actions) != 0 {
		t.Fatalf("zero step: %+v %v", r, err)
	}
	if len(r.Returns) != 1 || len(r.Returns[0].Trace.Actions) != 0 {
		t.Fatal("zero step return")
	}
	r, err = Search(context.Background(), twoBits(), truthSolver{}, Options{MaxPaths: 2})
	if err != nil || !r.Complete {
		t.Fatalf("exact path cap must allow exhaustion: %v", err)
	}
	r, err = Search(context.Background(), twoBits(), truthSolver{}, Options{MaxPaths: 1})
	if !errors.Is(err, ErrPathLimit) || r.Complete || len(r.Forward) != 1 || r.Status != "INCONCLUSIVE" {
		t.Fatalf("partial path cap: %+v %v", r, err)
	}
}
func TestBoundedReturnMissIsNotNoReturn(t *testing.T) {
	c := twoBits()
	c.ReturnLimit = 1
	r, err := Search(context.Background(), c, truthSolver{}, Options{})
	if err != nil || !r.Complete {
		t.Fatal(err)
	}
	if r.Returns[0].Status != "NO_RETURN_WITHIN_BOUND" || r.Returns[0].Trace != nil {
		t.Fatalf("false no-return: %+v", r.Returns)
	}
}
func TestLimitsAndCancellationRemainIncomplete(t *testing.T) {
	r, err := Search(context.Background(), twoBits(), truthSolver{}, Options{MaxQueries: 1})
	if !errors.Is(err, ErrQueryLimit) || r.Complete || len(r.Queries) != 1 {
		t.Fatalf("query cap: %+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = Search(ctx, twoBits(), truthSolver{}, Options{})
	if !errors.Is(err, context.Canceled) || r.Complete || len(r.Queries) != 0 {
		t.Fatalf("cancel: %+v %v", r, err)
	}
	c := twoBits()
	m, err := Project(c)
	if err != nil {
		t.Fatal(err)
	}
	opts, _ := Options{}.defaults()
	_, err = encode(context.Background(), m, c.Model.Forbidden, c.Start, c.Goal, 4095, nil, opts)
	if !errors.Is(err, ErrEncodingLimit) {
		t.Fatalf("large encoding must be rejected before allocation: %v", err)
	}
}
func TestMatrixRoundTripAndStableOrdering(t *testing.T) {
	c := twoBits()
	m, err := Project(c)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := m.Actions()
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(c.Model.Actions, func(a, b validation.Action) int { return strings.Compare(a.ID, b.ID) })
	if !reflect.DeepEqual(actions, c.Model.Actions) {
		t.Fatalf("round trip mismatch: %+v", actions)
	}
	slices.Reverse(c.Model.Actions)
	slices.Reverse(c.Model.Fields["a"])
	other, err := Project(c)
	if err != nil || !reflect.DeepEqual(m, other) {
		t.Fatalf("unstable projection: %v", err)
	}
}
func TestOneStepMatchesOriginalTruthTable(t *testing.T) {
	c := twoBits()
	c.Model.Forbidden = []validation.State{{"a": "1", "b": "1"}}
	m, err := Project(c)
	if err != nil {
		t.Fatal(err)
	}
	opts, _ := Options{}.defaults()
	states := []validation.State{{"a": "0", "b": "0"}, {"a": "0", "b": "1"}, {"a": "1", "b": "0"}, {"a": "1", "b": "1"}}
	for _, start := range states {
		for _, end := range states {
			for _, action := range c.Model.Actions {
				e, err := encode(context.Background(), m, c.Model.Forbidden, start, end, 1, nil, opts)
				if err != nil {
					t.Fatal(err)
				}
				for i, id := range m.ActionIDs {
					if id == action.ID {
						e.Clauses = append(e.Clauses, []int{e.actions[0][i]})
					}
				}
				result, err := truthSolver{}.Solve(context.Background(), "", e.CNF)
				if err != nil {
					t.Fatal(err)
				}
				valid := !(start["a"] == "1" && start["b"] == "1") && !(end["a"] == "1" && end["b"] == "1") && !maps.Equal(start, end)
				next := maps.Clone(start)
				for f, v := range action.Guard {
					valid = valid && start[f] == v
				}
				for f, v := range action.Effect {
					next[f] = v
				}
				valid = valid && maps.Equal(next, end)
				if (result.Status == "SAT") != valid {
					t.Fatalf("%v --%s--> %v: %s want %v", start, action.ID, end, result.Status, valid)
				}
			}
		}
	}
}

func TestDistinctActionsWithIdenticalStateTrace(t *testing.T) {
	c := twoBits()
	c.Model.Fields = map[string][]string{"x": {"0", "1"}}
	c.Model.Actions = []validation.Action{
		{ID: "first", Guard: validation.State{"x": "0"}, Effect: validation.State{"x": "1"}, Frame: []string{}},
		{ID: "second", Guard: validation.State{"x": "0"}, Effect: validation.State{"x": "1"}, Frame: []string{}},
		{ID: "undo", Guard: validation.State{"x": "1"}, Effect: validation.State{"x": "0"}, Frame: []string{}},
	}
	c.Start = validation.State{"x": "0"}
	c.Baseline = maps.Clone(c.Start)
	c.Goal = validation.State{"x": "1"}
	c.ForwardLimit = 1
	c.ReturnLimit = 1
	r, err := Search(context.Background(), c, truthSolver{}, Options{})
	if err != nil || !r.Complete || len(r.Forward) != 2 || len(r.Returns) != 1 {
		t.Fatalf("action identities collapsed: %+v %v", r, err)
	}
	if !reflect.DeepEqual(r.Forward[0].Trace.States, r.Forward[1].Trace.States) {
		t.Fatal("fixture should have identical state traces")
	}
}

func TestSimplePolicyRejectsLegalCycleBeforeGoal(t *testing.T) {
	c := twoBits()
	c.Model.Fields = map[string][]string{"x": {"0", "1", "2"}}
	c.Model.Actions = []validation.Action{
		{ID: "detour", Guard: validation.State{"x": "0"}, Effect: validation.State{"x": "1"}, Frame: []string{}},
		{ID: "back", Guard: validation.State{"x": "1"}, Effect: validation.State{"x": "0"}, Frame: []string{}},
		{ID: "finish", Guard: validation.State{"x": "0"}, Effect: validation.State{"x": "2"}, Frame: []string{}},
	}
	c.Start = validation.State{"x": "0"}
	c.Baseline = maps.Clone(c.Start)
	c.Goal = validation.State{"x": "2"}
	c.ForwardLimit = 3
	c.ReturnLimit = 1
	r, err := Search(context.Background(), c, truthSolver{}, Options{})
	if err != nil || !r.Complete || len(r.Forward) != 1 || !slices.Equal(r.Forward[0].Trace.Actions, []string{"finish"}) {
		t.Fatalf("cycle escaped simple policy: %+v %v", r, err)
	}
}
