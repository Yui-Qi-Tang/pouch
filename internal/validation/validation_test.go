package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Contract, Package) {
	t.Helper()
	s0, s1, s2 := State{"x": "0"}, State{"x": "1"}, State{"x": "2"}
	c := Contract{SchemaVersion: Version, RequestID: "q", SourceBinding: map[string]string{"canon-node:source": Digest([]byte("source"))}, Assumptions: map[string]string{"finite": "scenario_assumption"}, Model: Model{Fields: map[string][]string{"x": {"0", "1", "2"}}, Forbidden: []State{}, Actions: []Action{
		{ID: "first", Guard: s0, Effect: s1, Frame: []string{}}, {ID: "second", Guard: s1, Effect: s2, Frame: []string{}}, {ID: "direct", Guard: s0, Effect: s2, Frame: []string{}}, {ID: "restore", Guard: s2, Effect: s0, Frame: []string{}}}}, Start: s0, Goal: s2, Baseline: s0, ForwardLimit: 2, ReturnLimit: 2}
	p := Package{SchemaVersion: Version, RequestID: "q", SourceBinding: c.SourceBinding, Assumptions: c.Assumptions, Forward: Trace{States: []State{s0, s2}, Actions: []string{"direct"}, ClaimShortest: true}, ReturnKind: "return", Return: &Trace{States: []State{s2, s0}, Actions: []string{"restore"}, ClaimShortest: true}, ClosedStates: []State{}}
	return c, p
}
func bytesOf(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func bind(t *testing.T, c Contract, p *Package) *Authority {
	t.Helper()
	b := bytesOf(t, c)
	a, e := NewAuthority(b, Digest(b))
	if e != nil {
		t.Fatal(e)
	}
	p.AuthoritySHA256 = Digest(b)
	return a
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var e *Rejection
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
func TestValidationCertificates(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		c, p := fixture(t)
		a := bind(t, c, &p)
		r, e := a.Validate(t.Context(), "q", bytesOf(t, p))
		if e != nil || r.ForwardShortest != 1 || r.ReturnShortest != 1 || r.ObservedFact || r.NativeAHEReceipt {
			t.Fatalf("%+v %v", r, e)
		}
	})
	t.Run("longer legal path is not shortest", func(t *testing.T) {
		c, p := fixture(t)
		a := bind(t, c, &p)
		p.Forward = Trace{States: []State{{"x": "0"}, {"x": "1"}, {"x": "2"}}, Actions: []string{"first", "second"}, ClaimShortest: true}
		_, e := a.Validate(t.Context(), "q", bytesOf(t, p))
		code(t, e, "FORWARD_NOT_SHORTEST")
		p.Forward.ClaimShortest = false
		if _, e = a.Validate(t.Context(), "q", bytesOf(t, p)); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("true no return", func(t *testing.T) {
		c, p := fixture(t)
		c.Model.Actions = c.Model.Actions[:3]
		p.ReturnKind = "no_return"
		p.Return = nil
		p.ClosedStates = []State{{"x": "2"}}
		a := bind(t, c, &p)
		r, e := a.Validate(t.Context(), "q", bytesOf(t, p))
		if e != nil || r.ReturnShortest != -1 {
			t.Fatalf("%+v %v", r, e)
		}
		p.ClosedStates = []State{}
		_, e = a.Validate(t.Context(), "q", bytesOf(t, p))
		code(t, e, "CLOSURE_INCOMPLETE")
	})
	t.Run("short horizon is not no return", func(t *testing.T) {
		c, p := fixture(t)
		c.ReturnLimit = 0
		p.ReturnKind = "no_return"
		p.Return = nil
		p.ClosedStates = []State{{"x": "2"}}
		a := bind(t, c, &p)
		_, e := a.Validate(t.Context(), "q", bytesOf(t, p))
		code(t, e, "FALSE_NO_RETURN")
	})
}
func TestRefusals(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(*Package)
	}{
		{"authority", "PACKAGE_BINDING", func(p *Package) { p.AuthoritySHA256 = strings.Repeat("0", 64) }},
		{"source", "SOURCE_BINDING", func(p *Package) { p.SourceBinding = map[string]string{"canon-node:old": Digest([]byte("source"))} }},
		{"assumption", "ASSUMPTION_BINDING", func(p *Package) { p.Assumptions = map[string]string{"finite": "observed_fact"} }},
		{"request", "PACKAGE_BINDING", func(p *Package) { p.RequestID = "another" }},
		{"effect", "TRACE_EFFECT_OR_FRAME", func(p *Package) { p.Forward.States[1] = State{"x": "1"} }},
		{"extra coordinate", "TRACE_STATE_DOMAIN", func(p *Package) { p.Forward.States[1] = State{"x": "2", "hidden": "0"} }},
		{"missing coordinate", "TRACE_STATE_DOMAIN", func(p *Package) { p.Forward.States[1] = State{} }},
		{"action", "UNKNOWN_ACTION", func(p *Package) { p.Forward.Actions[0] = "invented" }},
		{"return junction", "TRACE_SHAPE_OR_START", func(p *Package) { p.Return.States[0] = State{"x": "1"} }},
		{"return certificate", "RETURN_CERTIFICATE_SHAPE", func(p *Package) { p.Return = nil }},
		{"return kind", "RETURN_KIND", func(p *Package) { p.ReturnKind = "unknown" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, p := fixture(t)
			a := bind(t, c, &p)
			tc.mutate(&p)
			_, e := a.Validate(t.Context(), "q", bytesOf(t, p))
			code(t, e, tc.want)
		})
	}
}
func TestStrictTypesAndCaller(t *testing.T) {
	c, p := fixture(t)
	a := bind(t, c, &p)
	raw := string(bytesOf(t, p))
	for _, bad := range []string{strings.Replace(raw, `"claim_shortest":true`, `"claim_shortest":1`, 1), strings.Replace(raw, `"claim_shortest":true`, `"claim_shortest":null`, 1), strings.Replace(raw, `"request_id":"q"`, `"Request_ID":"q"`, 1), strings.Replace(raw, `"request_id":"q"`, `"request_id":"q","request_id":"q"`, 1), strings.Replace(raw, `"x":"0"`, `"x":0`, 1), strings.Replace(raw, `"request_id":"q",`, "", 1), raw + raw} {
		if _, e := a.Validate(t.Context(), "q", []byte(bad)); e == nil {
			t.Fatal("accepted malformed typed package")
		}
	}
	_, e := a.Validate(t.Context(), "wrong", []byte(raw))
	code(t, e, "CALLER_REQUEST_MISMATCH")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = a.Validate(ctx, "q", []byte(raw))
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("%v", e)
	}
	_, e = NewAuthority(bytesOf(t, c), strings.Repeat("0", 64))
	code(t, e, "AUTHORITY_HASH_MISMATCH")
}
func TestAuthorityModelBounds(t *testing.T) {
	for _, mutate := range []func(*Contract){func(c *Contract) { c.Model.Actions[0].Frame = []string{"x"} }, func(c *Contract) { c.Model.Actions[0].Effect = State{} }, func(c *Contract) { c.Model.Fields["x"] = []string{"0", "0"} }, func(c *Contract) { c.Model.Forbidden = []State{{}} }, func(c *Contract) { c.Start = State{} }, func(c *Contract) { c.Model.Fields["huge"] = make([]string, maxFieldValues+1) }} {
		c, _ := fixture(t)
		mutate(&c)
		raw := bytesOf(t, c)
		if _, e := NewAuthority(raw, Digest(raw)); e == nil {
			t.Fatal("invalid authority accepted")
		}
	}
}

