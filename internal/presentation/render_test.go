package presentation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func sampleBundle() run.Bundle {
	trace := validation.Trace{States: []validation.State{{"mode": "before"}, {"mode": "after"}}, Actions: []string{"apply"}, ClaimShortest: true}
	ret := validation.Trace{States: []validation.State{{"mode": "after"}, {"mode": "before"}}, Actions: []string{"restore"}, ClaimShortest: true}
	return run.Bundle{
		SchemaVersion: "pouch-bundle/v1", ProjectVersion: "test", RequestID: "fixture", AuthoritySHA256: strings.Repeat("a", 64), Status: "FOUND", Complete: true, ConclusionKind: "model_conditional_conclusion",
		Contract:      validation.Contract{SchemaVersion: validation.Version, RequestID: "fixture", SourceBinding: map[string]string{"source": strings.Repeat("b", 64)}, Assumptions: map[string]string{"scope": "declared model"}, Model: validation.Model{Fields: map[string][]string{"mode": {"before", "after"}}, Forbidden: []validation.State{}, Actions: []validation.Action{{ID: "apply", Guard: validation.State{"mode": "before"}, Effect: validation.State{"mode": "after"}, Frame: []string{}}}}, Start: validation.State{"mode": "before"}, Goal: validation.State{"mode": "after"}, Baseline: validation.State{"mode": "before"}, ForwardLimit: 2, ReturnLimit: 2},
		Sources:       run.SourceManifest{SchemaVersion: "pouch-sources/v1", RequestID: "fixture", Sources: []run.Source{{ID: "source", File: "sources/s000.txt", SHA256: strings.Repeat("b", 64)}}},
		EvidenceScope: "selected records; original relationships unknown",
		Search:        planning.Result{Policy: "simple-first-hit/action-sequence", Forward: []planning.Path{{ID: "p000000", Trace: trace, QueryID: "q000001", ReturnIndex: 0}}, Returns: []planning.Return{{Endpoint: trace.States[1], Status: "RETURN_FOUND", Trace: &ret}}, Queries: []planning.Query{{ID: "q000001", Status: "SAT", Verified: true}}, ForwardComplete: true, Complete: true, Matrix: planning.Matrix{Fields: []string{"mode"}, ActionIDs: []string{"apply"}, Coordinates: []planning.Coordinate{{Field: "mode", Value: "before"}, {Field: "mode", Value: "after"}}, Guard: [][]int{{1, 0}}, Effect: [][]int{{0, 1}}, Frame: [][]int{{0}}}},
		Paths:         []run.PathResult{{ID: "p000000", ForwardVerified: true, RecoveryStatus: "RETURN_VERIFIED", Return: &ret, PackageFile: "packages/p000000.json"}}, Diagnostics: []string{},
	}
}

func embeddedJSON(t *testing.T, html string) []byte {
	t.Helper()
	const opening = `<script id="pouch-bundle" type="application/json">`
	start := strings.Index(html, opening)
	if start < 0 {
		t.Fatal("missing embedded AI bundle")
	}
	start += len(opening)
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		t.Fatal("missing JSON script terminator")
	}
	return []byte(html[start : start+end])
}

func TestRenderContainsExactBundleAndReplayControls(t *testing.T) {
	bundle := sampleBundle()
	var output bytes.Buffer
	if err := Render(&output, bundle); err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embeddedJSON(t, output.String()), want) {
		t.Fatal("viewer embeds a different bundle than AI JSON")
	}
	for _, id := range []string{"path", "direction", "previous", "step", "next", "download", "state-body", "guard", "effect", "frame", "evidence-graph", "sources", "receipt", "path-summary", "goal-title", "goal"} {
		if !strings.Contains(output.String(), `id="`+id+`"`) {
			t.Errorf("missing UI control %s", id)
		}
	}
	if strings.Contains(output.String(), "__POUCH_BUNDLE_JSON__") {
		t.Fatal("unresolved template placeholder")
	}
	if !strings.Contains(output.String(), "new Blob([bundleText+") {
		t.Fatal("download must preserve exact embedded JSON, including large opaque numbers")
	}
	if strings.Contains(output.String(), "innerHTML") || strings.Contains(output.String(), "<script src=") || strings.Contains(output.String(), "<link ") {
		t.Fatal("viewer relies on unsafe HTML insertion or remote assets")
	}
}

