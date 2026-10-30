package automata

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/calx/lga"
	"github.com/Yui-Qi-Tang/calx/sat"
	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// truthSolver is a test-only propositional search. Production uses pinned
// external solvers.
type truthSolver struct{ calls *int }

func (s truthSolver) Solve(_ context.Context, _ string, c sat.CNF) (planning.SolveResult, error) {
	if s.calls != nil {
		*s.calls++
	}
	values, ok := solveTruth(c.Clauses, map[int]bool{}, len(c.Variables))
	if !ok {
		return planning.SolveResult{Status: "UNSAT", Verified: true}, nil
	}
	return planning.SolveResult{Status: "SAT", Verified: true, Assignment: values}, nil
}

func solveTruth(clauses [][]int, values map[int]bool, n int) (map[int]bool, bool) {
	for changed := true; changed; {
		changed = false
		for _, clause := range clauses {
			satisfied, open, last := false, 0, 0
			for _, lit := range clause {
				value, known := values[max(lit, -lit)]
				if !known {
					open, last = open+1, lit
				} else if value == (lit > 0) {
					satisfied = true
					break
				}
			}
			if satisfied {
				continue
			}
			if open == 0 {
				return nil, false
			}
			if open == 1 {
				values[max(last, -last)], changed = last > 0, true
			}
		}
	}
	for id := 1; id <= n; id++ {
		if _, known := values[id]; known {
			continue
		}
		for _, value := range []bool{false, true} {
			next := map[int]bool{id: value}
			for k, v := range values {
				next[k] = v
			}
			if solved, ok := solveTruth(clauses, next, n); ok {
				return solved, true
			}
		}
		return nil, false
	}
	return values, true
}

// longCycleStart returns a 2×2 state on a 12-step cycle.
func longCycleStart(t *testing.T, h lga.HPP) automaton.State {
	t.Helper()
	r := rand.New(rand.NewPCG(9, 10))
	for range 100000 {
		start := randomState(r, h.Cells()*h.Bits())
		twelve, err := h.Run(start, 12)
		if err != nil {
			t.Fatal(err)
		}
		short := false
		for _, d := range []int{1, 2, 3, 4, 6} {
			state, err := h.Run(start, d)
			if err != nil {
				t.Fatal(err)
			}
			short = short || equal(state, start)
		}
		if equal(twelve, start) && !short {
			return start
		}
	}
	t.Fatal("no 12-step cycle found")
	return nil
}

func exactGoal(state automaton.State) Pattern {
	mask := make([]bool, len(state))
	for i := range mask {
		mask[i] = true
	}
	return Pattern{Mask: encodeBits(mask), Value: encodeBits(state)}
}

