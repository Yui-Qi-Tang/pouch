package candidate

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func fixture(t *testing.T) (Space, []byte, *Prepared, []byte) {
	t.Helper()
	s := Space{SchemaVersion: Version, RequestID: "example", SourceBinding: map[string]string{"evidence": validation.Digest([]byte("rule"))}, Assumptions: map[string]string{"scope": "declared options only"}, Groups: []Group{{ID: "fix", Options: []Option{{ID: "yes", Text: map[string]string{"source.txt": "fixed\n"}}, {ID: "no", Text: map[string]string{"source.txt": "broken\n"}}}}}, Constraints: []Constraint{{ID: "requires_fix", When: validation.State{"fix": "no"}, Evidence: []string{"evidence"}}}, Files: []File{{Path: "source.txt", SHA256: validation.Digest([]byte("broken\n")), Template: "{{pouch:fix}}"}}}
	raw, _ := json.Marshal(s)
	p, err := Prepare(raw, validation.Digest(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := p.validator.Contract()
	start := maps.Clone(c.Start)
	chosen := validation.State{"fix": "yes", "phase": "1"}
	done := validation.State{"fix": "yes", "phase": "done"}
	pkg := validation.Package{SchemaVersion: validation.Version, RequestID: c.RequestID, AuthoritySHA256: validation.Digest(p.Authority()), SourceBinding: c.SourceBinding, Assumptions: c.Assumptions, Forward: validation.Trace{States: []validation.State{start, chosen, done}, Actions: []string{"choose_fix_yes", "submit_candidate"}, ClaimShortest: true}, ReturnKind: "return", Return: &validation.Trace{States: []validation.State{done, chosen, start}, Actions: []string{"clear_submission", "withdraw_fix_yes"}, ClaimShortest: true}, ClosedStates: []validation.State{}}
	pr, _ := json.Marshal(pkg)
	return s, raw, p, pr
}
func TestMaterializeApplyAndReverse(t *testing.T) {
	_, _, p, pkg := fixture(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "source.txt"), []byte("broken\n"), 0644)
	dest := filepath.Join(t.TempDir(), "output")
	m, err := p.Materialize(t.Context(), pkg, root, dest)
	if err != nil {
		t.Fatal(err)
	}
	if m.RuntimeStatus != "NOT_RUN" || m.Validation.ForwardCost != 2 || m.Validation.ReturnCost != 2 {
		t.Fatal(m)
	}
	patch, _ := os.ReadFile(filepath.Join(dest, "candidate.patch"))
	if validation.Digest(patch) != m.PatchSHA256 {
		t.Fatal("digest")
	}
	cmd := exec.Command("git", "apply", "--check", filepath.Join(dest, "candidate.patch"))
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	for _, args := range [][]string{{"apply", filepath.Join(dest, "candidate.patch")}, {"apply", "-R", filepath.Join(dest, "candidate.patch")}} {
		cmd = exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, "source.txt"))
	if string(b) != "broken\n" {
		t.Fatal("not restored")
	}
	if _, err = p.Materialize(t.Context(), pkg, root, dest); err == nil {
		t.Fatal("overwrite accepted")
	}
}
func TestCandidateRejectsWrongBasis(t *testing.T) {
	s, raw, p, pkg := fixture(t)
	if _, err := Prepare(raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unpinned space")
	}
	for _, mut := range []func(*Space){
		func(s *Space) { s.Files[0].Path = "../escape" },
		func(s *Space) { s.Constraints[0].Evidence = []string{"unknown"} },
		func(s *Space) { s.Constraints[0].When["fix"] = "maybe" },
		func(s *Space) { s.Files[0].Template = "{{pouch:unknown}}" },
		func(s *Space) { s.Groups[0].Options[1].ID = "yes" },
	} {
		var altered Space
		json.Unmarshal(raw, &altered)
		mut(&altered)
		b, _ := json.Marshal(altered)
		if _, err := Prepare(b, validation.Digest(b)); err == nil {
			t.Fatal("invalid space accepted")
		}
	}
	for _, b := range [][]byte{bytes.Replace(raw, []byte(`"files":`), []byte(`"Files":`), 1), bytes.Replace(raw, []byte(`"constraints":`), []byte(`"request_id":"duplicate","constraints":`), 1), bytes.Replace(raw, []byte(`"assumptions":{"scope":"declared options only"}`), []byte(`"assumptions":null`), 1)} {
		if _, err := Prepare(b, validation.Digest(b)); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "source.txt"), []byte("other\n"), 0644)
	if _, err := p.Materialize(t.Context(), pkg, root, filepath.Join(root, "out")); err == nil {
		t.Fatal("wrong baseline")
	}
	os.Remove(filepath.Join(root, "source.txt"))
	os.Symlink("elsewhere", filepath.Join(root, "source.txt"))
	if _, err := p.Materialize(t.Context(), pkg, root, filepath.Join(root, "out")); err == nil {
		t.Fatal("symlink baseline")
	}
	var cert validation.Package
	json.Unmarshal(pkg, &cert)
	cert.Forward.States[2]["fix"] = "no"
	b, _ := json.Marshal(cert)
	if _, _, err := p.Validate(t.Context(), b); err == nil {
		t.Fatal("valid formula for wrong effect accepted")
	}
	s.Files[0].Template = "changed {{pouch:fix}}"
	b, _ = json.Marshal(s)
	other, err := Prepare(b, validation.Digest(b))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = other.Validate(t.Context(), pkg); err == nil {
		t.Fatal("old certificate accepted for changed renderer")
	}
}
func TestDiffEmptyAndNoFinalNewline(t *testing.T) {
	for _, pair := range [][2]string{{"", "x"}, {"x", ""}, {"x", "y\n"}, {"x\n", "y"}} {
		root := t.TempDir()
		os.WriteFile(filepath.Join(root, "f"), []byte(pair[0]), 0644)
		var b strings.Builder
		writeDiff(&b, "f", pair[0], pair[1])
		patch := filepath.Join(t.TempDir(), "p")
		os.WriteFile(patch, []byte(b.String()), 0644)
		for _, args := range [][]string{{"apply", patch}, {"apply", "-R", patch}} {
			c := exec.Command("git", args...)
			c.Dir = root
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatal(string(out), err, b.String())
			}
		}
	}
}