func TestAuthoritySourceBindingBounds(t *testing.T) {
	for _, count := range []int{0, 1, 8, 9, 12, 32, 33} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			c, p := fixture(t)
			c.SourceBinding = map[string]string{}
			for i := range count {
				id := fmt.Sprintf("canon-node:source%d", i)
				c.SourceBinding[id] = Digest([]byte(id))
			}
			raw := bytesOf(t, c)
			a, err := NewAuthority(raw, Digest(raw))
			if count == 0 || count == 33 {
				code(t, err, "AUTHORITY_CONTRACT")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(a.SourceIDs()) != count {
				t.Fatal("source bindings lost")
			}
			p.AuthoritySHA256 = Digest(raw)
			p.SourceBinding = c.SourceBinding
			r, err := a.Validate(t.Context(), c.RequestID, bytesOf(t, p))
			if err != nil || r.ForwardShortest != 1 || r.ReturnShortest != 1 {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}

func TestExpandedSourceBindingRefusals(t *testing.T) {
	c, p := fixture(t)
	c.SourceBinding = map[string]string{}
	for i := range 32 {
		id := fmt.Sprintf("canon-node:source%d", i)
		c.SourceBinding[id] = Digest([]byte(id))
	}
	p.SourceBinding = maps.Clone(c.SourceBinding)
	a := bind(t, c, &p)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing", func(m map[string]string) { delete(m, "canon-node:source31") }},
		{"extra", func(m map[string]string) { m["canon-node:extra"] = Digest([]byte("extra")) }},
		{"digest", func(m map[string]string) { m["canon-node:source31"] = Digest([]byte("changed")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			altered := p
			altered.SourceBinding = maps.Clone(p.SourceBinding)
			tc.mutate(altered.SourceBinding)
			_, err := a.Validate(t.Context(), c.RequestID, bytesOf(t, altered))
			code(t, err, "SOURCE_BINDING")
		})
	}
	c.SourceBinding["canon-node:source31"] = "invalid"
	raw := bytesOf(t, c)
	_, err := NewAuthority(raw, Digest(raw))
	code(t, err, "SOURCE_BINDING")
}

// chainContract declares one field per size. Each action advances one field by
// one value, so every combination of field values is reachable from the start.
func chainContract(t *testing.T, sizes ...int) Contract {
	t.Helper()
	c, _ := fixture(t)
	c.Model = Model{Fields: map[string][]string{}, Forbidden: []State{}}
	c.Start = State{}
	names := []string{}
	for i, size := range sizes {
		field := fmt.Sprintf("f%02d", i)
		names = append(names, field)
		for v := range size {
			c.Model.Fields[field] = append(c.Model.Fields[field], fmt.Sprint(v))
		}
		c.Start[field] = "0"
	}
	for _, field := range names {
		frame := slices.DeleteFunc(slices.Clone(names), func(f string) bool { return f == field })
		for v := 0; v+1 < len(c.Model.Fields[field]); v++ {
			c.Model.Actions = append(c.Model.Actions, Action{ID: fmt.Sprintf("%s-%d", field, v), Guard: State{field: fmt.Sprint(v)}, Effect: State{field: fmt.Sprint(v + 1)}, Frame: frame})
		}
	}
	c.Goal, c.Baseline = State{"f00": "1"}, c.Start
	c.ForwardLimit, c.ReturnLimit = 1, 1
	return c
}

func TestAuthorityLimits(t *testing.T) {
	accept := func(name string, c Contract) {
		t.Run(name, func(t *testing.T) {
			raw := bytesOf(t, c)
			if _, e := NewAuthority(raw, Digest(raw)); e != nil {
				t.Fatal(e)
			}
		})
	}
	refuse := func(name string, c Contract, want string) {
		t.Run(name, func(t *testing.T) {
			raw := bytesOf(t, c)
			_, e := NewAuthority(raw, Digest(raw))
			code(t, e, want)
		})
	}
	accept("domain product above the former limit", chainContract(t, 256, 256, 256))
	accept("largest field", chainContract(t, maxFieldValues))
	refuse("field too large", chainContract(t, maxFieldValues+1), "FIELD_DOMAIN")

	accept("most fields", chainContract(t, slices.Repeat([]int{2}, 128)...))
	refuse("too many fields", chainContract(t, slices.Repeat([]int{2}, 129)...), "MODEL_BOUND")

	coordinates := slices.Repeat([]int{256}, 16)
	most := chainContract(t, coordinates...)
	most.Model.Actions = most.Model.Actions[:1]
	accept("most coordinates", most)
	over := chainContract(t, append(coordinates, 1)...)
	over.Model.Actions = over.Model.Actions[:1]
	refuse("too many coordinates", over, "MODEL_BOUND")

	actions := chainContract(t, 2, 2)
	act := actions.Model.Actions[0]
	actions.Model.Actions = nil
	for i := range maxActions {
		act.ID = fmt.Sprint(i)
		actions.Model.Actions = append(actions.Model.Actions, act)
	}
	accept("most actions", actions)
	act.ID = "extra"
	actions.Model.Actions = append(actions.Model.Actions, act)
	refuse("too many actions", actions, "MODEL_BOUND")

	horizon := chainContract(t, 2)
	horizon.ForwardLimit, horizon.ReturnLimit = maxHorizon, maxHorizon
	accept("longest horizon", horizon)
	horizon.ReturnLimit = maxHorizon + 1
	refuse("horizon too long", horizon, "MODEL_BOUND")
}

func TestReachedStateBudget(t *testing.T) {
	c := chainContract(t, 256, 256)
	raw := bytesOf(t, c)
	a, e := NewAuthority(raw, Digest(raw))
	if e != nil {
		t.Fatal(e)
	}
	r, e := a.Reachable(t.Context(), c.Start, c.Goal)
	if e != nil || len(r.States) != maxReachedStates || r.Witness == nil {
		t.Fatalf("%d states, witness %v, error %v", len(r.States), r.Witness, e)
	}

	c = chainContract(t, 256, 256, 2)
	raw = bytesOf(t, c)
	if a, e = NewAuthority(raw, Digest(raw)); e != nil {
		t.Fatal(e)
	}
	r, e = a.Reachable(t.Context(), c.Start, c.Goal)
	if !errors.Is(e, ErrStateBudget) || r.Witness != nil || len(r.States) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	next := maps.Clone(c.Start)
	next["f00"] = "1"
	p := Package{SchemaVersion: Version, RequestID: c.RequestID, AuthoritySHA256: Digest(raw), SourceBinding: c.SourceBinding, Assumptions: c.Assumptions, Forward: Trace{States: []State{c.Start, next}, Actions: []string{"f00-0"}}, ReturnKind: "no_return", ClosedStates: []State{}}
	_, e = a.Validate(t.Context(), c.RequestID, bytesOf(t, p))
	var rejection *Rejection
	if !errors.Is(e, ErrStateBudget) || errors.As(e, &rejection) {
		t.Fatalf("got %v, want a budget error that is not a rejection", e)
	}
}

func TestRecordedPackagesKeepReceipts(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "recorded-repair", "model")
	raw, e := os.ReadFile(filepath.Join(dir, "authority.json"))
	if e != nil {
		t.Fatal(e)
	}
	a, e := NewAuthority(raw, Digest(raw))
	if e != nil {
		t.Fatal(e)
	}
	bundle, e := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if e != nil {
		t.Fatal(e)
	}
	var saved struct {
		RequestID string `json:"request_id"`
		Paths     []struct {
			PackageFile string  `json:"package_file"`
			Receipt     Receipt `json:"receipt"`
		} `json:"paths"`
	}
	if e = json.Unmarshal(bundle, &saved); e != nil {
		t.Fatal(e)
	}
	if len(saved.Paths) == 0 {
		t.Fatal("no recorded packages")
	}
	for _, path := range saved.Paths {
		pkg, e := os.ReadFile(filepath.Join(dir, path.PackageFile))
		if e != nil {
			t.Fatal(e)
		}
		got, e := a.Validate(t.Context(), saved.RequestID, pkg)
		if e != nil || got != path.Receipt {
			t.Fatalf("%s: got %+v, %v; want %+v", path.PackageFile, got, e, path.Receipt)
		}
	}
}

func TestSessionMatchesValidate(t *testing.T) {
	c, p := fixture(t)
	c.Model.Actions = c.Model.Actions[:3]
	p.ReturnKind, p.Return, p.ClosedStates = "no_return", nil, []State{{"x": "2"}}
	a := bind(t, c, &p)
	s := a.NewSession()
	check := func(p Package) {
		t.Helper()
		raw := bytesOf(t, p)
		want, wantErr := a.Validate(t.Context(), "q", raw)
		got, gotErr := s.Validate(t.Context(), "q", raw)
		if got != want || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
			t.Fatalf("session %+v, %v; validate %+v, %v", got, gotErr, want, wantErr)
		}
	}
	check(p)
	if _, e := s.Validate(t.Context(), "q", bytesOf(t, p)); e != nil {
		t.Fatal(e)
	}
	if len(s.closures) != 2 {
		t.Fatalf("%d closures after repeating one package, want 2", len(s.closures))
	}

	// A cached closure must still refuse every wrong certificate.
	tampered := p
	tampered.ClosedStates = []State{}
	check(tampered)
	tampered.ClosedStates = []State{{"x": "2"}, {"x": "1"}}
	check(tampered)
	tampered.Forward = Trace{States: []State{{"x": "0"}, {"x": "1"}, {"x": "2"}}, Actions: []string{"first", "second"}, ClaimShortest: true}
	check(tampered)
	check(p)
}

func TestSessionSeparatesTargetsFromOneStart(t *testing.T) {
	c, p := fixture(t)
	c.Baseline = State{"x": "1"}
	c.Goal = State{"x": "0"}
	c.Model.Actions = c.Model.Actions[:1]
	p.Forward = Trace{States: []State{{"x": "0"}}, Actions: []string{}, ClaimShortest: true}
	p.Return = &Trace{States: []State{{"x": "0"}, {"x": "1"}}, Actions: []string{"first"}, ClaimShortest: true}
	a := bind(t, c, &p)
	r, e := a.NewSession().Validate(t.Context(), "q", bytesOf(t, p))
	if e != nil || r.ForwardShortest != 0 || r.ReturnShortest != 1 {
		t.Fatalf("%+v %v", r, e)
	}
}