func TestDescribeReturnUsesCompleteEndpointAndBaseline(t *testing.T) {
	b := sampleBundle()
	b.Contract.Goal = validation.State{"mode": "after"}
	b.Contract.Start["checkpoint"] = "retained"
	b.Contract.Baseline["checkpoint"] = "retained"
	end := validation.State{"mode": "after", "checkpoint": "consumed"}
	b.Search.Forward[0].Trace.States[1] = end
	b.Paths[0].Receipt = &validation.Receipt{ForwardCost: 1, ReturnKind: "return", ReturnCost: 2}
	b.Contract.Model.Actions = append(b.Contract.Model.Actions, validation.Action{ID: "restore"})
	b.Contract.Assumptions["complete_original_declaration"] = `{"assumptions":[{"id":"S1","kind":"scenario_assumption","statement":"Only model checkpoint restoration is declared."},{"id":"F1","kind":"scope_limit","statement":"Forward-only assumption."}],"actions":[{"id":"restore","basis":["S1"]},{"id":"apply","basis":["F1"]}]}`
	d := describeBundle(b).Paths["p000000"]
	if !reflect.DeepEqual(d.Return.Start, end) || !reflect.DeepEqual(d.Return.Goal, b.Contract.Baseline) {
		t.Fatalf("return query lost endpoint fields or reused forward goal: %+v", d.Return)
	}
	if !reflect.DeepEqual(d.Forward.Start, b.Contract.Start) || !reflect.DeepEqual(d.Forward.Goal, b.Contract.Goal) {
		t.Fatalf("forward query changed: %+v", d.Forward)
	}
	if d.ForwardCost == nil || *d.ForwardCost != 1 || d.ReturnCost == nil || *d.ReturnCost != 2 {
		t.Fatalf("costs must come from the saved receipt, not the search limit or trace length: %+v", d)
	}
	if len(d.ReturnAssumptions) != 1 || d.ReturnAssumptions[0].ID != "S1" {
		t.Fatalf("return scope not tied to return operations: %+v", d.ReturnAssumptions)
	}
}

func TestDescribeReturnCostDoesNotTurnMissingOrNoReturnIntoZero(t *testing.T) {
	for _, status := range []string{"UNKNOWN", "NO_RETURN_IN_MODEL", "RETURN_VERIFIED"} {
		t.Run(status, func(t *testing.T) {
			b := sampleBundle()
			b.Paths[0].RecoveryStatus = status
			if d := describeBundle(b).Paths["p000000"]; d.ForwardCost != nil || d.ReturnCost != nil {
				t.Fatal("missing receipt became a certified cost")
			}
			b.Paths[0].Receipt = &validation.Receipt{ForwardCost: 1, ReturnKind: "no_return"}
			if describeBundle(b).Paths["p000000"].ReturnCost != nil {
				t.Fatal("no-return receipt became zero-cost recovery")
			}
			b.Paths[0].Receipt.ReturnKind = "return"
			d := describeBundle(b).Paths["p000000"]
			if status == "RETURN_VERIFIED" {
				if d.ReturnCost == nil || *d.ReturnCost != 0 {
					t.Fatal("a recorded zero-operation return was hidden")
				}
			} else if d.ReturnCost != nil {
				t.Fatal("unverified return was presented as a certified cost")
			}
		})
	}
}

