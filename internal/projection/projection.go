// Package projection preserves an evidence graph as incidence matrices and raw
// JSON sidecars. It does not interpret evidence relations as execution rules.
package projection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	// SchemaVersion is the supported input graph shape.
	SchemaVersion = "canonical-evidence-graph/v1"
	// MaxBytes bounds a source snapshot, including its opaque records.
	MaxBytes = 4 << 20
	// MaxNodes bounds the number of graph nodes.
	MaxNodes = 4096
	// MaxEdges bounds the number of graph edges.
	MaxEdges = 8192
	// MaxMatrixCells bounds all incidence and relation cells before allocation.
	MaxMatrixCells = 1 << 20
)

// Record preserves provider data without interpreting its semantics.
type Record map[string]json.RawMessage

// Node contains a topology ID and every remaining original node field.
type Node struct {
	ID         string
	Attributes Record
}

// Edge contains typed topology and every remaining original edge field.
type Edge struct {
	ID, From, To, Relation string
	Attributes             Record
}

// Graph is the supported provider-neutral evidence graph. Metadata retains all
// non-topology fields, including scope, truncation, payloads and AND manifests.
type Graph struct {
	SchemaVersion string
	SnapshotID    string
	Nodes         []Node
	Edges         []Edge
	Metadata      Record
}

// Snapshot binds an immutable graph copy to the exact supplied bytes.
type Snapshot struct {
	raw    []byte
	digest string
	graph  Graph
}

// Decode verifies an independently supplied digest and accepts only the explicit
// canonical graph shape. Unknown relation labels and metadata are preserved.
func Decode(raw []byte, expectedSHA256 string) (*Snapshot, error) {
	if len(raw) > MaxBytes {
		return nil, errors.New("snapshot exceeds byte limit")
	}
	digest := sha256.Sum256(raw)
	actual := hex.EncodeToString(digest[:])
	if expectedSHA256 != actual {
		return nil, errors.New("snapshot SHA-256 mismatch")
	}
	g, err := decodeGraph(raw)
	if err != nil {
		return nil, err
	}
	return &Snapshot{raw: slices.Clone(raw), digest: actual, graph: g}, nil
}

// SHA256 returns the digest of original bytes, not a reserialized graph.
func (s *Snapshot) SHA256() string { return s.digest }

// Raw returns a copy of the original bytes.
func (s *Snapshot) Raw() []byte { return slices.Clone(s.raw) }

// Graph returns a copy that cannot change the bound snapshot.
func (s *Snapshot) Graph() Graph { return cloneGraph(s.graph) }

// EdgeRecord retains only edge identity and non-topology data.
type EdgeRecord struct {
	ID         string `json:"id"`
	Attributes Record `json:"attributes"`
}

// Matrix uses node rows and edge columns. Each edge has one source, one target,
// and one relation. Separate incidence matrices preserve self-loops and parallel
// edges; isolated nodes remain in NodeIDs and NodeRecords.
type Matrix struct {
	SchemaVersion string           `json:"schema_version"`
	SnapshotID    string           `json:"snapshot_id"`
	SourceSHA256  string           `json:"source_sha256"`
	NodeIDs       []string         `json:"node_ids"`
	EdgeIDs       []string         `json:"edge_ids"`
	Presence      []int            `json:"presence"`
	Source        [][]int          `json:"source"`
	Target        [][]int          `json:"target"`
	RelationMasks map[string][]int `json:"relation_masks"`
	NodeRecords   []Node           `json:"node_records"`
	EdgeRecords   []EdgeRecord     `json:"edge_records"`
	Metadata      Record           `json:"metadata"`
}

