package automata

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/calx/lga"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

const exampleDir = "../../testdata/automaton-hpp/"

// The example is what the documentation tells a reader to expect, so it must keep
// preparing, solving and verifying as documented.
func TestDocumentedExample(t *testing.T) {
	spec, err := os.ReadFile(exampleDir + "spec.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Prepare(spec, validation.Digest(spec))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(exampleDir + "authority.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, saved) {
		t.Fatal("authority.json is not what the spec prepares")
	}
	digest := validation.Digest(raw)
	a, err := NewAuthority(raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	result, bundle, err := certified(t, a, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Forward.Choices) != 1 || bundle.ReturnSteps == nil || *bundle.ReturnSteps != 2 || !bundle.Complete || result.Forward.States[1] != "0020100000" {
		t.Fatalf("%+v", bundle)
	}
	if _, err := Verify(context.Background(), a, digest, a.RequestID(), savedBundle(t, a, digest)); err != nil {
		t.Fatal(err)
	}
}

// The workflow travels without this repository, so it repeats the spec.
func TestWorkflowRepeatsTheExampleSpec(t *testing.T) {
	workflow, err := os.ReadFile("../../.claude/skills/pouch/references/workflow.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(workflow), "### Automata")
	if !ok {
		t.Fatal("missing section")
	}
	_, rest, ok := strings.Cut(section, "```json\n")
	if !ok {
		t.Fatal("missing example")
	}
	block, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("unterminated example")
	}
	spec, err := os.ReadFile(exampleDir + "spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var shown, saved any
	if err := json.Unmarshal([]byte(block), &shown); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(spec, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shown, saved) {
		t.Fatal("workflow example differs from testdata")
	}
}

// These pin the layout tables in README.md and the workflow.
func TestDocumentedLayout(t *testing.T) {
	if lga.East != 0 || lga.North != 1 || lga.West != 2 || lga.South != 3 {
		t.Fatal("HPP direction order changed")
	}
	if lga.HexE != 0 || lga.HexNE != 1 || lga.HexNW != 2 || lga.HexW != 3 || lga.HexSW != 4 || lga.HexSE != 5 {
		t.Fatal("FHP direction order changed")
	}
	hpp := lga.HPP{Width: 3, Height: 3}
	for _, c := range []struct{ direction, from, to int }{{lga.East, 4, 5}, {lga.North, 4, 7}, {lga.West, 4, 3}, {lga.South, 4, 1}} {
		start := make(automaton.State, 36)
		start[c.from*4+c.direction] = true
		next, err := hpp.Run(start, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !next[c.to*4+c.direction] {
			t.Fatalf("HPP direction %d does not reach cell %d", c.direction, c.to)
		}
	}
	fhp := lga.FHP{Width: 5, Height: 5}
	center := 2*5 + 2
	for direction, step := range [][2]int{{1, 0}, {0, 1}, {-1, 1}, {-1, 0}, {0, -1}, {1, -1}} {
		start := make(automaton.State, 150)
		start[center*6+direction] = true
		next, err := fhp.Run(start, 1, func(_, _ int) bool { return false })
		if err != nil {
			t.Fatal(err)
		}
		if to := (2+step[1])*5 + 2 + step[0]; !next[to*6+direction] {
			t.Fatalf("FHP direction %d does not reach cell %d", direction, to)
		}
	}
}
