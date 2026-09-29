package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// checkGraphSources binds selected nodes to the caller's statement digests.
// Topology and all unselected provider records remain opaque to the planner.
func checkGraphSources(g projection.Graph, in Inputs) error {
	nodes := make(map[string]projection.Record, len(g.Nodes))
	for _, node := range g.Nodes {
		nodes[node.ID] = node.Attributes
	}
	var payloads map[string]projection.Record
	for _, id := range in.Authority.SourceIDs() {
		node, ok := nodes[id]
		if !ok {
			return fmt.Errorf("graph selected source %q: node missing", id)
		}
		want, _ := in.Authority.SourceDigest(id)
		kind := ""
		if raw, ok := node["kind"]; ok {
			var err error
			kind, err = graphString(raw)
			if err != nil {
				return fmt.Errorf("graph selected source %q: kind: %w", id, err)
			}
		}
		_, hasRef := node["payload_ref"]
		if kind == "source_claim" || hasRef {
			ref, err := graphString(node["payload_ref"])
			if kind != "source_claim" || err != nil || ref == "" {
				return fmt.Errorf("graph selected source %q: source_claim and payload_ref required together", id)
			}
			if payloads == nil {
				payloads, err = graphPayloads(g.Metadata["payloads"])
				if err != nil {
					return fmt.Errorf("graph selected source %q: %w", id, err)
				}
			}
			payload, ok := payloads[ref]
			if !ok {
				return fmt.Errorf("graph selected source %q: payload missing", id)
			}
			claim, err := graphString(payload["claim"])
			if err != nil || claim == "" || validation.Digest([]byte(claim)) != want {
				return fmt.Errorf("graph selected source %q: payload claim binding mismatch", id)
			}
		} else if _, ok := node["content_sha256"]; !ok {
			return fmt.Errorf("graph selected source %q: content digest missing", id)
		}
		// When both representations are supplied, neither may contradict the other.
		if raw, ok := node["content_sha256"]; ok {
			digest, err := graphString(raw)
			if err != nil || digest != want {
				return fmt.Errorf("graph selected source %q: content digest binding mismatch", id)
			}
		}
		if raw, ok := node["content"]; ok {
			content, err := graphString(raw)
			if err != nil || validation.Digest([]byte(content)) != want {
				return fmt.Errorf("graph selected source %q: inline content binding mismatch", id)
			}
		}
	}
	return nil
}

// The payload table is an ID namespace: duplicate IDs are invalid even when a
// duplicate is unselected. Only selected claim content is interpreted.
func graphPayloads(raw json.RawMessage) (map[string]projection.Record, error) {
	var records []projection.Record
	if err := json.Unmarshal(raw, &records); err != nil || records == nil {
		return nil, errors.New("payloads must be an array of records")
	}
	out := make(map[string]projection.Record, len(records))
	for _, record := range records {
		id, err := graphString(record["id"])
		if err != nil || id == "" {
			return nil, errors.New("payload ID must be a nonempty string")
		}
		if _, exists := out[id]; exists {
			return nil, errors.New("duplicate payload ID")
		}
		out[id] = record
	}
	return out, nil
}

// graphString hashes decoded UTF-8 text without normalization. encoding/json
// replaces unpaired surrogate escapes; reject those instead of binding altered
// text. projection.Decode has already checked raw UTF-8 and duplicate JSON keys.
func graphString(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	var text string
	if len(raw) == 0 || raw[0] != '"' {
		return "", errors.New("expected JSON string")
	}
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", err
	}
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++ // JSON syntax validation above guarantees a following escape byte.
		if raw[i] != 'u' {
			continue
		}
		unit, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return "", err
		}
		i += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return "", errors.New("unpaired low surrogate")
		}
		if unit < 0xd800 || unit > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return "", errors.New("unpaired high surrogate")
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return "", errors.New("invalid surrogate pair")
		}
		i += 6
	}
	return text, nil
}
