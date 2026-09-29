package run

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func sourceGraph() map[string]any {
	return map[string]any{
		"schema_version": "canonical-evidence-graph/v1", "snapshot_id": "test-snapshot",
		"nodes":    []any{map[string]any{"id": "source:1", "kind": "source_claim", "payload_ref": "payload:1"}},
		"edges":    []any{},
		"payloads": []any{map[string]any{"id": "payload:1", "claim": "rule", "span": "not the selected statement"}},
		"scope":    map[string]any{"truncated": true, "global_absence_inference_allowed": false},
	}
}

func TestAttachNativeClaimPreservesGraph(t *testing.T) {
	for _, form := range []string{"native", "native-with-digest", "native-with-content", "inline-content", "inline-digest"} {
		t.Run(form, func(t *testing.T) {
			in := simpleInput(t, false, 0)
			g := sourceGraph()
			n := g["nodes"].([]any)[0].(map[string]any)
			if form != "native" {
				n["content_sha256"] = validation.Digest([]byte("rule"))
			}
			if strings.Contains(form, "content") {
				n["content"] = "rule"
			}
			if strings.HasPrefix(form, "inline") {
				delete(n, "payload_ref")
				delete(n, "kind")
				delete(g, "payloads")
			}
			raw, err := json.MarshalIndent(g, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, '\n')
			out := t.TempDir()
			b := Bundle{RequestID: "q"}
			if err := AttachEvidence(&b, in, raw, validation.Digest(raw), out); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(filepath.Join(out, "evidence-graph.json"))
			if err != nil || !bytes.Equal(saved, raw) {
				t.Fatal("original graph bytes not retained", err)
			}
			original, err := projection.Decode(raw, validation.Digest(raw))
			if err != nil {
				t.Fatal(err)
			}
			restored, err := projection.Reconstruct(b.EvidenceMatrix)
			if err != nil || !reflect.DeepEqual(graphValue(t, restored), graphValue(t, original.Graph())) || b.EvidenceMatrix.SourceSHA256 != validation.Digest(raw) {
				t.Fatal("matrix lost graph identity or sidecars", err)
			}
		})
	}
}

func TestGraphSourceBindingRefusals(t *testing.T) {
	tests := map[string]func(map[string]any, map[string]any, map[string]any){
		"changed-claim":            func(g, n, p map[string]any) { p["claim"] = "changed" },
		"correct-span-wrong-claim": func(g, n, p map[string]any) { p["span"] = "rule"; p["claim"] = "changed" },
		"forged-digest-changed-claim": func(g, n, p map[string]any) {
			n["content_sha256"] = validation.Digest([]byte("rule"))
			p["claim"] = "changed"
		},
		"conflicting-inline-content": func(g, n, p map[string]any) { n["content"] = "changed" },
		"inline-null-content":        func(g, n, p map[string]any) { n["content"] = nil },
		"wrong-digest":               func(g, n, p map[string]any) { n["content_sha256"] = validation.Digest([]byte("changed")) },
		"null-digest":                func(g, n, p map[string]any) { n["content_sha256"] = nil },
		"wrong-reference":            func(g, n, p map[string]any) { n["payload_ref"] = "payload:missing" },
		"null-reference":             func(g, n, p map[string]any) { n["payload_ref"] = nil },
		"missing-reference-with-digest": func(g, n, p map[string]any) {
			delete(n, "payload_ref")
			n["content_sha256"] = validation.Digest([]byte("rule"))
		},
		"unsupported-kind-with-digest": func(g, n, p map[string]any) {
			n["kind"] = "raw_evidence"
			n["content_sha256"] = validation.Digest([]byte("rule"))
		},
		"missing-kind":  func(g, n, p map[string]any) { delete(n, "kind") },
		"null-claim":    func(g, n, p map[string]any) { p["claim"] = nil },
		"numeric-claim": func(g, n, p map[string]any) { p["claim"] = 1 },
		"missing-claim": func(g, n, p map[string]any) { delete(p, "claim") },
		"aliased-claim": func(g, n, p map[string]any) { delete(p, "claim"); p["Claim"] = "rule" },
		"duplicate-payload-id": func(g, n, p map[string]any) {
			g["payloads"] = append(g["payloads"].([]any), map[string]any{"id": "payload:1", "claim": "rule"})
		},
		"null-payload-list":     func(g, n, p map[string]any) { g["payloads"] = nil },
		"object-payload-list":   func(g, n, p map[string]any) { g["payloads"] = map[string]any{"payload:1": p} },
		"missing-selected-node": func(g, n, p map[string]any) { n["id"] = "source:other" },
		"inline-forged-digest": func(g, n, p map[string]any) {
			delete(n, "kind")
			delete(n, "payload_ref")
			n["content"] = "changed"
			n["content_sha256"] = validation.Digest([]byte("rule"))
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			g := sourceGraph()
			mutate(g, g["nodes"].([]any)[0].(map[string]any), g["payloads"].([]any)[0].(map[string]any))
			raw, err := json.Marshal(g)
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			// Re-pin the changed graph; authority stays fixed. An outer hash alone is insufficient.
			if err := AttachEvidence(&Bundle{RequestID: "q"}, simpleInput(t, false, 0), raw, validation.Digest(raw), out); err == nil {
				t.Fatal("invalid selected-source binding accepted")
			}
			if _, err := os.Stat(filepath.Join(out, "evidence-graph.json")); !os.IsNotExist(err) {
				t.Fatal("rejected graph was saved", err)
			}
		})
	}
}

func TestGraphClaimUnicodeAndExactWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name, claim, statement string
		accepted               bool
	}{
		{"unicode-and-newlines", `"\u03c0\r\n"`, "π\r\n", true},
		{"valid-surrogate-pair", `"\ud83d\ude00"`, "😀", true},
		{"literal-replacement", `"\ufffd"`, "�", true},
		{"literal-backslash-u", `"\\ud800"`, `\ud800`, true},
		{"unpaired-high", `"\ud800"`, "�", false},
		{"unpaired-low", `"\udc00"`, "�", false},
		{"trailing-newline-removed", `"rule"`, "rule\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := simpleInput(t, false, 0)
			c := in.Authority.Contract()
			c.SourceBinding["source:1"] = validation.Digest([]byte(tc.statement))
			a, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			in.Authority, err = validation.NewAuthority(a, validation.Digest(a))
			if err != nil {
				t.Fatal(err)
			}
			g := sourceGraph()
			g["payloads"].([]any)[0].(map[string]any)["claim"] = json.RawMessage(tc.claim)
			raw, err := json.Marshal(g)
			if err != nil {
				t.Fatal(err)
			}
			err = AttachEvidence(&Bundle{RequestID: "q"}, in, raw, validation.Digest(raw), t.TempDir())
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v, error=%v", tc.accepted, err)
			}
		})
	}
}

func graphValue(t *testing.T, g projection.Graph) any {
	t.Helper()
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
