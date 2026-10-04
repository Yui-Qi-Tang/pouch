package projection

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// NormalizeEmptyEdges is an explicit representation conversion, not an absence
// inference. The caller must establish that this is a complete zero-edge view.
// Decode remains strict. Both original and converted digests must be retained.
func NormalizeEmptyEdges(raw []byte, expected string) ([]byte, error) {
	if validation.Digest(raw) != expected {
		return nil, errors.New("graph digest mismatch")
	}
	if err := strictObjectJSON(raw); err != nil {
		return nil, err
	}
	root, err := object(raw, []string{"schema_version", "snapshot_id", "nodes", "edges"})
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(bytes.TrimSpace(root["edges"]), []byte("null")) {
		return nil, errors.New("explicit null edges required")
	}
	root["edges"] = json.RawMessage("[]")
	normalized, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	if _, err := decodeGraph(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}
