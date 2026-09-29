package projection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func decodeGraph(raw []byte) (Graph, error) {
	if err := strictObjectJSON(raw); err != nil {
		return Graph{}, err
	}
	root, err := object(raw, []string{"schema_version", "snapshot_id", "nodes", "edges"})
	if err != nil {
		return Graph{}, err
	}
	schema, err := stringField(root, "schema_version")
	if err != nil {
		return Graph{}, err
	}
	id, err := stringField(root, "snapshot_id")
	if err != nil {
		return Graph{}, err
	}
	nodeRecords, err := arrayField(root, "nodes")
	if err != nil {
		return Graph{}, err
	}
	edgeRecords, err := arrayField(root, "edges")
	if err != nil {
		return Graph{}, err
	}
	if len(nodeRecords) > MaxNodes || len(edgeRecords) > MaxEdges {
		return Graph{}, errors.New("graph exceeds record limits")
	}
	g := Graph{SchemaVersion: schema, SnapshotID: id, Metadata: root, Nodes: make([]Node, len(nodeRecords)), Edges: make([]Edge, len(edgeRecords))}
	delete(root, "schema_version")
	delete(root, "snapshot_id")
	delete(root, "nodes")
	delete(root, "edges")
	for i, raw := range nodeRecords {
		record, err := object(raw, []string{"id"})
		if err != nil {
			return Graph{}, fmt.Errorf("node %d: %w", i, err)
		}
		id, err := stringField(record, "id")
		if err != nil {
			return Graph{}, err
		}
		delete(record, "id")
		g.Nodes[i] = Node{ID: id, Attributes: record}
	}
	for i, raw := range edgeRecords {
		record, err := object(raw, []string{"id", "from", "to", "relation"})
		if err != nil {
			return Graph{}, fmt.Errorf("edge %d: %w", i, err)
		}
		values := make([]string, 4)
		for j, key := range []string{"id", "from", "to", "relation"} {
			values[j], err = stringField(record, key)
			if err != nil {
				return Graph{}, err
			}
			delete(record, key)
		}
		g.Edges[i] = Edge{ID: values[0], From: values[1], To: values[2], Relation: values[3], Attributes: record}
	}
	if err := validateGraph(g); err != nil {
		return Graph{}, err
	}
	return g, nil
}

func object(raw []byte, reserved []string) (Record, error) {
	if len(raw) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return nil, errors.New("JSON object required")
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	for key := range record {
		for _, name := range reserved {
			if strings.EqualFold(key, name) && key != name {
				return nil, fmt.Errorf("field alias %q is unsupported", key)
			}
		}
	}
	return record, nil
}