// Project produces deterministically ordered matrices from a decoded snapshot.
func Project(s *Snapshot) (*Matrix, error) {
	if s == nil || s.digest == "" {
		return nil, errors.New("snapshot was not decoded")
	}
	g := s.Graph()
	slices.SortFunc(g.Nodes, func(a, b Node) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(g.Edges, func(a, b Edge) int { return strings.Compare(a.ID, b.ID) })
	relations := make(map[string][]int)
	for _, e := range g.Edges {
		relations[e.Relation] = nil
	}
	if err := matrixBound(len(g.Nodes), len(g.Edges), len(relations)); err != nil {
		return nil, err
	}
	m := &Matrix{
		SchemaVersion: g.SchemaVersion, SnapshotID: g.SnapshotID, SourceSHA256: s.digest,
		NodeIDs: make([]string, len(g.Nodes)), EdgeIDs: make([]string, len(g.Edges)),
		Presence: make([]int, len(g.Nodes)), Source: make([][]int, len(g.Nodes)),
		Target: make([][]int, len(g.Nodes)), RelationMasks: relations,
		NodeRecords: g.Nodes, EdgeRecords: make([]EdgeRecord, len(g.Edges)), Metadata: g.Metadata,
	}
	indices := make(map[string]int, len(g.Nodes))
	for i, node := range g.Nodes {
		indices[node.ID] = i
		m.NodeIDs[i], m.Presence[i] = node.ID, 1
		m.Source[i], m.Target[i] = make([]int, len(g.Edges)), make([]int, len(g.Edges))
	}
	for relation := range relations {
		relations[relation] = make([]int, len(g.Edges))
	}
	for j, edge := range g.Edges {
		m.EdgeIDs[j] = edge.ID
		m.Source[indices[edge.From]][j], m.Target[indices[edge.To]][j] = 1, 1
		relations[edge.Relation][j] = 1
		m.EdgeRecords[j] = EdgeRecord{ID: edge.ID, Attributes: edge.Attributes}
	}
	return m, nil
}

// Reconstruct derives endpoints and relation labels from matrix cells. Sidecars
// contain no copied edge endpoints or labels to substitute for them.
// SourceSHA256 is provenance only; it does not authenticate modified matrices.
func Reconstruct(m *Matrix) (Graph, error) {
	if m == nil {
		return Graph{}, errors.New("matrix is nil")
	}
	n, e := len(m.NodeIDs), len(m.EdgeIDs)
	if err := matrixBound(n, e, len(m.RelationMasks)); err != nil {
		return Graph{}, err
	}
	if len(m.Presence) != n || len(m.Source) != n || len(m.Target) != n || len(m.NodeRecords) != n || len(m.EdgeRecords) != e {
		return Graph{}, errors.New("matrix dimensions disagree")
	}
	for i, id := range m.NodeIDs {
		if m.Presence[i] != 1 || m.NodeRecords[i].ID != id || len(m.Source[i]) != e || len(m.Target[i]) != e {
			return Graph{}, errors.New("node record or incidence dimensions disagree")
		}
	}
	for relation, mask := range m.RelationMasks {
		if !validName(relation) || len(mask) != e {
			return Graph{}, errors.New("invalid relation mask")
		}
	}
	g := Graph{SchemaVersion: m.SchemaVersion, SnapshotID: m.SnapshotID, Nodes: make([]Node, n), Edges: make([]Edge, e), Metadata: cloneRecord(m.Metadata)}
	for i, node := range m.NodeRecords {
		g.Nodes[i] = Node{ID: node.ID, Attributes: cloneRecord(node.Attributes)}
	}
	for j, id := range m.EdgeIDs {
		if m.EdgeRecords[j].ID != id {
			return Graph{}, errors.New("edge record ID disagrees with matrix column")
		}
		from, err := endpoint(m.Source, j)
		if err != nil {
			return Graph{}, fmt.Errorf("edge %q source: %w", id, err)
		}
		to, err := endpoint(m.Target, j)
		if err != nil {
			return Graph{}, fmt.Errorf("edge %q target: %w", id, err)
		}
		selected := ""
		for relation, mask := range m.RelationMasks {
			if mask[j] != 0 && mask[j] != 1 {
				return Graph{}, errors.New("relation mask is not binary")
			}
			if mask[j] == 1 {
				if selected != "" {
					return Graph{}, errors.New("relation column is not one-hot")
				}
				selected = relation
			}
		}
		if selected == "" {
			return Graph{}, errors.New("relation column is not one-hot")
		}
		g.Edges[j] = Edge{ID: id, From: m.NodeIDs[from], To: m.NodeIDs[to], Relation: selected, Attributes: cloneRecord(m.EdgeRecords[j].Attributes)}
	}
	if err := validateGraph(g); err != nil {
		return Graph{}, err
	}
	// Check caller-modified opaque data with the same strict JSON boundary.
	raw, err := json.Marshal(g)
	if err != nil {
		return Graph{}, err
	}
	return decodeGraph(raw)
}

func endpoint(rows [][]int, column int) (int, error) {
	selected := -1
	for i, row := range rows {
		if row[column] != 0 && row[column] != 1 {
			return 0, errors.New("incidence value is not binary")
		}
		if row[column] == 1 {
			if selected != -1 {
				return 0, errors.New("incidence column is not one-hot")
			}
			selected = i
		}
	}
	if selected == -1 {
		return 0, errors.New("incidence column is not one-hot")
	}
	return selected, nil
}

func matrixBound(nodes, edges, relations int) error {
	if nodes > MaxNodes || edges > MaxEdges || relations > edges || (2*nodes+relations)*edges > MaxMatrixCells {
		return errors.New("projection exceeds matrix limits")
	}
	return nil
}

func cloneRecord(in Record) Record {
	out := make(Record, len(in))
	for k, v := range in {
		out[k] = slices.Clone(v)
	}
	return out
}

func cloneGraph(g Graph) Graph {
	out := Graph{SchemaVersion: g.SchemaVersion, SnapshotID: g.SnapshotID, Metadata: cloneRecord(g.Metadata), Nodes: make([]Node, len(g.Nodes)), Edges: make([]Edge, len(g.Edges))}
	for i, node := range g.Nodes {
		out.Nodes[i] = Node{ID: node.ID, Attributes: cloneRecord(node.Attributes)}
	}
	for i, edge := range g.Edges {
		out.Edges[i] = Edge{ID: edge.ID, From: edge.From, To: edge.To, Relation: edge.Relation, Attributes: cloneRecord(edge.Attributes)}
	}
	return out
}
