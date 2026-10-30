package automata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/calx/lga"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func randomState(r *rand.Rand, n int) automaton.State {
	s := make(automaton.State, n)
	for i := range s {
		s[i] = r.Float64() < 0.3
	}
	return s
}

func testSpec(l Lattice, start automaton.State) Spec {
	mask := make([]bool, len(start))
	mask[0] = true
	return Spec{SchemaVersion: SpecVersion, RequestID: "lattice", SourceBinding: map[string]string{"synthetic:rules": strings.Repeat("a", 64)}, Assumptions: map[string]string{}, Lattice: l, Start: encodeBits(start), Goal: Pattern{Mask: encodeBits(mask), Value: encodeBits(make([]bool, len(start)))}, Baseline: encodeBits(start), ForwardLimit: 3, ReturnLimit: 3}
}

func prepare(t *testing.T, s Spec) ([]byte, *Authority) {
	t.Helper()
	spec, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Prepare(spec, validation.Digest(spec))
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAuthority(raw, validation.Digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	return raw, a
}

// choice is a fixed pseudo-random schedule of FHP collision choices.
func choice(step, cell int) bool { return (step*31+cell*17)%5 < 2 }

func TestTablesMatchHPP(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, l := range []Lattice{{Rule: "hpp", Width: 1, Height: 1, Walls: []int{}}, {Rule: "hpp", Width: 3, Height: 1, Walls: []int{}}, {Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, {Rule: "hpp", Width: 5, Height: 3, Walls: []int{}}, {Rule: "hpp", Width: 8, Height: 8, Walls: []int{}}, {Rule: "hpp", Width: 6, Height: 4, Walls: []int{1, 7, 20}}} {
		h := lga.HPP{Width: l.Width, Height: l.Height}
		if len(l.Walls) > 0 {
			h.Walls = make([]bool, h.Cells())
			for _, c := range l.Walls {
				h.Walls[c] = true
			}
		}
		start := randomState(r, h.Cells()*h.Bits())
		_, a := prepare(t, testSpec(l, start))
		states, err := automaton.Simulate(a.System(), start, 40, nil)
		if err != nil {
			t.Fatal(err)
		}
		for step, state := range states {
			want, err := h.Run(start, step)
			if err != nil {
				t.Fatal(err)
			}
			if !equal(state, want) {
				t.Fatalf("%+v differs from lga.Run at step %d", l, step)
			}
		}
		if err := a.Replay(automaton.Trajectory{States: states, Choices: make([][]bool, 40)}); err != nil {
			t.Fatalf("%+v: %v", l, err)
		}
	}
}

func TestTablesMatchFHP(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for _, l := range []Lattice{{Rule: "fhp", Width: 2, Height: 1, Walls: []int{}}, {Rule: "fhp", Width: 3, Height: 3, Walls: []int{}}, {Rule: "fhp", Width: 7, Height: 5, Walls: []int{}}, {Rule: "fhp", Width: 6, Height: 4, Walls: []int{0, 9, 23}}} {
		f := lga.FHP{Width: l.Width, Height: l.Height}
		if len(l.Walls) > 0 {
			f.Walls = make([]bool, f.Cells())
			for _, c := range l.Walls {
				f.Walls[c] = true
			}
		}
		start := randomState(r, f.Cells()*f.Bits())
		_, a := prepare(t, testSpec(l, start))
		states, err := automaton.Simulate(a.System(), start, 40, func(step, cell int) []bool {
			if a.system.Choices(cell) == 0 {
				return nil
			}
			return []bool{choice(step, cell)}
		})
		if err != nil {
			t.Fatal(err)
		}
		choices := make([][]bool, 40)
		for step := range choices {
			choices[step] = []bool{}
			for cell := range f.Cells() {
				if a.system.Choices(cell) == 1 {
					choices[step] = append(choices[step], choice(step, cell))
				}
			}
		}
		for step, state := range states {
			want, err := f.Run(start, step, choice)
			if err != nil {
				t.Fatal(err)
			}
			if !equal(state, want) {
				t.Fatalf("%+v differs from lga.Run at step %d", l, step)
			}
		}
		if err := a.Replay(automaton.Trajectory{States: states, Choices: choices}); err != nil {
			t.Fatalf("%+v: %v", l, err)
		}
	}
}

func equal(a, b automaton.State) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Calx's encoding digest covers structure, rule tables and query, so an equal
// digest shows the tables and wiring encode exactly as the lattice gas does.
func TestEncodingMatchesLatticeGas(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	h := lga.HPP{Width: 4, Height: 3}
	start := randomState(r, h.Cells()*h.Bits())
	_, a := prepare(t, testSpec(Lattice{Rule: "hpp", Width: 4, Height: 3, Walls: []int{}}, start))
	f := lga.FHP{Width: 3, Height: 3}
	fstart := randomState(r, f.Cells()*f.Bits())
	_, fa := prepare(t, testSpec(Lattice{Rule: "fhp", Width: 3, Height: 3, Walls: []int{}}, fstart))
	for _, c := range []struct {
		lattice, table automaton.System
	}{{h, a.System()}, {f, fa.System()}} {
		query := automaton.Query{Steps: 3, Goal: automaton.Pattern{{Cell: 1, Index: 2}: true}}
		want, err := automaton.Compile(context.Background(), c.lattice, query)
		if err != nil {
			t.Fatal(err)
		}
		got, err := automaton.Compile(context.Background(), c.table, query)
		if err != nil {
			t.Fatal(err)
		}
		wantDigest, _ := want.Digest()
		gotDigest, _ := got.Digest()
		if wantDigest == "" || wantDigest != gotDigest {
			t.Fatalf("digest %q want %q", gotDigest, wantDigest)
		}
	}
}

func TestPrepareIsDeterministicAndBindsSpec(t *testing.T) {
	start := make(automaton.State, 16)
	start[3] = true
	s := testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start)
	first, _ := prepare(t, s)
	second, _ := prepare(t, s)
	if !bytes.Equal(first, second) {
		t.Fatal("same spec produced different bytes")
	}
	var c Contract
	if err := json.Unmarshal(first, &c); err != nil {
		t.Fatal(err)
	}
	spec, _ := json.Marshal(s)
	if c.Assumptions[specKey] != validation.Digest(spec) || len(c.Model.Rules) != 1 || len(c.Model.Cells) != 4 {
		t.Fatalf("%+v", c)
	}
	if _, err := Prepare(spec, strings.Repeat("0", 64)); !isRejection(err, "SPEC_HASH_MISMATCH") {
		t.Fatal(err)
	}
	s.Assumptions = map[string]string{specKey: "caller"}
	spec, _ = json.Marshal(s)
	if _, err := Prepare(spec, validation.Digest(spec)); !isRejection(err, "SPEC_CONTRACT") {
		t.Fatal(err)
	}
}

func TestPrepareRejectsLattice(t *testing.T) {
	start := make(automaton.State, 16)
	for code, l := range map[string]Lattice{
		"LATTICE_RULE":  {Rule: "rule110", Width: 2, Height: 2, Walls: []int{}},
		"LATTICE_BOUND": {Rule: "hpp", Width: 0, Height: 2, Walls: []int{}},
		"LATTICE_WALL":  {Rule: "hpp", Width: 2, Height: 2, Walls: []int{4}},
	} {
		spec, _ := json.Marshal(testSpec(l, start))
		if _, err := Prepare(spec, validation.Digest(spec)); !isRejection(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
	over := testSpec(Lattice{Rule: "hpp", Width: 1 << 11, Height: 1 << 10, Walls: []int{}}, start)
	spec, _ := json.Marshal(over)
	if _, err := Prepare(spec, validation.Digest(spec)); !isRejection(err, "LATTICE_BOUND") {
		t.Fatal(err)
	}
}

func isRejection(err error, code string) bool {
	var r *validation.Rejection
	return errors.As(err, &r) && r.Code == code
}

func TestAuthorityRejections(t *testing.T) {
	start := make(automaton.State, 16)
	raw, _ := prepare(t, testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start))
	cases := map[string]func(*Contract){
		"AUTHORITY_CONTRACT": func(c *Contract) { c.SchemaVersion = "pouch-automaton/v0" },
		"SOURCE_BINDING":     func(c *Contract) { c.SourceBinding["synthetic:rules"] = "bad" },
		"MODEL_BOUND":        func(c *Contract) { c.ForwardLimit = maxSteps + 1 },
		"RULE_FAN_IN":        func(c *Contract) { c.Model.Rules[0].Inputs = maxFanIn + 1 },
		"RULE_TABLE":         func(c *Contract) { c.Model.Rules[0].Table += "00" },
		"CELL_RULE":          func(c *Contract) { c.Model.Cells[0].Rule = 1 },
		"CELL_INPUT":         func(c *Contract) { c.Model.Cells[0].Inputs[0] = 16 },
		"STATE_ENCODING":     func(c *Contract) { c.Start = strings.ToUpper("ff") + c.Start[2:] },
		"GOAL_PATTERN":       func(c *Contract) { c.Goal = Pattern{Mask: "0000", Value: "0000"} },
	}
	for code, mutate := range cases {
		var c Contract
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		mutate(&c)
		changed, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewAuthority(changed, validation.Digest(changed)); !isRejection(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
	var c Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	c.Model.Cells[0].Inputs[1] = c.Model.Cells[0].Inputs[0]
	changed, _ := json.Marshal(c)
	if _, err := NewAuthority(changed, validation.Digest(changed)); !isRejection(err, "CELL_INPUT") {
		t.Fatalf("duplicate input: %v", err)
	}
	c.Model.Cells[0].Inputs = c.Model.Cells[0].Inputs[:3]
	changed, _ = json.Marshal(c)
	if _, err := NewAuthority(changed, validation.Digest(changed)); !isRejection(err, "CELL_RULE") {
		t.Fatalf("short inputs: %v", err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	c.Goal = Pattern{Mask: "0100", Value: "0200"}
	changed, _ = json.Marshal(c)
	if _, err := NewAuthority(changed, validation.Digest(changed)); !isRejection(err, "GOAL_PATTERN") {
		t.Fatalf("value outside mask: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "assumptions")
	changed, _ = json.Marshal(fields)
	if _, err := NewAuthority(changed, validation.Digest(changed)); err == nil {
		t.Fatal("accepted missing assumptions")
	}
	if _, err := NewAuthority(raw, strings.Repeat("0", 64)); !isRejection(err, "AUTHORITY_HASH_MISMATCH") {
		t.Fatal(err)
	}
	extra := bytes.Replace(raw, []byte(`{"schema_version"`), []byte(`{"extra":1,"schema_version"`), 1)
	if _, err := NewAuthority(extra, validation.Digest(extra)); err == nil {
		t.Fatal("accepted unknown field")
	}
}

func TestReplayRejectsBadTrajectory(t *testing.T) {
	h := lga.HPP{Width: 2, Height: 2}
	start := make(automaton.State, h.Cells()*h.Bits())
	start[lga.East] = true
	_, a := prepare(t, testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start))
	states, err := automaton.Simulate(a.System(), start, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Replay(automaton.Trajectory{States: states, Choices: make([][]bool, 2)}); err != nil {
		t.Fatal(err)
	}
	wrong := append([]automaton.State{}, states...)
	wrong[1] = append(automaton.State{}, states[1]...)
	wrong[1][5] = !wrong[1][5]
	if err := a.Replay(automaton.Trajectory{States: wrong, Choices: make([][]bool, 2)}); !isRejection(err, "TRAJECTORY_STEP") {
		t.Fatal(err)
	}
	for name, trajectory := range map[string]automaton.Trajectory{
		"empty":        {},
		"choice count": {States: states, Choices: make([][]bool, 1)},
		"choice bits":  {States: states, Choices: [][]bool{{true}, {}}},
		"state size":   {States: []automaton.State{start[:3]}, Choices: [][]bool{}},
	} {
		if err := a.Replay(trajectory); !isRejection(err, "TRAJECTORY_SHAPE") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestBitStrings(t *testing.T) {
	bits := []bool{true, false, true, false, false, false, false, false, true, true}
	text := encodeBits(bits)
	if text != "0503" {
		t.Fatal(text)
	}
	if got, ok := decodeBits(text, 10); !ok || !equal(got, bits) {
		t.Fatal(got, ok)
	}
	for _, bad := range []string{"05", "050300", "05ff", "0G03", "0A03"} {
		if _, ok := decodeBits(bad, 10); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
}
