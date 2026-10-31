package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func simpleInput(t *testing.T, reversible bool, returnLimit int) Inputs {
	t.Helper()
	s0, s1 := validation.State{"x": "0"}, validation.State{"x": "1"}
	actions := []validation.Action{{ID: "go", Guard: s0, Effect: s1, Frame: []string{}}}
	if reversible {
		actions = append(actions, validation.Action{ID: "back", Guard: s1, Effect: s0, Frame: []string{}})
	}
	c := validation.Contract{SchemaVersion: validation.Version, RequestID: "q", SourceBinding: map[string]string{"source:1": validation.Digest([]byte("rule"))}, Assumptions: map[string]string{}, Model: validation.Model{Fields: map[string][]string{"x": {"0", "1"}}, Forbidden: []validation.State{}, Actions: actions}, Start: s0, Goal: s1, Baseline: s0, ForwardLimit: 1, ReturnLimit: returnLimit}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	a, e := validation.NewAuthority(raw, validation.Digest(raw))
	if e != nil {
		t.Fatal(e)
	}
	return Inputs{Authority: a, AuthorityRaw: raw, Content: map[string][]byte{"source:1": []byte("rule")}, Sources: SourceManifest{SchemaVersion: "pouch-sources/v1", AuthoritySHA256: validation.Digest(raw), RequestID: "q", Sources: []Source{{ID: "source:1", File: "source.txt", SHA256: c.SourceBinding["source:1"]}}}}
}
func noReturnResult() planning.Result {
	return planning.Result{ForwardComplete: true, Complete: true, Forward: []planning.Path{{ID: "p000000", Trace: validation.Trace{States: []validation.State{{"x": "0"}, {"x": "1"}}, Actions: []string{"go"}, ClaimShortest: true}, ReturnIndex: 0}}, Returns: []planning.Return{{Endpoint: validation.State{"x": "1"}, Status: "NO_RETURN_WITHIN_BOUND", CompleteWithinBound: true}}}
}
func TestNoReturnNeedsFullOriginalClosure(t *testing.T) {
	in := simpleInput(t, true, 0)
	b, e := Certify(t.Context(), in, noReturnResult(), t.TempDir())
	if e != nil || b.Complete || b.Paths[0].Receipt != nil || b.Paths[0].RecoveryStatus != "UNKNOWN" || b.Paths[0].Reason != "RETURN_EXISTS_BEYOND_SEARCH_BOUND" {
		t.Fatalf("%+v %v", b, e)
	}
	in = simpleInput(t, false, 0)
	b, e = Certify(t.Context(), in, noReturnResult(), t.TempDir())
	if e != nil || !b.Complete || b.Paths[0].Receipt == nil || b.Paths[0].RecoveryStatus != "NO_RETURN_IN_MODEL" {
		t.Fatalf("%+v %v", b, e)
	}
}
func TestMissingPathIsRejectedDespiteExhaustion(t *testing.T) {
	in := simpleInput(t, false, 0)
	r := noReturnResult()
	r.Forward = nil
	b, e := Certify(t.Context(), in, r, t.TempDir())
	if e == nil || b.Status != "REJECTED" || b.SetCheck.Missing != 1 {
		t.Fatalf("%+v %v", b, e)
	}
}
func TestPartialSearchPreservesVerifiedForward(t *testing.T) {
	in := simpleInput(t, true, 1)
	r := noReturnResult()
	r.Complete = false
	r.ForwardComplete = false
	r.Returns = nil
	r.Forward[0].ReturnIndex = -1
	b, e := Certify(t.Context(), in, r, t.TempDir())
	if e != nil || b.Complete || !b.Paths[0].ForwardVerified || b.Paths[0].Receipt != nil {
		t.Fatalf("%+v %v", b, e)
	}
}

