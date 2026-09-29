package main

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func fixture(t *testing.T) (run.Inputs, validation.Package, historicalFiles) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "django-13344-repair")
	raw, err := os.ReadFile(filepath.Join(dir, "authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c validation.Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	in, err := run.ReadInputs(filepath.Join(dir, "authority.json"), validation.Digest(raw), c.RequestID, filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	trace := func(name string) validation.Trace {
		b, err := os.ReadFile(filepath.Join(dir, "history", name+"-trace.json"))
		if err != nil {
			t.Fatal(err)
		}
		var old struct {
			States  []validation.State `json:"states"`
			Actions []string           `json:"actions"`
			Cost    int                `json:"cost"`
		}
		if err := json.Unmarshal(b, &old); err != nil {
			t.Fatal(err)
		}
		if old.Cost != len(old.Actions) {
			t.Fatal("historical cost does not match unit model")
		}
		return validation.Trace{States: old.States, Actions: old.Actions, ClaimShortest: true}
	}
	ret := trace("return")
	pkg := validation.Package{SchemaVersion: validation.Version, RequestID: c.RequestID, AuthoritySHA256: validation.Digest(raw), SourceBinding: c.SourceBinding, Assumptions: c.Assumptions, Forward: trace("forward"), ReturnKind: "return", Return: &ret, ClosedStates: []validation.State{}}
	var history historicalFiles
	if err := json.Unmarshal(in.Content[sourcePrefix+"materializer"], &history); err != nil {
		t.Fatal(err)
	}
	return in, pkg, history
}

func TestH13WitnessAndMissingUndo(t *testing.T) {
	in, pkg, _ := fixture(t)
	if err := checkModel(in); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := in.Authority.Validate(t.Context(), pkg.RequestID, raw)
	if err != nil || receipt.ForwardCost != 3 || receipt.ReturnCost != 3 {
		t.Fatalf("historical file witness: %+v %v", receipt, err)
	}
	pkg.Return.Actions = pkg.Return.Actions[:2]
	pkg.Return.States = pkg.Return.States[:3]
	raw, err = json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Authority.Validate(t.Context(), pkg.RequestID, raw); err == nil {
		t.Fatal("accepted return leaving one file patched")
	}
}

func TestPhysicalCheckpointChecksBytesAndMode(t *testing.T) {
	in, pkg, h := fixture(t)
	if err := matchesState(h.Initial, pkg.Forward.States[0], h); err != nil {
		t.Fatal(err)
	}
	if err := matchesState(h.Patched, pkg.Forward.States[len(pkg.Forward.States)-1], h); err != nil {
		t.Fatal(err)
	}
	bad := maps.Clone(h.Initial)
	bad[filePaths[1]] = h.Patched[filePaths[1]]
	if err := matchesState(bad, pkg.Forward.States[0], h); err == nil {
		t.Fatal("accepted partially reversed source bytes")
	}
	bad = maps.Clone(h.Initial)
	entry := bad[filePaths[0]]
	entry.Mode = 0600
	bad[filePaths[0]] = entry
	if err := matchesState(bad, pkg.Forward.States[0], h); err == nil {
		t.Fatal("accepted changed file mode")
	}
	for _, field := range []string{"checkpoint", "source_binding"} {
		state := maps.Clone(pkg.Forward.States[0])
		state[field] = "missing"
		if err := matchesState(h.Initial, state, h); err == nil {
			t.Fatal("accepted absent checkpoint or wrong source")
		}
	}
	for i, path := range filePaths {
		patch := in.Content[sourcePrefix+"patch:"+string(rune('0'+i))]
		if err := checkPatch(patch, path); err != nil {
			t.Fatal(err)
		}
		if err := checkPatch(patch, filePaths[(i+1)%3]); err == nil {
			t.Fatal("accepted wrong file patch")
		}
	}
}

func TestAdapterRejectsChangedModelAndAdditionalPatchTarget(t *testing.T) {
	in, _, _ := fixture(t)
	c := in.Authority.Contract()
	c.Model.Actions = c.Model.Actions[:5]
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	in.Authority, err = validation.NewAuthority(raw, validation.Digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkModel(in); err == nil {
		t.Fatal("accepted missing declared operation")
	}
	patch := append([]byte{}, in.Content[sourcePrefix+"patch:0"]...)
	patch = append(patch, []byte("\n--- a/elsewhere\n+++ b/elsewhere\n@@ -1 +1 @@\n-old\n+new\n")...)
	if err := checkPatch(patch, filePaths[0]); err == nil {
		t.Fatal("accepted extra file target")
	}
}

func TestReplayRealFilesFollowsThreeEditWitness(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	in, pkg, history := fixture(t)
	work := filepath.Join(t.TempDir(), "copies")
	result, err := replayFiles(t.Context(), in, history, pkg, work)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Restored || len(result.Events) != 6 || !maps.Equal(result.Initial, history.Initial) || !maps.Equal(result.Patched, history.Patched) || !maps.Equal(result.Final, history.Initial) {
		t.Fatalf("physical replay mismatch: %+v", result)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatal("successful replay retained disposable work files")
	}
}
