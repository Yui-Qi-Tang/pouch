package projection

import (
	"reflect"
	"testing"

	"github.com/Yui-Qi-Tang/calx/incidence"
)

// TestProjectionMatchesCalxIncidence keeps this package equal to Calx incidence,
// which it could replace. Calx does not test against Pouch, so the check lives here.
func TestProjectionMatchesCalxIncidence(t *testing.T) {
	snapshot := snapshot(t, []byte(graphFixture))
	graph := snapshot.Graph()
	var input incidence.Graph
	for _, node := range graph.Nodes {
		input.NodeIDs = append(input.NodeIDs, node.ID)
	}
	for _, edge := range graph.Edges {
		input.Edges = append(input.Edges, incidence.Edge{ID: edge.ID, From: edge.From, To: edge.To, Relation: edge.Relation})
	}
	got, err := Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want, err := incidence.Project(input, incidence.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.NodeIDs, want.NodeIDs) || !reflect.DeepEqual(got.EdgeIDs, want.EdgeIDs) || !reflect.DeepEqual(got.Source, want.Source) || !reflect.DeepEqual(got.Target, want.Target) || !reflect.DeepEqual(got.RelationMasks, want.RelationMasks) {
		t.Fatal("topology differs from Calx incidence")
	}
	got.Source, got.Target, got.RelationMasks = want.Source, want.Target, want.RelationMasks
	if _, err := Reconstruct(got); err != nil {
		t.Fatal(err)
	}
}
