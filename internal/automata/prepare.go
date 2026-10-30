package automata

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/calx/lga"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// SpecVersion identifies the lattice gas specification schema.
const SpecVersion = "pouch-automaton-spec/v1"

// specKey is the assumption that records which spec produced an authority.
const specKey = "pouch-automaton-spec-sha256"

// Lattice names a Calx lattice gas. Walls lists wall cell indices.
type Lattice struct {
	Rule   string `json:"rule"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Walls  []int  `json:"walls"`
}

// Spec asks for an authority with the tables of a lattice gas.
type Spec struct {
	SchemaVersion string            `json:"schema_version"`
	RequestID     string            `json:"request_id"`
	SourceBinding map[string]string `json:"source_binding"`
	Assumptions   map[string]string `json:"assumptions"`
	Lattice       Lattice           `json:"lattice"`
	Start         string            `json:"start"`
	Goal          Pattern           `json:"goal"`
	Baseline      string            `json:"baseline"`
	ForwardLimit  int               `json:"forward_limit"`
	ReturnLimit   int               `json:"return_limit"`
}

// Prepare writes the authority for a caller-pinned spec. The result is checked
// with NewAuthority, and the same spec always produces the same bytes.
func Prepare(raw []byte, expected string) ([]byte, error) {
	if !validDigest(expected) || validation.Digest(raw) != expected {
		return nil, reject("SPEC_HASH_MISMATCH")
	}
	var s Spec
	if err := validation.Decode(raw, &s); err != nil {
		return nil, err
	}
	if _, reserved := s.Assumptions[specKey]; s.SchemaVersion != SpecVersion || reserved {
		return nil, reject("SPEC_CONTRACT")
	}
	sys, err := s.Lattice.system()
	if err != nil {
		return nil, err
	}
	model, err := tabulate(sys)
	if err != nil {
		return nil, err
	}
	assumptions := maps.Clone(s.Assumptions)
	assumptions[specKey] = expected
	authority, err := json.Marshal(Contract{SchemaVersion: Version, RequestID: s.RequestID, SourceBinding: s.SourceBinding, Assumptions: assumptions, Model: model, Start: s.Start, Goal: s.Goal, Baseline: s.Baseline, ForwardLimit: s.ForwardLimit, ReturnLimit: s.ReturnLimit})
	if err != nil {
		return nil, fmt.Errorf("encoding authority: %w", err)
	}
	if _, err := NewAuthority(authority, validation.Digest(authority)); err != nil {
		return nil, err
	}
	return authority, nil
}

func (l Lattice) system() (automaton.System, error) {
	if l.Width < 1 || l.Height < 1 || l.Width > maxCells/l.Height {
		return nil, reject("LATTICE_BOUND")
	}
	var walls []bool
	if len(l.Walls) > 0 {
		walls = make([]bool, l.Width*l.Height)
		for _, cell := range l.Walls {
			if cell < 0 || cell >= len(walls) || walls[cell] {
				return nil, reject("LATTICE_WALL")
			}
			walls[cell] = true
		}
	}
	switch l.Rule {
	case "hpp":
		return lga.HPP{Width: l.Width, Height: l.Height, Walls: walls}, nil
	case "fhp":
		return lga.FHP{Width: l.Width, Height: l.Height, Walls: walls}, nil
	}
	return nil, reject("LATTICE_RULE")
}

// tabulate evaluates every cell's rule on every input row. Cells with equal
// tables share one rule, which keeps a uniform lattice's authority small.
func tabulate(sys automaton.System) (Model, error) {
	chooser, _ := sys.(automaton.Chooser)
	m := Model{Bits: sys.Bits(), Rules: []Rule{}, Cells: make([]Cell, sys.Cells())}
	known := map[Rule]int{}
	for c := range m.Cells {
		inputs := sys.Inputs(c)
		rule := Rule{Inputs: len(inputs)}
		if chooser != nil {
			rule.Choices = chooser.Choices(c)
		}
		if rule.Inputs+rule.Choices > maxFanIn {
			return Model{}, reject("RULE_FAN_IN")
		}
		rule.Table = encodeBits(ruleTable(sys, c, rule.Inputs+rule.Choices))
		index, ok := known[rule]
		if !ok {
			index = len(m.Rules)
			known[rule] = index
			m.Rules = append(m.Rules, rule)
		}
		wired := make([]int, len(inputs))
		for i, b := range inputs {
			wired[i] = b.Cell*m.Bits + b.Index
		}
		m.Cells[c] = Cell{Rule: index, Inputs: wired}
	}
	return m, nil
}

func ruleTable(sys automaton.System, cell, fanIn int) []bool {
	bits := sys.Bits()
	output := make([]bool, bits<<fanIn)
	in := make([]bool, fanIn)
	for row := range 1 << fanIn {
		for i := range in {
			in[i] = row>>i&1 == 1
		}
		sys.Next(cell, in, output[row*bits:(row+1)*bits])
	}
	return output
}
