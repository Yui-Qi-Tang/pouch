package presentation

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/automata"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
)

func automatonBundle() automata.Bundle {
	forward := automata.Trace{States: []string{"01", "02"}, Choices: []string{""}}
	return automata.Bundle{SchemaVersion: automata.BundleVersion, RequestID: "fixture", AuthoritySHA256: strings.Repeat("a", 64), Status: "FOUND", Complete: true, ConclusionKind: "model_conditional_conclusion", Bits: 1, Display: automata.Display{Kind: "grid", Columns: 2, Rows: 1, Walls: []int{}}, Sources: run.SourceManifest{Sources: []run.Source{{ID: "source", File: "sources/s000.txt", SHA256: strings.Repeat("b", 64)}}}, Search: automata.Result{Forward: &forward}, ForwardVerified: true, RecoveryStatus: "NO_RETURN_IN_MODEL", Diagnostics: []string{}}
}

func TestAutomatonViewerIsSelfContained(t *testing.T) {
	var out bytes.Buffer
	if err := RenderAutomaton(&out, automatonBundle()); err != nil {
		t.Fatal(err)
	}
	page := out.String()
	if strings.Contains(page, "__POUCH_AUTOMATON_JSON__") || !strings.Contains(page, `"request_id":"fixture"`) {
		t.Fatal("bundle was not embedded")
	}
	for _, external := range []string{"http://", "https://", "<link", " src="} {
		if strings.Contains(page, external) {
			t.Fatalf("viewer loads %q", external)
		}
	}
}

func TestAutomatonViewerKeepsTextInsideItsScript(t *testing.T) {
	b := automatonBundle()
	b.RequestID = "</script><script>alert(1)</script>"
	var out bytes.Buffer
	if err := RenderAutomaton(&out, b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>alert") {
		t.Fatal("bundle text escaped its script element")
	}
}

func TestAutomatonViewerRejectsNonPortablePaths(t *testing.T) {
	b := automatonBundle()
	b.Sources.Sources[0].File = "/private/source.txt"
	if err := RenderAutomaton(&bytes.Buffer{}, b); err == nil {
		t.Fatal("accepted an absolute path")
	}
}
