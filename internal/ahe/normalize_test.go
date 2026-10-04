package ahe

import (
	"bytes"
	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
	"strings"
	"testing"
)

func TestExplicitEmptyGraphProjection(t *testing.T) {
	no := false
	v := View{MaxDepth: 0, MaxEdges: 0, EdgeCount: 0, Truncated: &no, GlobalAbsenceInferenceAllowed: &no}
	raw := []byte(`{"schema_version":"canonical-evidence-graph/v1","snapshot_id":"s","nodes":[{"id":"n"}],"edges":null}`)
	hash := validation.Digest(raw)
	projected, method, err := projectionInput(raw, v)
	if err != nil {
		t.Fatal(err)
	}
	if method != "ahe-depth-zero-null-edges/v1" || hash != validation.Digest(raw) || bytes.Equal(raw, projected) {
		t.Fatal("identity lost")
	}
	snap, err := projection.Decode(projected, validation.Digest(projected))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projection.Project(snap); err != nil {
		t.Fatal(err)
	}
	if _, err = projection.Decode(raw, hash); err == nil {
		t.Fatal("generic decoder silently converted null")
	}
	for _, change := range []func(*View){func(v *View) { v.MaxDepth = 1 }, func(v *View) { v.MaxEdges = 1 }, func(v *View) { v.EdgeCount = 1 }, func(v *View) { yes := true; v.Truncated = &yes }, func(v *View) { v.Truncated = nil }} {
		bad := v
		change(&bad)
		if _, _, err = projectionInput(raw, bad); err == nil {
			t.Fatal("incomplete scope accepted")
		}
	}
	for _, bad := range []string{strings.Replace(string(raw), `,"edges":null`, "", 1), strings.Replace(string(raw), `"nodes":[{"id":"n"}]`, `"nodes":null`, 1), strings.Replace(string(raw), `"edges":null`, `"edges":[],"edges":null`, 1)} {
		if _, _, err = projectionInput([]byte(bad), v); err == nil {
			t.Fatal("bad graph normalized")
		}
	}
}