func stringField(record Record, key string) (string, error) {
	raw, ok := record[key]
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return "", fmt.Errorf("string field %q is required", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || !validName(value) {
		return "", fmt.Errorf("invalid string field %q", key)
	}
	return value, nil
}

func arrayField(record Record, key string) ([]json.RawMessage, error) {
	raw, ok := record[key]
	if !ok || len(raw) == 0 || raw[0] != '[' {
		return nil, fmt.Errorf("array field %q is required", key)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func validName(value string) bool {
	return len(value) > 0 && len(value) <= 1024 && strings.TrimSpace(value) != ""
}

func validateGraph(g Graph) error {
	if g.SchemaVersion != SchemaVersion || !validName(g.SnapshotID) {
		return errors.New("unsupported graph schema or snapshot ID")
	}
	if len(g.Nodes) > MaxNodes || len(g.Edges) > MaxEdges {
		return errors.New("graph exceeds record limits")
	}
	nodes := make(map[string]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		if !validName(node.ID) || nodes[node.ID] {
			return errors.New("invalid or duplicate node ID")
		}
		nodes[node.ID] = true
	}
	edges := make(map[string]bool, len(g.Edges))
	for _, edge := range g.Edges {
		if !validName(edge.ID) || edges[edge.ID] || !validName(edge.Relation) {
			return errors.New("invalid edge ID or relation")
		}
		edges[edge.ID] = true
		if !nodes[edge.From] || !nodes[edge.To] {
			return errors.New("edge references a missing node")
		}
	}
	return nil
}

// uniqueJSON rejects duplicates and case aliases before encoding/json can
// silently overwrite them. Opaque provider values, including null, stay opaque.
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON exceeds nesting limit")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if depth == 0 && (!container || delim != '{') {
		return errors.New("graph must be a JSON object")
	}
	if !container {
		return nil
	}
	seen := make(map[string]bool)
	for d.More() {
		if delim == '{' {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			normalized := strings.ToLower(key)
			if seen[normalized] {
				return fmt.Errorf("duplicate or aliased JSON key %q", key)
			}
			seen[normalized] = true
		}
		if err := uniqueJSON(d, depth+1); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func attributes(record Record, reserved ...string) (Record, error) {
	for key := range record {
		for _, name := range reserved {
			if strings.EqualFold(key, name) {
				return nil, fmt.Errorf("sidecar contains reserved field %q", key)
			}
		}
	}
	return cloneRecord(record), nil
}

func jsonString(value string) json.RawMessage { raw, _ := json.Marshal(value); return raw }

// MarshalJSON restores the canonical node record shape.
func (n Node) MarshalJSON() ([]byte, error) {
	record, err := attributes(n.Attributes, "id")
	if err != nil {
		return nil, err
	}
	record["id"] = jsonString(n.ID)
	return json.Marshal(record)
}

// MarshalJSON restores the canonical edge record shape.
func (e Edge) MarshalJSON() ([]byte, error) {
	record, err := attributes(e.Attributes, "id", "from", "to", "relation")
	if err != nil {
		return nil, err
	}
	record["id"], record["from"], record["to"], record["relation"] = jsonString(e.ID), jsonString(e.From), jsonString(e.To), jsonString(e.Relation)
	return json.Marshal(record)
}

// MarshalJSON reconstructs the graph shape. It does not preserve original
// whitespace or record ordering; use Snapshot.Raw for exact source bytes.
func (g Graph) MarshalJSON() ([]byte, error) {
	record, err := attributes(g.Metadata, "schema_version", "snapshot_id", "nodes", "edges")
	if err != nil {
		return nil, err
	}
	nodes, err := json.Marshal(g.Nodes)
	if err != nil {
		return nil, err
	}
	edges, err := json.Marshal(g.Edges)
	if err != nil {
		return nil, err
	}
	record["schema_version"], record["snapshot_id"] = jsonString(g.SchemaVersion), jsonString(g.SnapshotID)
	record["nodes"], record["edges"] = nodes, edges
	return json.Marshal(record)
}

// UnmarshalJSON decodes a node without discarding unknown provider fields.
func (n *Node) UnmarshalJSON(raw []byte) error {
	if err := strictObjectJSON(raw); err != nil {
		return err
	}
	record, err := object(raw, []string{"id"})
	if err != nil {
		return err
	}
	id, err := stringField(record, "id")
	if err != nil {
		return err
	}
	delete(record, "id")
	*n = Node{ID: id, Attributes: record}
	return nil
}

// UnmarshalJSON decodes an edge without discarding unknown provider fields.
func (e *Edge) UnmarshalJSON(raw []byte) error {
	if err := strictObjectJSON(raw); err != nil {
		return err
	}
	record, err := object(raw, []string{"id", "from", "to", "relation"})
	if err != nil {
		return err
	}
	values := make([]string, 4)
	for i, key := range []string{"id", "from", "to", "relation"} {
		values[i], err = stringField(record, key)
		if err != nil {
			return err
		}
		delete(record, key)
	}
	*e = Edge{ID: values[0], From: values[1], To: values[2], Relation: values[3], Attributes: record}
	return nil
}

// UnmarshalJSON checks the supported graph shape and preserves raw sidecars.
// This validates structure only; use Decode to check a caller-supplied digest.
func (g *Graph) UnmarshalJSON(raw []byte) error {
	decoded, err := decodeGraph(raw)
	if err != nil {
		return err
	}
	*g = decoded
	return nil
}

func strictObjectJSON(raw []byte) error {
	if len(raw) > MaxBytes || !utf8.Valid(raw) {
		return errors.New("invalid JSON size or UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := uniqueJSON(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}