func TestDescribeReviewedDjangoScenariosAreDistinct(t *testing.T) {
	for _, scenario := range []string{"original_inner", "candidate_outer"} {
		t.Run(scenario, func(t *testing.T) {
			b := sampleBundle()
			b.Search.Forward[0].Trace.Actions = []string{"build_" + scenario, "await_view_" + scenario, "observe_" + scenario}
			b.Search.Forward[0].Trace.States[1]["chain"] = scenario
			b.Contract.Assumptions["complete_original_declaration"] = `{"case":"django-13344","assumptions":[],"actions":[]}`
			d := describeBundle(b).Paths["p000000"]
			if d.Scenario != scenario || d.Note == "" {
				t.Fatalf("reviewed scenario lacks its qualification: %+v", d)
			}
			if scenario == "original_inner" && !strings.Contains(d.Note, "does not preserve") {
				t.Fatal("reordered scenario presented as preserving the original scenario")
			}
			if scenario == "candidate_outer" && !strings.Contains(d.Note, "not full Django or official repair acceptance") {
				t.Fatal("candidate scenario promoted to full repair acceptance")
			}
			b.Contract.Assumptions["complete_original_declaration"] = `{"case":"unrelated","assumptions":[],"actions":[]}`
			if describeBundle(b).Paths["p000000"].Note != "" {
				t.Fatal("example-specific interpretation leaked into another case")
			}
			b.Contract.Assumptions["complete_original_declaration"] = `{"case":"django-13344","assumptions":[],"actions":[]}`
			b.Search.Forward[0].Trace.Actions[0] = "another_operation"
			if describeBundle(b).Paths["p000000"].Note != "" {
				t.Fatal("unreviewed path inherited a reviewed path's interpretation")
			}
		})
	}
}

