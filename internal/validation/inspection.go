package validation

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
)

// Contract returns a deep copy; callers cannot replace this authority's inputs.
func (a *Authority) Contract() Contract {
	c := a.contract
	c.SourceBinding = maps.Clone(c.SourceBinding)
	c.Assumptions = maps.Clone(c.Assumptions)
	c.Start, c.Goal, c.Baseline = maps.Clone(c.Start), maps.Clone(c.Goal), maps.Clone(c.Baseline)
	c.Model.Fields = make(map[string][]string, len(a.contract.Model.Fields))
	for f, values := range a.contract.Model.Fields {
		c.Model.Fields[f] = slices.Clone(values)
	}
	c.Model.Forbidden = make([]State, len(a.contract.Model.Forbidden))
	for i, s := range a.contract.Model.Forbidden {
		c.Model.Forbidden[i] = maps.Clone(s)
	}
	c.Model.Actions = make([]Action, len(a.contract.Model.Actions))
	for i, act := range a.contract.Model.Actions {
		act.Guard = maps.Clone(act.Guard)
		act.Effect = maps.Clone(act.Effect)
		act.Frame = slices.Clone(act.Frame)
		c.Model.Actions[i] = act
	}
	return c
}

// ReplayForward checks a single forward trace even when recovery is unresolved.
func (a *Authority) ReplayForward(ctx context.Context, trace Trace) (State, error) {
	if ctx == nil || a == nil {
		return nil, errors.New("authority and context required")
	}
	end, err := a.replay(ctx, trace, a.contract.Start, a.contract.Goal, a.contract.ForwardLimit)
	return maps.Clone(end), err
}

// Reachability is the complete finite closure from one original-rule state.
// Witness is nil exactly when the goal is unreachable after successful analysis.
type Reachability struct {
	States   []State `json:"states"`
	Shortest int     `json:"shortest"`
	Witness  *Trace  `json:"witness"`
}

// Reachable independently evaluates the original rules without a sender graph,
// CNF, or horizon. It does not turn an operational search bound into no-return.
func (a *Authority) Reachable(ctx context.Context, start, goal State) (Reachability, error) {
	r := Reachability{States: []State{}, Shortest: -1}
	if ctx == nil || a == nil {
		return r, errors.New("authority and context required")
	}
	if !a.valid(start) || len(goal) == 0 || !a.partial(goal) {
		return r, reject("QUERY_DOMAIN")
	}
	queue := []State{maps.Clone(start)}
	seen := map[string]int{a.key(start): 0}
	parents, labels := []int{-1}, []string{""}
	hit := -1
	for head := 0; head < len(queue); head++ {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if hit < 0 && matches(queue[head], goal) {
			hit = head
		}
		for _, act := range a.contract.Model.Actions {
			next, ok := a.step(queue[head], act)
			if !ok {
				continue
			}
			key := a.key(next)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = len(queue)
			queue = append(queue, next)
			parents = append(parents, head)
			labels = append(labels, act.ID)
		}
	}
	r.States = queue
	if hit >= 0 {
		t := Trace{States: []State{}, Actions: []string{}, ClaimShortest: true}
		for i := hit; i >= 0; i = parents[i] {
			t.States = append(t.States, maps.Clone(queue[i]))
			if parents[i] >= 0 {
				t.Actions = append(t.Actions, labels[i])
			}
		}
		slices.Reverse(t.States)
		slices.Reverse(t.Actions)
		r.Shortest = len(t.Actions)
		r.Witness = &t
	}
	return r, nil
}

// SetCheck separates reference traversal completion from set equality.
type SetCheck struct {
	Complete       bool   `json:"complete"`
	Matches        bool   `json:"matches"`
	ReferencePaths int    `json:"reference_paths"`
	SubmittedPaths int    `json:"submitted_paths"`
	Missing        int    `json:"missing"`
	Extra          int    `json:"extra"`
	Duplicates     int    `json:"duplicates"`
	Visits         int    `json:"visits"`
	StopReason     string `json:"stop_reason,omitempty"`
}

func sequenceKey(actions []string) string { b, _ := json.Marshal(actions); return string(b) }

// CheckSet compares bounded simple first-hit paths using original-rule DFS.
// Resource limits leave Complete false; partial counts never establish equality.
func (a *Authority) CheckSet(ctx context.Context, paths []Trace, maxPaths, maxVisits int) (SetCheck, error) {
	r := SetCheck{SubmittedPaths: len(paths)}
	if ctx == nil || a == nil || maxPaths <= 0 || maxVisits <= 0 {
		return r, errors.New("authority, context, and positive reference limits required")
	}
	submitted := map[string]bool{}
	for _, t := range paths {
		if _, err := a.ReplayForward(ctx, t); err != nil {
			return r, err
		}
		k := sequenceKey(t.Actions)
		if submitted[k] {
			r.Duplicates++
		}
		submitted[k] = true
	}
	expected := map[string]bool{}
	visited := map[string]bool{a.key(a.contract.Start): true}
	var walk func(State, []string) error
	walk = func(s State, actions []string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.Visits++
		if r.Visits > maxVisits {
			r.StopReason = "REFERENCE_VISIT_LIMIT"
			return errors.New("reference visit limit")
		}
		if matches(s, a.contract.Goal) {
			k := sequenceKey(actions)
			if len(expected) >= maxPaths && !expected[k] {
				r.StopReason = "REFERENCE_PATH_LIMIT"
				return errors.New("reference path limit")
			}
			expected[k] = true
			return nil
		}
		if len(actions) >= a.contract.ForwardLimit {
			return nil
		}
		for _, act := range a.contract.Model.Actions {
			next, ok := a.step(s, act)
			if !ok {
				continue
			}
			key := a.key(next)
			if visited[key] {
				continue
			}
			visited[key] = true
			err := walk(next, append(actions, act.ID))
			delete(visited, key)
			if err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(a.contract.Start, []string{})
	r.ReferencePaths = len(expected)
	if err != nil {
		if r.StopReason == "" {
			r.StopReason = "REFERENCE_INTERRUPTED"
		}
		return r, err
	}
	r.Complete = true
	for key := range expected {
		if !submitted[key] {
			r.Missing++
		}
	}
	for key := range submitted {
		if !expected[key] {
			r.Extra++
		}
	}
	r.Matches = r.Missing == 0 && r.Extra == 0 && r.Duplicates == 0
	return r, nil
}
