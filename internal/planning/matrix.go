// Package planning compiles declared finite models and searches for bounded paths.
// It does not infer action semantics from evidence edges or use receiver answers.
package planning

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Coordinate identifies one categorical value in the state matrix.
type Coordinate struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// Matrix uses stable sorted field, value and action axes. A guard or effect cell
// is one exactly when that coordinate is explicitly required or assigned. Frame
// rows identify unchanged fields. An absent guard field has all-zero cells.
type Matrix struct {
	Fields      []string     `json:"fields"`
	Coordinates []Coordinate `json:"coordinates"`
	ActionIDs   []string     `json:"action_ids"`
	Guard       [][]int      `json:"guard"`
	Effect      [][]int      `json:"effect"`
	Frame       [][]int      `json:"frame"`
}

// Project checks the existing v1 input language and projects its action rules.
// Re-encoding here is only schema validation; it never creates external authority.
func Project(c validation.Contract) (Matrix, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return Matrix{}, fmt.Errorf("encoding contract: %w", err)
	}
	if _, err := validation.NewAuthority(raw, validation.Digest(raw)); err != nil {
		return Matrix{}, fmt.Errorf("checking supported model: %w", err)
	}
	var m Matrix
	for field := range c.Model.Fields {
		m.Fields = append(m.Fields, field)
	}
	slices.Sort(m.Fields)
	for _, field := range m.Fields {
		values := slices.Clone(c.Model.Fields[field])
		slices.Sort(values)
		for _, value := range values {
			m.Coordinates = append(m.Coordinates, Coordinate{Field: field, Value: value})
		}
	}
	actions := slices.Clone(c.Model.Actions)
	slices.SortFunc(actions, func(a, b validation.Action) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	for _, action := range actions {
		m.ActionIDs = append(m.ActionIDs, action.ID)
		guard, effect, frame := make([]int, len(m.Coordinates)), make([]int, len(m.Coordinates)), make([]int, len(m.Fields))
		for i, cell := range m.Coordinates {
			if action.Guard[cell.Field] == cell.Value {
				guard[i] = 1
			}
			if action.Effect[cell.Field] == cell.Value {
				effect[i] = 1
			}
		}
		for i, field := range m.Fields {
			if slices.Contains(action.Frame, field) {
				frame[i] = 1
			}
		}
		m.Guard = append(m.Guard, guard)
		m.Effect = append(m.Effect, effect)
		m.Frame = append(m.Frame, frame)
	}
	return m, nil
}

// Actions reconstructs the declared action rules from a valid projection.
func (m Matrix) Actions() ([]validation.Action, error) {
	if len(m.Guard) != len(m.ActionIDs) || len(m.Effect) != len(m.ActionIDs) || len(m.Frame) != len(m.ActionIDs) {
		return nil, fmt.Errorf("matrix row count mismatch")
	}
	out := make([]validation.Action, 0, len(m.ActionIDs))
	for i, id := range m.ActionIDs {
		if len(m.Guard[i]) != len(m.Coordinates) || len(m.Effect[i]) != len(m.Coordinates) || len(m.Frame[i]) != len(m.Fields) {
			return nil, fmt.Errorf("matrix column count mismatch")
		}
		a := validation.Action{ID: id, Guard: validation.State{}, Effect: validation.State{}, Frame: []string{}}
		for j, cell := range m.Coordinates {
			if m.Guard[i][j] < 0 || m.Guard[i][j] > 1 || m.Effect[i][j] < 0 || m.Effect[i][j] > 1 {
				return nil, fmt.Errorf("nonbinary matrix cell")
			}
			if m.Guard[i][j] == 1 {
				if _, ok := a.Guard[cell.Field]; ok {
					return nil, fmt.Errorf("multiple guard values")
				}
				a.Guard[cell.Field] = cell.Value
			}
			if m.Effect[i][j] == 1 {
				if _, ok := a.Effect[cell.Field]; ok {
					return nil, fmt.Errorf("multiple effect values")
				}
				a.Effect[cell.Field] = cell.Value
			}
		}
		for j, field := range m.Fields {
			if m.Frame[i][j] < 0 || m.Frame[i][j] > 1 {
				return nil, fmt.Errorf("nonbinary frame cell")
			}
			if m.Frame[i][j] == 1 {
				a.Frame = append(a.Frame, field)
			}
		}
		out = append(out, a)
	}
	return out, nil
}
