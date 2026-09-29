package planning

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Variable is the semantic identity of a one-based DIMACS variable.
type Variable struct {
	ID int `json:"id"`
	// state, action, or different; see README.md.
	Kind string `json:"kind"`
	// Zero-based state time or action transition start.
	Time int `json:"time"`
	// Later state time for different; otherwise unused.
	OtherTime int    `json:"other_time,omitempty"`
	Field     string `json:"field,omitempty"`
	Value     string `json:"value,omitempty"`
	Action    string `json:"action,omitempty"`
}

// CNF retains both exact clauses and their variable interpretation.
// Clause literals are signed one-based IDs, never zero; an empty clause is false.
// The different helper witnesses inequality only when true; false does not
// assert equality. Its meaning and unused Variable fields are in README.md.
type CNF struct {
	Variables []Variable `json:"variables"`
	Clauses   [][]int    `json:"clauses"`
}

// CheckShape verifies DIMACS bounds before tools consume a formula.
func (c CNF) CheckShape() error {
	for i, variable := range c.Variables {
		if variable.ID != i+1 {
			return fmt.Errorf("variable IDs are not contiguous")
		}
	}
	for i, clause := range c.Clauses {
		for _, lit := range clause {
			if lit == 0 || lit > len(c.Variables) || lit < -len(c.Variables) {
				return fmt.Errorf("invalid literal in clause %d", i)
			}
		}
	}
	return nil
}

// WriteDIMACS writes the exact formula checked by the external tools.
func (c CNF) WriteDIMACS(w io.Writer) error {
	if err := c.CheckShape(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "p cnf %d %d\n", len(c.Variables), len(c.Clauses)); err != nil {
		return err
	}
	for _, clause := range c.Clauses {
		for _, lit := range clause {
			if _, err := fmt.Fprintf(w, "%d ", lit); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, "0"); err != nil {
			return err
		}
	}
	return nil
}

// CheckAssignment requires a total assignment and checks every original clause.
func (c CNF) CheckAssignment(values map[int]bool) error {
	if err := c.CheckShape(); err != nil {
		return err
	}
	if len(values) != len(c.Variables) {
		return fmt.Errorf("assignment is not total")
	}
	for i := 1; i <= len(c.Variables); i++ {
		if _, ok := values[i]; !ok {
			return fmt.Errorf("assignment misses variable %d", i)
		}
	}
	for i, clause := range c.Clauses {
		satisfied := false
		for _, lit := range clause {
			id := lit
			if id < 0 {
				id = -id
			}
			if id < 1 || id > len(c.Variables) {
				return fmt.Errorf("invalid literal in clause %d", i)
			}
			if values[id] == (lit > 0) {
				satisfied = true
				break
			}
		}
		if !satisfied {
			return fmt.Errorf("assignment violates clause %d", i)
		}
	}
	return nil
}

type compiler struct {
	ctx   context.Context
	cnf   CNF
	limit Options
	err   error
}

func (b *compiler) variable(v Variable) int {
	if b.err != nil {
		return 0
	}
	if len(b.cnf.Variables) >= b.limit.MaxVariables {
		b.err = ErrEncodingLimit
		return 0
	}
	v.ID = len(b.cnf.Variables) + 1
	b.cnf.Variables = append(b.cnf.Variables, v)
	return v.ID
}
func (b *compiler) add(lits ...int) {
	if b.err != nil {
		return
	}
	if err := b.ctx.Err(); err != nil {
		b.err = err
		return
	}
	if len(b.cnf.Clauses) >= b.limit.MaxClauses {
		b.err = ErrEncodingLimit
		return
	}
	b.cnf.Clauses = append(b.cnf.Clauses, slices.Clone(lits))
}
func (b *compiler) one(vars []int) {
	b.add(vars...)
	for i, x := range vars {
		for _, y := range vars[i+1:] {
			b.add(-x, -y)
		}
	}
}

type encoding struct {
	CNF
	states  [][]int
	actions [][]int
	matrix  Matrix
}

