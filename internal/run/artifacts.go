package run

import (
	"encoding/json"
	"sort"

	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// AttachEvidence projects an explicitly pinned graph or a record-only source view.
// A supplied graph must bind every selected source to its caller-owned digest.
// Native source_claim payloads and explicit inline content digests are supported.
func AttachEvidence(b *Bundle, in Inputs, raw []byte, expected, dir string) error {
	supplied := len(raw) > 0
	if !supplied {
		g := projection.Graph{SchemaVersion: "canonical-evidence-graph/v1", SnapshotID: b.RequestID + "/selected-sources", Nodes: []projection.Node{}, Edges: []projection.Edge{}, Metadata: projection.Record{}}
		g.Metadata["scope"] = json.RawMessage(`{"kind":"selected_source_records","topology":"not_supplied","absence_of_edges":"unknown","provider":"local-file-adapter"}`)
		ids := in.Authority.SourceIDs()
		sort.Strings(ids)
		for _, id := range ids {
			h, _ := in.Authority.SourceDigest(id)
			digest, _ := json.Marshal(h)
			content, _ := json.Marshal(string(in.Content[id]))
			g.Nodes = append(g.Nodes, projection.Node{ID: id, Attributes: projection.Record{"content_sha256": digest, "content": content}})
		}
		var err error
		raw, err = json.MarshalIndent(g, "", "  ")
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		expected = validation.Digest(raw)
	}
	snapshot, err := projection.Decode(raw, expected)
	if err != nil {
		return err
	}
	if supplied {
		if err := checkGraphSources(snapshot.Graph(), in); err != nil {
			return err
		}
		b.EvidenceScope = "caller-pinned graph; selected source bindings checked against authority; payload claims and supplied inline content hashed; metadata defines scope"
	} else {
		b.EvidenceScope = "selected source records; graph topology not supplied"
	}
	matrix, err := projection.Project(snapshot)
	if err != nil {
		return err
	}
	if _, err = projection.Reconstruct(matrix); err != nil {
		return err
	}
	b.EvidenceMatrix = matrix
	if err = writeBytes(dir, "evidence-graph.json", raw); err != nil {
		return err
	}
	return writeJSON(dir, "evidence-matrix.json", matrix)
}

// SaveBundle writes the final shared AI/human data without overwriting a run.
func SaveBundle(dir string, b Bundle) error { return writeJSON(dir, "bundle.json", b) }
