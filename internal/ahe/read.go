package ahe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

type ReadRequest struct {
	RootNodeIDs []string `json:"root_node_ids"`
	Relations   []string `json:"relations"`
	MaxDepth    int      `json:"max_depth"`
	MaxNodes    int      `json:"max_nodes"`
	MaxEdges    int      `json:"max_edges"`
}
type View struct {
	Handle                        string   `json:"handle"`
	SnapshotID                    string   `json:"snapshot_id"`
	RootNodeIDs                   []string `json:"root_node_ids"`
	Relations                     []string `json:"relations"`
	MaxDepth                      int      `json:"max_depth"`
	MaxNodes                      int      `json:"max_nodes"`
	MaxEdges                      int      `json:"max_edges"`
	NodeCount                     int      `json:"node_count"`
	EdgeCount                     int      `json:"edge_count"`
	Truncated                     *bool    `json:"truncated"`
	GlobalAbsenceInferenceAllowed *bool    `json:"global_absence_inference_allowed"`
}

// Snapshot preserves materialized evidence; its handle alone is not evidence.
// A complete bounded view never establishes global absence. CLI ahe-read
// prints this provider-specific wrapper; it is not a planning input contract.
// Hash scopes, nullable View flags and field values are defined in README.md.
type Snapshot struct {
	SchemaVersion string          `json:"schema_version"`
	View          View            `json:"view"`
	Artifact      json.RawMessage `json:"artifact"`
	ArtifactBytes []byte          `json:"artifact_bytes_base64"`
	// Exact tool structuredContent bytes, not the full JSON-RPC envelope.
	RawResponse    []byte `json:"raw_response_base64"`
	ResponseSHA256 string `json:"response_sha256"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

func ReadSnapshot(ctx context.Context, query *Profile, req ReadRequest) (Snapshot, error) {
	var s Snapshot
	if query == nil || query.kind != ProfileQuery {
		return s, ErrCapabilityUnavailable
	}
	if len(req.RootNodeIDs) < 1 || len(req.RootNodeIDs) > 32 || req.MaxDepth < 0 || req.MaxDepth > 8 || req.MaxNodes < 1 || req.MaxNodes > 1024 || req.MaxEdges < 0 || req.MaxEdges > 4096 || (req.MaxDepth > 0 && req.MaxEdges == 0) {
		return s, errors.New("invalid bounded read scope")
	}
	roots := slices.Clone(req.RootNodeIDs)
	slices.Sort(roots)
	for i, id := range roots {
		if !strings.HasPrefix(id, "canon-node:") || len(id) > 200 || (i > 0 && id == roots[i-1]) {
			return s, errors.New("invalid canonical root ID")
		}
	}
	raw, err := query.call(ctx, "open_canonical_read_view", req)
	if err != nil {
		return s, err
	}
	var out struct {
		SchemaVersion string          `json:"schema_version"`
		View          View            `json:"view"`
		Artifact      json.RawMessage `json:"artifact"`
	}
	if json.Unmarshal(raw, &out) != nil || out.SchemaVersion != "canonical-read-view-query-v1" || out.View.Handle == "" || out.View.SnapshotID == "" || out.View.Truncated == nil || out.View.GlobalAbsenceInferenceAllowed == nil || *out.View.GlobalAbsenceInferenceAllowed {
		return s, errors.New("invalid materialized read descriptor")
	}
	v := out.View
	gotRoots := slices.Clone(v.RootNodeIDs)
	slices.Sort(gotRoots)
	if !slices.Equal(roots, gotRoots) || v.MaxDepth != req.MaxDepth || v.MaxNodes != req.MaxNodes || v.MaxEdges != req.MaxEdges {
		return s, errors.New("read scope mismatch")
	}
	if len(req.Relations) > 0 {
		want := slices.Clone(req.Relations)
		got := slices.Clone(v.Relations)
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(want, got) {
			return s, errors.New("read relation scope mismatch")
		}
	}
	var artifact struct {
		SchemaVersion string            `json:"schema_version"`
		SnapshotID    string            `json:"snapshot_id"`
		Nodes         []json.RawMessage `json:"nodes"`
		Edges         []json.RawMessage `json:"edges"`
	}
	if json.Unmarshal(out.Artifact, &artifact) != nil || artifact.SchemaVersion != "canonical-evidence-graph/v1" || artifact.SnapshotID != v.SnapshotID || len(artifact.Nodes) != v.NodeCount || len(artifact.Edges) != v.EdgeCount || v.NodeCount > req.MaxNodes || v.EdgeCount > req.MaxEdges {
		return s, errors.New("artifact and read descriptor mismatch")
	}
	return Snapshot{SchemaVersion: out.SchemaVersion, View: v, Artifact: bytes.Clone(out.Artifact), ArtifactBytes: bytes.Clone(out.Artifact), RawResponse: bytes.Clone(raw), ResponseSHA256: validation.Digest(raw), ArtifactSHA256: validation.Digest(out.Artifact)}, nil
}

// Record is a minimal checked projection; Raw retains every native provenance field.
// StatementSHA256 intentionally binds only statement_text UTF-8, the existing v1
// source_binding projection. It is not a hash of all AHE metadata or original bytes.
type Record struct {
	CanonicalID     string          `json:"canonical_id"`
	Statement       string          `json:"statement"`
	StatementSHA256 string          `json:"statement_sha256"`
	Raw             json.RawMessage `json:"raw"`
}

func ReadRecord(ctx context.Context, query *Profile, id string) (Record, error) {
	var r Record
	if query == nil || query.kind != ProfileQuery {
		return r, ErrCapabilityUnavailable
	}
	if !strings.HasPrefix(id, "canon-node:") || len(id) > 200 {
		return r, errors.New("exact canonical ID required")
	}
	raw, err := query.call(ctx, "get_evidence_record", map[string]string{"canonical_id": id})
	if err != nil {
		return r, err
	}
	var out struct {
		RecordRef struct {
			ID string `json:"id"`
		} `json:"record_ref"`
		CanonicalRef string          `json:"canonical_ref"`
		Statement    string          `json:"statement_text"`
		Admission    string          `json:"admission_outcome"`
		Canonical    json.RawMessage `json:"canonical"`
	}
	if json.Unmarshal(raw, &out) != nil || out.RecordRef.ID != id || out.CanonicalRef != id || out.Admission != "admitted" || out.Statement == "" || len(out.Canonical) == 0 || string(out.Canonical) == "null" {
		return r, fmt.Errorf("native source identity or admission mismatch: %s", id)
	}
	return Record{CanonicalID: id, Statement: out.Statement, StatementSHA256: validation.Digest([]byte(out.Statement)), Raw: bytes.Clone(raw)}, nil
}