func certified(t *testing.T, a *Authority, opts Options) (Result, Bundle, error) {
	t.Helper()
	result, err := Search(context.Background(), a, truthSolver{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := Certify(context.Background(), a, strings.Repeat("a", 64), run.SourceManifest{}, result)
	return result, bundle, err
}

// On a 2×2 HPP lattice every run is a cycle. With the goal k steps along a cycle
// of length L, the search must report k forward and L-k back: a return with no
// inverse rule, found by continuing around.
func TestReturnCostMatchesOrbit(t *testing.T) {
	h := lga.HPP{Width: 2, Height: 2}
	r := rand.New(rand.NewPCG(7, 8))
	seen := map[int]bool{}
	for len(seen) < 3 {
		start := randomState(r, 16)
		cycle := 0
		for cur := start; ; {
			next, err := h.Run(cur, 1)
			if err != nil {
				t.Fatal(err)
			}
			cur, cycle = next, cycle+1
			if equal(cur, start) {
				break
			}
		}
		if cycle < 2 || seen[cycle] {
			continue
		}
		seen[cycle] = true
		for k := 1; k < cycle; k++ {
			goal, err := h.Run(start, k)
			if err != nil {
				t.Fatal(err)
			}
			s := testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start)
			s.Goal = exactGoal(goal)
			s.ForwardLimit, s.ReturnLimit = cycle, cycle
			_, a := prepare(t, s)
			result, b, err := certified(t, a, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Deterministic || len(result.Forward.Choices) != k || b.ReturnSteps == nil || *b.ReturnSteps != cycle-k || b.RecoveryStatus != "RETURN_VERIFIED" || !b.Complete || b.Status != "FOUND" {
				t.Fatalf("L=%d k=%d: %+v %+v", cycle, k, result, b)
			}
		}
	}
}

func TestReturnBeyondBoundIsNotImpossible(t *testing.T) {
	h := lga.HPP{Width: 2, Height: 2}
	start := longCycleStart(t, h)
	goal, err := h.Run(start, 1)
	if err != nil {
		t.Fatal(err)
	}
	s := testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start)
	s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(goal), 1, 0
	_, a := prepare(t, s)
	result, b, err := certified(t, a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Return.Status != "NO_RETURN_WITHIN_BOUND" || b.RecoveryStatus != "UNKNOWN" || b.Reason != "RETURN_EXISTS_BEYOND_SEARCH_BOUND" || b.ReturnSteps == nil || b.Complete {
		t.Fatalf("%+v", b)
	}
}

// merge is an irreversible two-cell rule: each cell becomes the OR of both. Once
// both bits are set the run cannot leave that state.
func merge(start, baseline, goal string, forward, back int) []byte {
	raw, _ := json.Marshal(Contract{SchemaVersion: Version, RequestID: "merge", SourceBinding: map[string]string{"synthetic:rules": strings.Repeat("a", 64)}, Assumptions: map[string]string{}, Model: Model{Bits: 1, Rules: []Rule{{Inputs: 2, Table: "0e"}}, Cells: []Cell{{Rule: 0, Inputs: []int{0, 1}}, {Rule: 0, Inputs: []int{0, 1}}}}, Start: start, Goal: Pattern{Mask: "03", Value: goal}, Baseline: baseline, ForwardLimit: forward, ReturnLimit: back})
	return raw
}

func TestIrreversibleRuleProvesNoReturn(t *testing.T) {
	raw := merge("01", "01", "03", 3, 3)
	a, err := NewAuthority(raw, validation.Digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	result, b, err := certified(t, a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Forward.Choices) != 1 || result.Return.Status != "NO_RETURN_WITHIN_BOUND" || b.RecoveryStatus != "NO_RETURN_IN_MODEL" || !b.Complete {
		t.Fatalf("%+v", b)
	}
}

func TestUnreachableGoal(t *testing.T) {
	raw := merge("01", "01", "02", 3, 3)
	a, err := NewAuthority(raw, validation.Digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	result, b, err := certified(t, a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "NO_PATH_WITHIN_BOUND" || b.Status != "NO_PATH_IN_MODEL" || !b.Complete || b.ForwardVerified {
		t.Fatalf("%+v", b)
	}
}

func TestOrbitLimitLeavesRecoveryUnknown(t *testing.T) {
	h := lga.HPP{Width: 2, Height: 2}
	start := longCycleStart(t, h)
	goal, _ := h.Run(start, 1)
	s := testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start)
	s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(goal), 1, 0
	_, a := prepare(t, s)
	_, b, err := certified(t, a, Options{MaxOrbitSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	if b.RecoveryStatus != "UNKNOWN" || b.Reason != "ORBIT_NOT_CLOSED" || b.Complete {
		t.Fatalf("%+v", b)
	}
}

func TestChoicesGiveOnlyAWitness(t *testing.T) {
	f := lga.FHP{Width: 2, Height: 2}
	start := make(automaton.State, f.Cells()*f.Bits())
	start[lga.HexE], start[lga.HexE+1] = true, true
	end, err := f.Run(start, 2, func(_, _ int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	s := testSpec(Lattice{Rule: "fhp", Width: 2, Height: 2, Walls: []int{}}, start)
	s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(end), 3, 0
	_, a := prepare(t, s)
	result, b, err := certified(t, a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deterministic || !b.ForwardVerified {
		t.Fatalf("%+v", b)
	}
	if b.RecoveryStatus == "NO_RETURN_IN_MODEL" {
		t.Fatal("a bounded search with choices proved no return")
	}
}

func TestSearchBudgetsAreInconclusive(t *testing.T) {
	h := lga.HPP{Width: 2, Height: 2}
	start := make(automaton.State, 16)
	start[lga.East] = true
	goal, _ := h.Run(start, 3)
	s := testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start)
	s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(goal), 3, 3
	_, a := prepare(t, s)
	for name, c := range map[string]struct {
		opts Options
		want error
	}{
		"queries":   {Options{MaxQueries: 1}, planning.ErrQueryLimit},
		"variables": {Options{MaxVariables: 1}, planning.ErrEncodingLimit},
		"clauses":   {Options{MaxClauses: 1}, planning.ErrEncodingLimit},
		"literals":  {Options{MaxLiterals: 1}, planning.ErrEncodingLimit},
	} {
		result, err := Search(context.Background(), a, truthSolver{}, c.opts)
		if !errors.Is(err, c.want) || result.Complete || result.Status != "INCONCLUSIVE" {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
		b, err := Certify(context.Background(), a, strings.Repeat("a", 64), run.SourceManifest{}, result)
		if err != nil || b.Complete || b.Status != "INCONCLUSIVE" {
			t.Fatalf("%s: %+v %v", name, b, err)
		}
	}
}

func TestCertifyRejectsFalseClaims(t *testing.T) {
	h := lga.HPP{Width: 2, Height: 2}
	start := longCycleStart(t, h)
	goal, _ := h.Run(start, 2)
	s := testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start)
	s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(goal), 12, 12
	_, a := prepare(t, s)
	honest, err := Search(context.Background(), a, truthSolver{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	check := func(name string, r Result) {
		t.Helper()
		if b, err := Certify(context.Background(), a, digest, run.SourceManifest{}, r); err == nil || b.Status != "REJECTED" {
			t.Fatalf("%s accepted: %+v", name, b)
		}
	}
	bad := honest
	forward := *honest.Forward
	forward.States = append([]string{}, forward.States...)
	forward.States[1] = strings.Repeat("0", len(forward.States[1]))
	bad.Forward = &forward
	check("tampered state", bad)
	bad = honest
	bad.Forward, bad.Return = nil, nil
	bad.Status = "NO_PATH_WITHIN_BOUND"
	check("false forward exhaustion", bad)
	bad = honest
	bad.Return = &Return{Status: "NO_RETURN_WITHIN_BOUND", QueryIDs: []string{}}
	check("false return exhaustion", bad)
	bad = honest
	loop := *honest.Forward
	loop.States = append(append([]string{}, loop.States...), loop.States[1:]...)
	loop.Choices = append(append([]string{}, loop.Choices...), loop.Choices...)
	bad.Forward = &loop
	check("not shortest", bad)
}

func TestSearchStopsOnSolverFailure(t *testing.T) {
	_, a := prepare(t, testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, make(automaton.State, 16)))
	result, err := Search(context.Background(), a, failing{}, Options{})
	if err == nil || result.Status != "INCONCLUSIVE" || len(result.Queries) != 1 || result.Queries[0].Error == "" {
		t.Fatalf("%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Search(ctx, a, truthSolver{}, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type failing struct{}

func (failing) Solve(context.Context, string, sat.CNF) (planning.SolveResult, error) {
	return planning.SolveResult{Status: "INCONCLUSIVE"}, errors.New("tool execution failed")
}

// reachable returns the shortest step count from start to every state, trying
// every choice at every step. It is the oracle for systems with choices, where
// no single run can be walked.
func reachable(a *Authority, start automaton.State, limit int) map[string]int {
	distance := map[string]int{encodeBits(start): 0}
	frontier := []automaton.State{start}
	combinations := 1 << a.system.totalChoices
	for depth := 1; depth <= limit && len(frontier) > 0; depth++ {
		var next []automaton.State
		for _, state := range frontier {
			for c := range combinations {
				choices := make([]bool, a.system.totalChoices)
				for i := range choices {
					choices[i] = c>>i&1 == 1
				}
				following := a.system.step(state, choices)
				if _, seen := distance[encodeBits(following)]; !seen {
					distance[encodeBits(following)] = depth
					next = append(next, following)
				}
			}
		}
		frontier = next
	}
	return distance
}

func TestSearchWithChoicesMatchesExhaustiveReachability(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	lattice := Lattice{Rule: "fhp", Width: 2, Height: 2, Walls: []int{}}
	checked, returned, missing := 0, 0, 0
	for attempt := 0; attempt < 400 && checked < 40; attempt++ {
		start := make(automaton.State, 24)
		for i := range start {
			start[i] = r.Float64() < 0.25
		}
		_, probe := prepare(t, testSpec(lattice, start))
		distance := reachable(probe, start, 5)
		var goal string
		cost := 0
		for state, d := range distance {
			if d > cost || (d == cost && state < goal) {
				goal, cost = state, d
			}
		}
		if cost == 0 {
			continue
		}
		state, ok := decodeBits(goal, 24)
		if !ok {
			t.Fatal(goal)
		}
		back, found := reachable(probe, state, 6)[encodeBits(start)]
		s := testSpec(lattice, start)
		s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(state), 5, 6
		_, a := prepare(t, s)
		result, b, err := certified(t, a, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Deterministic || result.Forward == nil || len(result.Forward.Choices) != cost || !b.ForwardVerified {
			t.Fatalf("forward: want %d, got %+v", cost, b)
		}
		switch {
		case found:
			returned++
			if b.ReturnSteps == nil || *b.ReturnSteps != back || b.RecoveryStatus != "RETURN_VERIFIED" {
				t.Fatalf("return: want %d, got %+v", back, b)
			}
		default:
			missing++
			if result.Return.Status != "NO_RETURN_WITHIN_BOUND" || b.RecoveryStatus != "UNKNOWN" || b.Reason != "CHOICES_NOT_ENUMERATED" {
				t.Fatalf("no return: got %+v", b)
			}
		}
		checked++
	}
	if checked < 40 || returned == 0 {
		t.Fatalf("checked %d, returned %d, no return %d", checked, returned, missing)
	}
	t.Logf("checked %d: %d returns, %d with no return within the limit", checked, returned, missing)
}