// chainInput declares one field per size, plus one single-valued padding field
// when padding is nonempty. Each action advances one field by one value, so
// every combination of values is reachable and the start is never revisited.
func chainInput(t *testing.T, padding string, forwardLimit int, sizes ...int) Inputs {
	t.Helper()
	fields, start := map[string][]string{}, validation.State{}
	names := []string{}
	for i, size := range sizes {
		field := fmt.Sprintf("f%d", i)
		names = append(names, field)
		start[field] = "0"
		for v := range size {
			fields[field] = append(fields[field], fmt.Sprint(v))
		}
	}
	if padding != "" {
		for i := range 14 {
			field := fmt.Sprintf("pad%02d", i)
			names = append(names, field)
			fields[field], start[field] = []string{padding}, padding
		}
	}
	actions := []validation.Action{}
	for i := range sizes {
		field := fmt.Sprintf("f%d", i)
		frame := []string{}
		for _, name := range names {
			if name != field {
				frame = append(frame, name)
			}
		}
		for v := 0; v+1 < sizes[i]; v++ {
			actions = append(actions, validation.Action{ID: fmt.Sprintf("%s-%d", field, v), Guard: validation.State{field: fmt.Sprint(v)}, Effect: validation.State{field: fmt.Sprint(v + 1)}, Frame: frame})
		}
	}
	c := validation.Contract{SchemaVersion: validation.Version, RequestID: "q", SourceBinding: map[string]string{"source:1": validation.Digest([]byte("rule"))}, Assumptions: map[string]string{}, Model: validation.Model{Fields: fields, Forbidden: []validation.State{}, Actions: actions}, Start: start, Goal: validation.State{"f0": "1"}, Baseline: start, ForwardLimit: forwardLimit, ReturnLimit: 1}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	a, e := validation.NewAuthority(raw, validation.Digest(raw))
	if e != nil {
		t.Fatal(e)
	}
	return Inputs{Authority: a, AuthorityRaw: raw}
}

func firstChainStep(in Inputs) planning.Result {
	c := in.Authority.Contract()
	end := validation.State{}
	for field, value := range c.Start {
		end[field] = value
	}
	end["f0"] = "1"
	return planning.Result{ForwardComplete: true, Complete: true, Forward: []planning.Path{{ID: "p000000", Trace: validation.Trace{States: []validation.State{c.Start, end}, Actions: []string{"f0-0"}, ClaimShortest: true}, ReturnIndex: 0}}, Returns: []planning.Return{{Endpoint: end, Status: "NO_RETURN_WITHIN_BOUND", CompleteWithinBound: true}}}
}

func TestStateBudgetLeavesPathsIncomplete(t *testing.T) {
	in := chainInput(t, "", 1, 256, 256, 2)
	b, e := Certify(t.Context(), in, firstChainStep(in), t.TempDir())
	if e != nil || b.Status != "FOUND" || b.Complete || len(b.Paths) != 1 {
		t.Fatalf("%+v %v", b, e)
	}
	path := b.Paths[0]
	if !path.ForwardVerified || path.RecoveryStatus != "UNKNOWN" || path.Reason != "STATE_BUDGET_EXHAUSTED" || path.Receipt != nil || path.PackageFile != "" || path.ClosureFile != "" {
		t.Fatalf("%+v", path)
	}
	if !slices.Contains(b.Diagnostics, "REFERENCE_REACHABILITY_INCOMPLETE") {
		t.Fatal(b.Diagnostics)
	}
}

func TestStateBudgetIsNotAbsence(t *testing.T) {
	in := chainInput(t, "", 0, 256, 256, 2)
	dir := t.TempDir()
	b, e := Certify(t.Context(), in, planning.Result{ForwardComplete: true, Complete: true}, dir)
	if e != nil || b.Status != "INCONCLUSIVE" || b.Complete || !b.SetCheck.Complete {
		t.Fatalf("%+v %v", b, e)
	}
	if _, e = os.Stat(filepath.Join(dir, "forward-closure.json")); !os.IsNotExist(e) {
		t.Fatal("a stopped closure was saved as complete", e)
	}
}

func TestOversizedPackageIsNotARejection(t *testing.T) {
	in := chainInput(t, strings.Repeat("p", 200), 1, 256, 8)
	b, e := Certify(t.Context(), in, firstChainStep(in), t.TempDir())
	if e != nil || b.Status != "FOUND" || b.Complete {
		t.Fatalf("%v %v %v", b.Status, b.Complete, e)
	}
	path := b.Paths[0]
	if path.RecoveryStatus != "UNKNOWN" || path.Reason != "PACKAGE_SIZE_LIMIT" || path.Return != nil || path.Receipt != nil || path.PackageFile != "" {
		t.Fatalf("%+v", path)
	}
}

// expiringContext reports cancellation after a fixed number of checks.
type expiringContext struct {
	context.Context
	checks int
}

func (c *expiringContext) Err() error {
	if c.checks == 0 {
		return context.Canceled
	}
	c.checks--
	return nil
}

func TestInterruptionIsNotARejection(t *testing.T) {
	in := simpleInput(t, false, 0)
	for checks := 0; ; checks++ {
		b, e := Certify(&expiringContext{Context: t.Context(), checks: checks}, in, noReturnResult(), t.TempDir())
		if e == nil {
			if checks == 0 || !b.Complete {
				t.Fatalf("checks %d: %+v", checks, b)
			}
			return
		}
		if b.Status == "REJECTED" {
			t.Fatalf("checks %d: %v was reported as rejected", checks, e)
		}
	}
}

