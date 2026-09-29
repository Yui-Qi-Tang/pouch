package run

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// AttachEvidence projects an explicitly pinned graph or a record-only source view.
// A supplied graph must explicitly bind each selected source node's content digest.
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
		present := map[string]string{}
		for _, node := range snapshot.Graph().Nodes {
			var digest string
			if v, ok := node.Attributes["content_sha256"]; ok {
				if err := json.Unmarshal(v, &digest); err != nil {
					return errors.New("graph source digest type")
				}
				present[node.ID] = digest
			}
		}
		for _, id := range in.Authority.SourceIDs() {
			want, _ := in.Authority.SourceDigest(id)
			if present[id] != want {
				return errors.New("graph selected source binding mismatch")
			}
		}
		b.EvidenceScope = "caller-pinned graph; selected source node content digests checked; metadata defines scope"
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