func TestRenderEscapesUntrustedSourceAndModelText(t *testing.T) {
	bundle := sampleBundle()
	attack := `</script><img src=x onerror=alert(1)>`
	bundle.RequestID = attack
	bundle.Contract.Assumptions["untrusted"] = attack
	bundle.Sources.Sources[0].ID = attack
	var output bytes.Buffer
	if err := Render(&output, bundle); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), attack) {
		t.Fatal("untrusted input escaped the JSON script")
	}
	var got run.Bundle
	if err := json.Unmarshal(embeddedJSON(t, output.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.RequestID != attack || got.Contract.Assumptions["untrusted"] != attack || got.Sources.Sources[0].ID != attack {
		t.Fatal("escaping changed original data")
	}
}

func TestRenderRejectsNonPortableLinks(t *testing.T) {
	for _, name := range []string{"", "/private/source.txt", "../source.txt", "sources/../source.txt", "https://example.org/a", "javascript:alert(1)", "C:\\secret.txt", "sources\\secret.txt", "a\n.txt"} {
		t.Run(name, func(t *testing.T) {
			b := sampleBundle()
			b.Sources.Sources[0].File = name
			var out bytes.Buffer
			if err := Render(&out, b); err == nil {
				t.Fatal("accepted non-portable source link")
			}
			if out.Len() != 0 {
				t.Fatal("wrote an unsafe partial document")
			}
		})
	}
	b := sampleBundle()
	b.Paths[0].PackageFile = "../package.json"
	if err := Render(io.Discard, b); err == nil {
		t.Fatal("accepted escaping package link")
	}
}

func TestRenderEmptyAndIncompleteResults(t *testing.T) {
	b := sampleBundle()
	b.Complete = false
	b.Status = "INCONCLUSIVE"
	b.Search.Forward = nil
	b.Search.ForwardComplete = false
	b.Paths = nil
	var out bytes.Buffer
	if err := Render(&out, b); err != nil {
		t.Fatal(err)
	}
	var got run.Bundle
	if err := json.Unmarshal(embeddedJSON(t, out.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Complete || got.Status != "INCONCLUSIVE" || got.Search.ForwardComplete {
		t.Fatal("viewer upgraded an incomplete result")
	}
	for _, notice := range []string{"is incomplete; additional paths may exist.", "No checked trace is available", "No forward witness was found"} {
		if !strings.Contains(out.String(), notice) {
			t.Errorf("missing incomplete-result message %q", notice)
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func TestRenderPropagatesWriteFailure(t *testing.T) {
	if err := Render(brokenWriter{}, sampleBundle()); err == nil {
		t.Fatal("lost write error")
	}
}

func TestDescribeStructuredAssumptionsAndReferences(t *testing.T) {
	b := sampleBundle()
	b.Contract.Assumptions["complete_original_declaration"] = `{"assumptions":[{"id":"A1","kind":"scenario_assumption","statement":"Retain a checkpoint.","basis":["checkpoint-source"]}],"actions":[{"id":"apply","basis":["A1"]},{"id":"undeclared","basis":["A2"]}]}`
	b.Contract.Assumptions["current_native_source_mapping"] = `{"checkpoint-source":"source","missing":"not-bound"}`
	want, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	d := describeBundle(b)
	if len(d.Assumptions) != 1 || d.Assumptions[0].Statement != "Retain a checkpoint." || d.Assumptions[0].Kind != "scenario_assumption" {
		t.Fatalf("lost declared assumption: %+v", d.Assumptions)
	}
	if len(d.ActionBasis) != 1 || len(d.ActionBasis["apply"]) != 1 || d.ActionBasis["apply"][0] != "A1" {
		t.Fatalf("invented or lost action references: %+v", d.ActionBasis)
	}
	if len(d.SourceNames) != 1 || d.SourceNames["source"][0] != "checkpoint-source" || len(d.Notices) != 1 {
		t.Fatalf("unbound source alias shown as bound: %+v", d)
	}
	if len(d.OtherAssumptions) != 1 || d.OtherAssumptions["scope"] != "declared model" {
		t.Fatalf("failed to separate readable assumptions: %+v", d.OtherAssumptions)
	}
	var out bytes.Buffer
	if err := Render(&out, b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, embeddedJSON(t, out.String())) {
		t.Fatal("display projection changed bound AI data")
	}
}

func TestDescribeRetainsUnrecognizedDeclarationText(t *testing.T) {
	for _, raw := range []string{"not JSON", "null", "{}", `{"assumptions":"not an array","actions":[]}`} {
		t.Run(raw, func(t *testing.T) {
			b := sampleBundle()
			b.Contract.Assumptions["complete_original_declaration"] = raw
			b.Contract.Assumptions["current_native_source_mapping"] = raw
			d := describeBundle(b)
			if len(d.Assumptions) != 0 || len(d.ActionBasis) != 0 || d.OtherAssumptions["complete_original_declaration"] != raw {
				t.Fatalf("unrecognized declaration was reinterpreted or lost: %+v", d)
			}
			if len(d.Notices) == 0 {
				t.Fatal("missing fallback explanation")
			}
		})
	}
}

func TestDescribeUnknownTopologyIsNotAnEdgelessGraph(t *testing.T) {
	b := sampleBundle()
	if describeBundle(b).TopologySupplied {
		t.Fatal("nil matrix shown as a supplied graph")
	}
	b.EvidenceMatrix = &projection.Matrix{NodeIDs: []string{"source"}, Metadata: projection.Record{"scope": json.RawMessage(`{"topology":"not_supplied","absence_of_edges":"unknown"}`)}}
	if describeBundle(b).TopologySupplied {
		t.Fatal("source-only records shown as independent evidence nodes")
	}
	b.EvidenceMatrix.Metadata = projection.Record{}
	if !describeBundle(b).TopologySupplied {
		t.Fatal("an explicitly supplied graph was hidden only because it had no edges")
	}
}

func TestRenderDisplayDataCannotReplaceTemplateOrEscapeScript(t *testing.T) {
	b := sampleBundle()
	b.RequestID = "__POUCH_VIEW_DETAILS_JSON__"
	b.Contract.Assumptions["complete_original_declaration"] = `{"assumptions":[{"id":"A1","kind":"scenario_assumption","statement":"</script><img src=x onerror=alert(1)>","basis":[]}],"actions":[]}`
	var out bytes.Buffer
	if err := Render(&out, b); err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, embeddedJSON(t, out.String())) {
		t.Fatal("a placeholder inside source data changed the embedded bundle")
	}
	if strings.Contains(out.String(), "</script><img") {
		t.Fatal("display details escaped their data script")
	}
	const marker = `<script id="pouch-view-details" type="application/json">`
	_, tail, ok := strings.Cut(out.String(), marker)
	if !ok {
		t.Fatal("missing display details")
	}
	raw, _, ok := strings.Cut(tail, "</script>")
	var d viewerDetails
	if !ok || json.Unmarshal([]byte(raw), &d) != nil || len(d.Assumptions) != 1 {
		t.Fatal("display details are not parseable")
	}
}
