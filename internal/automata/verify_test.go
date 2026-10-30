package automata

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/calx/automaton"
	"github.com/Yui-Qi-Tang/calx/lga"
	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// savedBundle runs the whole pipeline and returns the bytes solve-automaton saves.
func savedBundle(t *testing.T, a *Authority, digest string) []byte {
	t.Helper()
	result, err := Search(context.Background(), a, truthSolver{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	sources := run.SourceManifest{SchemaVersion: "pouch-sources/v1", AuthoritySHA256: digest, RequestID: a.RequestID(), Sources: []run.Source{}}
	bundle, err := Certify(context.Background(), a, digest, sources, result)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func deterministicCase(t *testing.T) (*Authority, string) {
	t.Helper()
	h := lga.HPP{Width: 2, Height: 2}
	start := longCycleStart(t, h)
	goal, err := h.Run(start, 2)
	if err != nil {
		t.Fatal(err)
	}
	s := testSpec(Lattice{Rule: "hpp", Width: 2, Height: 2, Walls: []int{}}, start)
	s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(goal), 12, 12
	raw, a := prepare(t, s)
	return a, validation.Digest(raw)
}

func choiceCase(t *testing.T) (*Authority, string, automaton.State) {
	t.Helper()
	f := lga.FHP{Width: 2, Height: 2}
	start := make(automaton.State, 24)
	start[lga.HexE], start[lga.HexW+6] = true, true
	end, err := f.Run(start, 1, func(_, _ int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	s := testSpec(Lattice{Rule: "fhp", Width: 2, Height: 2, Walls: []int{}}, start)
	s.Goal, s.ForwardLimit, s.ReturnLimit = exactGoal(end), 3, 3
	raw, a := prepare(t, s)
	return a, validation.Digest(raw), end
}

func TestVerifyAcceptsSavedBundles(t *testing.T) {
	a, digest := deterministicCase(t)
	raw := savedBundle(t, a, digest)
	receipt, err := Verify(context.Background(), a, digest, a.RequestID(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.BundleSHA256 != validation.Digest(raw) || receipt.ForwardCost == nil || *receipt.ForwardCost != 2 || receipt.ReturnSteps == nil || *receipt.ReturnSteps != 10 || !receipt.WalkChecked || !receipt.Complete || receipt.RecoveryStatus != "RETURN_VERIFIED" {
		t.Fatalf("%+v", receipt)
	}
	a, digest, _ = choiceCase(t)
	receipt, err = Verify(context.Background(), a, digest, a.RequestID(), savedBundle(t, a, digest))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.WalkChecked {
		t.Fatalf("%+v", receipt)
	}
}

func TestVerifySettlesNoReturn(t *testing.T) {
	raw := merge("01", "01", "03", 3, 3)
	digest := validation.Digest(raw)
	a, err := NewAuthority(raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := Verify(context.Background(), a, digest, "merge", savedBundle(t, a, digest))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RecoveryStatus != "NO_RETURN_IN_MODEL" || !receipt.Complete {
		t.Fatalf("%+v", receipt)
	}
}

func tamper(t *testing.T, raw []byte, change func(*Bundle)) []byte {
	t.Helper()
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	change(&b)
	changed, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func TestVerifyRejectsTamperedBundles(t *testing.T) {
	a, digest := deterministicCase(t)
	raw := savedBundle(t, a, digest)
	longer := 3
	cases := map[string]struct {
		change func(*Bundle)
		code   string
	}{
		"status":         {func(b *Bundle) { b.Status = "NO_PATH_IN_MODEL" }, "BUNDLE_CLAIM_MISMATCH"},
		"complete":       {func(b *Bundle) { b.Complete = false }, "BUNDLE_CLAIM_MISMATCH"},
		"recovery":       {func(b *Bundle) { b.RecoveryStatus = "NO_RETURN_IN_MODEL" }, "BUNDLE_CLAIM_MISMATCH"},
		"return steps":   {func(b *Bundle) { b.ReturnSteps = &longer }, "BUNDLE_CLAIM_MISMATCH"},
		"reason":         {func(b *Bundle) { b.Reason = "ORBIT_NOT_CLOSED" }, "BUNDLE_CLAIM_MISMATCH"},
		"observed fact":  {func(b *Bundle) { b.ObservedFact = true }, "BUNDLE_CLAIM_MISMATCH"},
		"bits":           {func(b *Bundle) { b.Bits = 1 }, "BUNDLE_CLAIM_MISMATCH"},
		"display":        {func(b *Bundle) { b.Display.Columns = 4 }, "BUNDLE_CLAIM_MISMATCH"},
		"diagnostics":    {func(b *Bundle) { b.Diagnostics = []string{"NOTE"} }, "BUNDLE_CLAIM_MISMATCH"},
		"forward state":  {func(b *Bundle) { b.Search.Forward.States[1] = strings.Repeat("0", 4) }, "BUNDLE_CLAIM_REJECTED"},
		"return state":   {func(b *Bundle) { b.Search.Return.Trace.States[1] = strings.Repeat("0", 4) }, "BUNDLE_CLAIM_REJECTED"},
		"query answer":   {func(b *Bundle) { b.Search.Queries[0].Status = "SAT" }, "BUNDLE_CLAIM_REJECTED"},
		"query verified": {func(b *Bundle) { b.Search.Queries[0].Verified = false }, "BUNDLE_CLAIM_REJECTED"},
		"query dropped":  {func(b *Bundle) { b.Search.Queries = b.Search.Queries[1:] }, "BUNDLE_CLAIM_REJECTED"},
		"forward limit":  {func(b *Bundle) { b.Search.ForwardLimit = 2 }, "BUNDLE_CLAIM_REJECTED"},
		"return limit":   {func(b *Bundle) { b.Search.ReturnLimit = 2 }, "BUNDLE_CLAIM_REJECTED"},
		"deterministic":  {func(b *Bundle) { b.Search.Deterministic = false }, "BUNDLE_CLAIM_REJECTED"},
		"policy":         {func(b *Bundle) { b.Search.Policy = "other" }, "BUNDLE_SEARCH"},
		"orbit budget":   {func(b *Bundle) { b.Search.Options.MaxOrbitSteps = 0 }, "BUNDLE_SEARCH"},
		"schema":         {func(b *Bundle) { b.SchemaVersion = "pouch-bundle/v1" }, "BUNDLE_SCHEMA"},
		"request":        {func(b *Bundle) { b.RequestID = "other" }, "BUNDLE_BINDING"},
		"digest":         {func(b *Bundle) { b.AuthoritySHA256 = strings.Repeat("0", 64) }, "BUNDLE_BINDING"},
		"source digest":  {func(b *Bundle) { b.Sources.AuthoritySHA256 = strings.Repeat("0", 64) }, "BUNDLE_BINDING"},
		"source request": {func(b *Bundle) { b.Sources.RequestID = "other" }, "BUNDLE_BINDING"},
	}
	for name, c := range cases {
		if _, err := Verify(context.Background(), a, digest, a.RequestID(), tamper(t, raw, c.change)); !isRejection(err, c.code) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := Verify(context.Background(), a, digest, "other", raw); !isRejection(err, "BUNDLE_BINDING") {
		t.Fatal(err)
	}
	if _, err := Verify(context.Background(), a, strings.Repeat("0", 64), a.RequestID(), raw); !isRejection(err, "BUNDLE_BINDING") {
		t.Fatal(err)
	}
	for name, changed := range map[string][]byte{
		"unknown field": bytes.Replace(raw, []byte(`"status"`), []byte(`"extra": 1, "status"`), 1),
		"trailing":      append(bytes.Clone(raw), []byte("{}")...),
		"not json":      []byte("<html>"),
	} {
		if _, err := Verify(context.Background(), a, digest, a.RequestID(), changed); !isRejection(err, "BUNDLE_SCHEMA") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// With choices no run can be walked. This run is genuine, replays and ends at the
// goal, so only the query ledger shows that it is not the shortest.
func TestVerifyRejectsLongerRunWithChoices(t *testing.T) {
	a, digest, _ := choiceCase(t)
	raw := savedBundle(t, a, digest)
	changed := tamper(t, raw, func(b *Bundle) {
		forward, back := b.Search.Forward, b.Search.Return.Trace
		if back == nil {
			t.Fatal("the case needs a return to build a longer run")
		}
		longer := Trace{States: append(append(append([]string{}, forward.States...), back.States[1:]...), forward.States[1:]...), Choices: append(append(append([]string{}, forward.Choices...), back.Choices...), forward.Choices...)}
		run, err := a.decodeTrace(longer)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Replay(run); err != nil {
			t.Fatalf("the longer run must replay: %v", err)
		}
		b.Search.Forward = &longer
	})
	if _, err := Verify(context.Background(), a, digest, a.RequestID(), changed); !isRejection(err, "BUNDLE_CLAIM_REJECTED") {
		t.Fatal(err)
	}
}

// A consistent lie in both the run and the ledger still fails the walk.
func TestWalkRejectsConsistentFalseExhaustion(t *testing.T) {
	a, digest := deterministicCase(t)
	raw := savedBundle(t, a, digest)
	changed := tamper(t, raw, func(b *Bundle) {
		b.Search.Forward, b.Search.Return, b.Status = nil, nil, "NO_PATH_WITHIN_BOUND"
		b.Search.Queries = b.Search.Queries[:0]
		for h := 0; h <= b.Search.ForwardLimit; h++ {
			b.Search.Queries = append(b.Search.Queries, planningQuery("forward", h))
		}
	})
	if _, err := Verify(context.Background(), a, digest, a.RequestID(), changed); !isRejection(err, "BUNDLE_CLAIM_REJECTED") {
		t.Fatal(err)
	}
}

func planningQuery(direction string, horizon int) planning.Query {
	return planning.Query{Direction: direction, Horizon: horizon, Status: "UNSAT", Verified: true}
}