func TestFixtureSourcesAndTamperRefusals(t *testing.T) {
	for _, name := range []string{"synthetic-key-rotation", "django-13344", "django-13344-repair"} {
		dir := filepath.Join("..", "..", "testdata", name)
		raw, e := os.ReadFile(filepath.Join(dir, "authority.json"))
		if e != nil {
			t.Fatal(e)
		}
		var c validation.Contract
		if e = json.Unmarshal(raw, &c); e != nil {
			t.Fatal(e)
		}
		in, e := ReadInputs(filepath.Join(dir, "authority.json"), validation.Digest(raw), c.RequestID, filepath.Join(dir, "sources.json"))
		if e != nil {
			t.Fatal(e)
		}
		if len(in.Content) != len(c.SourceBinding) {
			t.Fatal("source loss")
		}
		out := t.TempDir()
		if _, e = SaveInputs(in, out); e != nil {
			t.Fatal(e)
		}
		if _, e = ReadInputs(filepath.Join(out, "authority.json"), validation.Digest(raw), c.RequestID, filepath.Join(out, "sources.json")); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(out, "sources", "s000.txt"), []byte("changed"), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = ReadInputs(filepath.Join(out, "authority.json"), validation.Digest(raw), c.RequestID, filepath.Join(out, "sources.json")); e == nil {
			t.Fatal("changed source accepted")
		}
	}
}
func TestManifestStrictness(t *testing.T) {
	var m SourceManifest
	for _, raw := range []string{`{"schema_version":"pouch-sources/v1","schema_version":"pouch-sources/v1"}`, `{"Schema_Version":"pouch-sources/v1","authority_sha256":"x","request_id":"q","sources":[]}`, `{"schema_version":"pouch-sources/v1","authority_sha256":"x","request_id":"q"}`} {
		if e := decodeExact([]byte(raw), &m); e == nil {
			t.Fatal("accepted malformed manifest")
		}
	}
}

func TestExpandedSourceManifest(t *testing.T) {
	in := simpleInput(t, true, 1)
	c := in.Authority.Contract()
	c.SourceBinding = map[string]string{}
	in.Content = map[string][]byte{}
	in.Sources.Sources = []Source{}
	for i := range 32 {
		id := fmt.Sprintf("source:%d", i)
		content := []byte(fmt.Sprintf("rule %d\n", i))
		digest := validation.Digest(content)
		c.SourceBinding[id] = digest
		in.Content[id] = content
		in.Sources.Sources = append(in.Sources.Sources, Source{ID: id, SHA256: digest})
	}
	var err error
	in.AuthorityRaw, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	in.Sources.AuthoritySHA256 = validation.Digest(in.AuthorityRaw)
	in.Authority, err = validation.NewAuthority(in.AuthorityRaw, in.Sources.AuthoritySHA256)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in, err = SaveInputs(in, dir)
	if err != nil {
		t.Fatal(err)
	}
	authorityFile, manifestFile := filepath.Join(dir, "authority.json"), filepath.Join(dir, "sources.json")
	loaded, err := ReadInputs(authorityFile, in.Sources.AuthoritySHA256, c.RequestID, manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Content) != 32 || !bytes.Equal(loaded.AuthorityRaw, in.AuthorityRaw) {
		t.Fatal("expanded source inputs lost")
	}
	for id, content := range in.Content {
		if !bytes.Equal(loaded.Content[id], content) {
			t.Fatalf("source bytes changed: %s", id)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SourceManifest)
	}{
		{"missing", func(m *SourceManifest) { m.Sources = m.Sources[:31] }},
		{"extra", func(m *SourceManifest) { m.Sources = append(m.Sources, m.Sources[0]) }},
		{"duplicate", func(m *SourceManifest) { m.Sources[31] = m.Sources[0] }},
		{"unknown", func(m *SourceManifest) { m.Sources[31].ID = "source:unknown" }},
		{"digest", func(m *SourceManifest) { m.Sources[31].SHA256 = validation.Digest([]byte("changed")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := in.Sources
			m.Sources = append([]Source(nil), in.Sources.Sources...)
			tc.mutate(&m)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, tc.name+".json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadInputs(authorityFile, in.Sources.AuthoritySHA256, c.RequestID, path); err == nil {
				t.Fatal("altered source manifest accepted")
			}
		})
	}
	if err := os.WriteFile(filepath.Join(dir, in.Sources.Sources[31].File), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadInputs(authorityFile, in.Sources.AuthoritySHA256, c.RequestID, manifestFile); err == nil {
		t.Fatal("changed source bytes accepted")
	}
}
