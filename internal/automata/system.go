package automata

import (
	"context"
	"slices"

	"github.com/Yui-Qi-Tang/calx/automaton"
)

type table struct {
	inputs, choices int
	output          []bool
}

type wiredCell struct {
	rule   *table
	inputs []automaton.Bit
}

// system implements automaton.System and automaton.Chooser from rule tables.
type system struct {
	bits         int
	cells        []wiredCell
	totalChoices int
}

func newSystem(m Model) (*system, error) {
	if m.Bits < 1 || m.Bits > maxBits || len(m.Rules) == 0 || len(m.Rules) > maxRules || len(m.Cells) == 0 || len(m.Cells) > maxCells {
		return nil, reject("MODEL_BOUND")
	}
	tables := make([]table, len(m.Rules))
	for i, r := range m.Rules {
		if r.Inputs < 0 || r.Choices < 0 || r.Inputs+r.Choices > maxFanIn {
			return nil, reject("RULE_FAN_IN")
		}
		output, ok := decodeBits(r.Table, m.Bits<<(r.Inputs+r.Choices))
		if !ok {
			return nil, reject("RULE_TABLE")
		}
		tables[i] = table{inputs: r.Inputs, choices: r.Choices, output: output}
	}
	s := &system{bits: m.Bits, cells: make([]wiredCell, len(m.Cells))}
	stateBits := len(m.Cells) * m.Bits
	for i, c := range m.Cells {
		if c.Rule < 0 || c.Rule >= len(tables) || len(c.Inputs) != tables[c.Rule].inputs {
			return nil, reject("CELL_RULE")
		}
		inputs := make([]automaton.Bit, len(c.Inputs))
		for j, address := range c.Inputs {
			if address < 0 || address >= stateBits || slices.Contains(c.Inputs[:j], address) {
				return nil, reject("CELL_INPUT")
			}
			inputs[j] = automaton.Bit{Cell: address / m.Bits, Index: address % m.Bits}
		}
		s.cells[i] = wiredCell{rule: &tables[c.Rule], inputs: inputs}
		s.totalChoices += tables[c.Rule].choices
	}
	return s, nil
}

func (s *system) Cells() int                      { return len(s.cells) }
func (s *system) Bits() int                       { return s.bits }
func (s *system) Inputs(cell int) []automaton.Bit { return s.cells[cell].inputs }
func (s *system) Choices(cell int) int            { return s.cells[cell].rule.choices }

// Next implements automaton.System. in holds the inputs and then the choices.
func (s *system) Next(cell int, in, out []bool) {
	row := 0
	for i, set := range in {
		if set {
			row |= 1 << i
		}
	}
	copy(out, s.cells[cell].rule.output[row*s.bits:])
}

// Replay checks every step of t against the rule tables. It reads the tables
// directly, so it shares no code with the Calx encoder or its Replay. It says
// nothing about whether the rule models anything real.
func (a *Authority) Replay(t automaton.Trajectory) error {
	if len(t.States) == 0 || len(t.Choices) != len(t.States)-1 {
		return reject("TRAJECTORY_SHAPE")
	}
	size := len(a.system.cells) * a.system.bits
	for _, state := range t.States {
		if len(state) != size {
			return reject("TRAJECTORY_SHAPE")
		}
	}
	for i, choices := range t.Choices {
		if len(choices) != a.system.totalChoices {
			return reject("TRAJECTORY_SHAPE")
		}
		if !slices.Equal(a.system.step(t.States[i], choices), t.States[i+1]) {
			return reject("TRAJECTORY_STEP")
		}
	}
	return nil
}

// step advances one synchronous step. choices holds every cell's choice bits in
// cell order; the caller has checked both lengths.
func (s *system) step(state automaton.State, choices []bool) automaton.State {
	next := make(automaton.State, len(state))
	for c, cell := range s.cells {
		row := 0
		for i, b := range cell.inputs {
			if state[b.Cell*s.bits+b.Index] {
				row |= 1 << i
			}
		}
		for i, set := range choices[:cell.rule.choices] {
			if set {
				row |= 1 << (cell.rule.inputs + i)
			}
		}
		choices = choices[cell.rule.choices:]
		copy(next[c*s.bits:(c+1)*s.bits], cell.rule.output[row*s.bits:])
	}
	return next
}

// walk follows the only run of a choice-free system. It returns the first step
// at which reached holds, or -1. closed reports that the run has repeated, so no
// later step can differ. Cycle detection keeps one saved state (Brent), because
// a run can be far longer than memory allows.
func (s *system) walk(ctx context.Context, state automaton.State, reached func(automaton.State) bool, limit int) (hit int, closed bool, err error) {
	saved, power, length := state, 1, 0
	for step := 0; ; step++ {
		if reached(state) {
			return step, false, nil
		}
		if step == limit {
			return -1, false, nil
		}
		if err := ctx.Err(); err != nil {
			return -1, false, err
		}
		state = s.step(state, nil)
		length++
		if slices.Equal(state, saved) {
			return -1, true, nil
		}
		if length == power {
			saved, power, length = state, power*2, 0
		}
	}
}