func encode(ctx context.Context, m Matrix, forbidden []validation.State, start, goal validation.State, horizon int, blocked [][]string, opts Options) (encoding, error) {
	// Bound the quadratic simple-path encoding before allocating time-indexed rows.
	pairs := int64(horizon) * int64(horizon+1) / 2
	variables := int64(horizon+1)*int64(len(m.Coordinates)) + int64(horizon)*int64(len(m.ActionIDs)) + pairs*int64(len(m.Fields))
	domainClauses := int64(len(forbidden))
	for _, field := range m.Fields {
		n := int64(0)
		for _, cell := range m.Coordinates {
			if cell.Field == field {
				n++
			}
		}
		domainClauses += 1 + n*(n-1)/2
	}
	actionCount := int64(len(m.ActionIDs))
	transitionClauses := int64(2) + actionCount*(actionCount-1)/2
	for i := range m.ActionIDs {
		for j := range m.Coordinates {
			transitionClauses += int64(m.Guard[i][j] + m.Effect[i][j])
		}
		for j, field := range m.Fields {
			if m.Frame[i][j] == 1 {
				for _, cell := range m.Coordinates {
					if cell.Field == field {
						transitionClauses += 2
					}
				}
			}
		}
	}
	clauses := int64(horizon+1)*domainClauses + int64(horizon)*transitionClauses + int64(len(start)+len(goal)) + pairs*int64(len(m.Coordinates)+1) + int64(len(blocked))
	if variables > int64(opts.MaxVariables) || clauses > int64(opts.MaxClauses) {
		return encoding{}, ErrEncodingLimit
	}
	b := compiler{ctx: ctx, limit: opts}
	out := encoding{states: make([][]int, horizon+1), actions: make([][]int, horizon), matrix: m}
	fieldCells := map[string][]int{}
	cells := map[Coordinate]int{}
	actionIndex := map[string]int{}
	for i, c := range m.Coordinates {
		fieldCells[c.Field] = append(fieldCells[c.Field], i)
		cells[c] = i
	}
	for i, a := range m.ActionIDs {
		actionIndex[a] = i
	}
	pattern := func(t int, p validation.State) []int {
		var vars []int
		for _, field := range m.Fields {
			if value, ok := p[field]; ok {
				vars = append(vars, out.states[t][cells[Coordinate{Field: field, Value: value}]])
			}
		}
		return vars
	}
	for t := 0; t <= horizon; t++ {
		out.states[t] = make([]int, len(m.Coordinates))
		for i, cell := range m.Coordinates {
			out.states[t][i] = b.variable(Variable{Kind: "state", Time: t, Field: cell.Field, Value: cell.Value})
		}
		for _, field := range m.Fields {
			var vars []int
			for _, i := range fieldCells[field] {
				vars = append(vars, out.states[t][i])
			}
			b.one(vars)
		}
		for _, p := range forbidden {
			vars := pattern(t, p)
			for i := range vars {
				vars[i] = -vars[i]
			}
			b.add(vars...)
		}
	}
	for _, lit := range pattern(0, start) {
		b.add(lit)
	}
	for t := 0; t < horizon; t++ {
		for _, a := range m.ActionIDs {
			out.actions[t] = append(out.actions[t], b.variable(Variable{Kind: "action", Time: t, Action: a}))
		}
		b.one(out.actions[t])
		for i, selected := range out.actions[t] {
			for j := range m.Coordinates {
				if m.Guard[i][j] == 1 {
					b.add(-selected, out.states[t][j])
				}
				if m.Effect[i][j] == 1 {
					b.add(-selected, out.states[t+1][j])
				}
			}
			for j, field := range m.Fields {
				if m.Frame[i][j] == 1 {
					for _, cell := range fieldCells[field] {
						before, after := out.states[t][cell], out.states[t+1][cell]
						b.add(-selected, -before, after)
						b.add(-selected, before, -after)
					}
				}
			}
		}
		vars := pattern(t, goal)
		for i := range vars {
			vars[i] = -vars[i]
		}
		b.add(vars...)
	}
	for _, lit := range pattern(horizon, goal) {
		b.add(lit)
	}
	// A pair must differ in at least one field. Equal one-hot values force that
	// field's difference witness false; no action/auxiliary assignments define identity.
	for left := 0; left <= horizon; left++ {
		for right := left + 1; right <= horizon; right++ {
			var different []int
			for _, field := range m.Fields {
				d := b.variable(Variable{Kind: "different", Time: left, OtherTime: right, Field: field})
				different = append(different, d)
				for _, cell := range fieldCells[field] {
					b.add(-out.states[left][cell], -out.states[right][cell], -d)
				}
			}
			b.add(different...)
		}
	}
	for _, sequence := range blocked {
		if len(sequence) != horizon {
			return encoding{}, fmt.Errorf("blocked path length mismatch")
		}
		clause := make([]int, horizon)
		for t, action := range sequence {
			i, ok := actionIndex[action]
			if !ok {
				return encoding{}, fmt.Errorf("blocked path action unknown")
			}
			clause[t] = -out.actions[t][i]
		}
		b.add(clause...)
	}
	if b.err != nil {
		return encoding{}, b.err
	}
	out.CNF = b.cnf
	return out, nil
}
func (e encoding) trace(values map[int]bool) (validation.Trace, error) {
	if err := e.CheckAssignment(values); err != nil {
		return validation.Trace{}, err
	}
	trace := validation.Trace{States: make([]validation.State, len(e.states)), Actions: make([]string, len(e.actions))}
	for t, vars := range e.states {
		trace.States[t] = validation.State{}
		for i, id := range vars {
			if values[id] {
				cell := e.matrix.Coordinates[i]
				trace.States[t][cell.Field] = cell.Value
			}
		}
	}
	for t, vars := range e.actions {
		for i, id := range vars {
			if values[id] {
				trace.Actions[t] = e.matrix.ActionIDs[i]
			}
		}
	}
	return trace, nil
}
