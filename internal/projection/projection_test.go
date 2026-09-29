package projection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const graphFixture = `{
 "schema_version":"canonical-evidence-graph/v1","snapshot_id":"fixture",
 "nodes":[{"id":"c","kind":"isolated"},{"id":"a","payload":{"text":"A", "number":9007199254740993}},{"id":"b","parents":{"operator":"AND","complete":true,"ids":["a"]}}],
 "edges":[
  {"id":"parallel-2","from":"a","to":"b","relation":"supports_claim","provenance":{"observed":false}},
  {"id":"loop","from":"b","to":"b","relation":"unknown_relation"},
  {"id":"parallel-1","from":"a","to":"b","relation":"supports_claim"}],
 "scope":{"direction":"inbound","truncated":true},"temporal":{"status":"unknown"},
 "parent_manifest":{"b":["a"]},"uninterpreted":null
}`

func digest(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }
func snapshot(t *testing.T, raw []byte) *Snapshot {
	t.Helper()
	s, err := Decode(raw, digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func projectFixture(t *testing.T) *Matrix {
	t.Helper()
	m, err := Project(snapshot(t, []byte(graphFixture)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func jsonValue(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	var out any
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
func sortedGraph(g Graph) Graph {
	slices.SortFunc(g.Nodes, func(a, b Node) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(g.Edges, func(a, b Edge) int { return strings.Compare(a.ID, b.ID) })
	return g
}

func TestRoundTripRecordsAndTopology(t *testing.T) {
	h2, err := os.ReadFile("testdata/h2-artifact.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"H2": h2, "parallel-loop-isolated-unknown": []byte(graphFixture), "empty": []byte(`{"schema_version":"canonical-evidence-graph/v1","snapshot_id":"empty","nodes":[],"edges":[]}`)} {
		t.Run(name, func(t *testing.T) {
			s := snapshot(t, raw)
			m, err := Project(s)
			if err != nil {
				t.Fatal(err)
			}
			g, err := Reconstruct(m)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(jsonValue(t, g), jsonValue(t, sortedGraph(s.Graph()))) {
				t.Fatal("round trip changed graph records")
			}
			if m.SourceSHA256 != digest(raw) {
				t.Fatal("original bytes digest was lost")
			}
			encoded, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Matrix
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			g2, err := Reconstruct(&decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(jsonValue(t, g), jsonValue(t, g2)) {
				t.Fatal("serialized matrix lost sidecar records")
			}
		})
	}
}

func TestMatricesDetermineEndpointsAndRelation(t *testing.T) {
	m := projectFixture(t)
	// loop is edge column 0; nodes are a, b, c. Change the source to the
	// previously isolated c, and select a different existing relation mask.
	m.Source[1][0] = 0
	m.Source[2][0] = 1
	m.RelationMasks["unknown_relation"][0] = 0
	m.RelationMasks["supports_claim"][0] = 1
	g, err := Reconstruct(m)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Edges[0]; got.From != "c" || got.To != "b" || got.Relation != "supports_claim" {
		t.Fatalf("topology did not follow matrix: %+v", got)
	}
	// The digest remains only the original source reference, never a claim
	// that the changed graph is byte-identical to that source.
	if m.SourceSHA256 != digest([]byte(graphFixture)) {
		t.Fatal("unexpected source digest")
	}
}

func TestPermutationPreservesMatrices(t *testing.T) {
	s := snapshot(t, []byte(graphFixture))
	g := s.Graph()
	slices.Reverse(g.Nodes)
	slices.Reverse(g.Edges)
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Project(s)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Project(snapshot(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if a.SourceSHA256 == b.SourceSHA256 {
		t.Fatal("different source bytes should have different digests")
	}
	a.SourceSHA256 = ""
	b.SourceSHA256 = ""
	if !reflect.DeepEqual(jsonValue(t, a), jsonValue(t, b)) {
		t.Fatal("record order changed matrix meaning")
	}
}

func TestSnapshotBindingAndCopies(t *testing.T) {
	raw := []byte(graphFixture)
	if _, err := Decode(append(slices.Clone(raw), ' '), digest(raw)); err == nil {
		t.Fatal("accepted changed bytes under old hash")
	}
	if _, err := Decode(raw, ""); err == nil {
		t.Fatal("accepted missing caller hash")
	}
	s := snapshot(t, raw)
	raw[0] = '!'
	s.Raw()[0] = '!'
	g := s.Graph()
	g.Nodes[0].ID = "changed"
	g.Nodes[1].Attributes["payload"][0] = '!'
	g.Metadata["scope"][0] = '!'
	if string(s.Raw()) != graphFixture || s.Graph().Nodes[0].ID != "c" {
		t.Fatal("mutable access changed bound snapshot")
	}
	if _, err := Project(s); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRejectsMalformedSnapshots(t *testing.T) {
	for name, raw := range map[string]string{
		"root alias":          strings.Replace(graphFixture, "schema_version", "Schema_Version", 1),
		"unsupported version": strings.Replace(graphFixture, "canonical-evidence-graph/v1", "canonical-evidence-graph/v2", 1),
		"duplicate root":      strings.Replace(graphFixture, `"snapshot_id":"fixture"`, `"snapshot_id":"fixture","snapshot_id":"other"`, 1),
		"nested duplicate":    strings.Replace(graphFixture, `"text":"A"`, `"text":"A","text":"B"`, 1),
		"nested alias":        strings.Replace(graphFixture, `"text":"A"`, `"text":"A","Text":"B"`, 1),
		"node ID alias":       strings.Replace(graphFixture, `"id":"c"`, `"ID":"c"`, 1),
		"edge from alias":     strings.Replace(graphFixture, `"from":"a"`, `"From":"a"`, 1),
		"missing endpoint":    strings.Replace(graphFixture, `"from":"a"`, `"from":"missing"`, 1),
		"numeric ID":          strings.Replace(graphFixture, `"id":"c"`, `"id":3`, 1),
		"null ID":             strings.Replace(graphFixture, `"id":"c"`, `"id":null`, 1),
		"duplicate node":      strings.Replace(graphFixture, `"id":"c"`, `"id":"a"`, 1),
		"duplicate edge":      strings.Replace(graphFixture, `"id":"loop"`, `"id":"parallel-1"`, 1),
		"empty relation":      strings.Replace(graphFixture, `"relation":"unknown_relation"`, `"relation":""`, 1),
		"trailing":            graphFixture + "{}",
		"null nodes":          `{"schema_version":"canonical-evidence-graph/v1","snapshot_id":"x","nodes":null,"edges":[]}`,
		"missing edges":       `{"schema_version":"canonical-evidence-graph/v1","snapshot_id":"x","nodes":[]}`,
		"non object":          "[]",
	} {
		t.Run(name, func(t *testing.T) {
			bytes := []byte(raw)
			if _, err := Decode(bytes, digest(bytes)); err == nil {
				t.Fatal("accepted malformed graph")
			}
		})
	}
	invalid := append([]byte(graphFixture), 0xff)
	if _, err := Decode(invalid, digest(invalid)); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func TestReconstructRejectsMalformedMatrices(t *testing.T) {
	mutations := map[string]func(*Matrix){
		"source dimensions":         func(m *Matrix) { m.Source = m.Source[:1] },
		"row dimensions":            func(m *Matrix) { m.Target[0] = nil },
		"source empty":              func(m *Matrix) { m.Source[1][0] = 0 },
		"source multiple":           func(m *Matrix) { m.Source[0][0] = 1 },
		"target nonbinary":          func(m *Matrix) { m.Target[1][0] = 2 },
		"relation empty":            func(m *Matrix) { m.RelationMasks["unknown_relation"][0] = 0 },
		"relation multiple":         func(m *Matrix) { m.RelationMasks["supports_claim"][0] = 1 },
		"relation nonbinary":        func(m *Matrix) { m.RelationMasks["unknown_relation"][0] = -1 },
		"relation dimensions":       func(m *Matrix) { m.RelationMasks["unknown_relation"] = nil },
		"missing presence":          func(m *Matrix) { m.Presence[0] = 0 },
		"node ID mismatch":          func(m *Matrix) { m.NodeIDs[0] = "other" },
		"duplicate node":            func(m *Matrix) { m.NodeIDs[0] = "b"; m.NodeRecords[0].ID = "b" },
		"edge ID mismatch":          func(m *Matrix) { m.EdgeIDs[0] = "other" },
		"hidden endpoint":           func(m *Matrix) { m.EdgeRecords[0].Attributes["from"] = json.RawMessage(`"a"`) },
		"hidden endpoint alias":     func(m *Matrix) { m.EdgeRecords[0].Attributes["From"] = json.RawMessage(`"a"`) },
		"hidden duplicate metadata": func(m *Matrix) { m.Metadata["opaque"] = json.RawMessage(`{"x":1,"x":2}`) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := projectFixture(t)
			mutate(m)
			if _, err := Reconstruct(m); err == nil {
				t.Fatal("accepted malformed matrix")
			}
		})
	}
	if _, err := Reconstruct(nil); err == nil {
		t.Fatal("accepted nil matrix")
	}
	if _, err := Project(nil); err == nil {
		t.Fatal("accepted nil snapshot")
	}
}

func TestProjectionBoundsBeforeMatrixAllocation(t *testing.T) {
	g := Graph{SchemaVersion: SchemaVersion, SnapshotID: "large", Nodes: make([]Node, 513), Edges: make([]Edge, 1024)}
	for i := range g.Nodes {
		g.Nodes[i] = Node{ID: fmt.Sprint(i)}
	}
	for i := range g.Edges {
		g.Edges[i] = Edge{ID: fmt.Sprint(i), From: "0", To: "1", Relation: "r"}
	}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	s := snapshot(t, raw)
	if _, err := Project(s); err == nil {
		t.Fatal("accepted over-budget dense matrices")
	}
	raw = make([]byte, MaxBytes+1)
	if _, err := Decode(raw, digest(raw)); err == nil {
		t.Fatal("accepted over-budget bytes")
	}
}
