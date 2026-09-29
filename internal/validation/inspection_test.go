package validation

import (
	"context"
	"errors"
	"testing"
)

func TestInspectionIsIndependentAndImmutable(t *testing.T) {
	c, p := fixture(t)
	a := bind(t, c, &p)
	copy := a.Contract()
	copy.Start["x"] = "2"
	copy.Model.Actions[0].Effect["x"] = "0"
	copy.Model.Fields["x"][0] = "changed"
	if _, err := a.Validate(t.Context(), "q", bytesOf(t, p)); err != nil {
		t.Fatal(err)
	}
	r, err := a.Reachable(t.Context(), c.Start, c.Goal)
	if err != nil || r.Shortest != 1 || len(r.States) != 3 || r.Witness.Actions[0] != "direct" {
		t.Fatalf("%+v %v", r, err)
	}
	traces := []Trace{p.Forward, {States: []State{{"x": "0"}, {"x": "1"}, {"x": "2"}}, Actions: []string{"first", "second"}}}
	check, err := a.CheckSet(t.Context(), traces, 10, 100)
	if err != nil || !check.Complete || !check.Matches || check.ReferencePaths != 2 {
		t.Fatalf("%+v %v", check, err)
	}
	check, err = a.CheckSet(t.Context(), traces[:1], 10, 100)
	if err != nil || !check.Complete || check.Matches || check.Missing != 1 {
		t.Fatalf("%+v %v", check, err)
	}
	check, err = a.CheckSet(t.Context(), append(traces, traces[0]), 10, 100)
	if err != nil || check.Matches || check.Duplicates != 1 {
		t.Fatalf("%+v %v", check, err)
	}
	check, err = a.CheckSet(t.Context(), traces, 1, 100)
	if err == nil || check.Complete || check.Matches {
		t.Fatalf("budget became completion: %+v %v", check, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = a.Reachable(ctx, c.Start, c.Goal); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
