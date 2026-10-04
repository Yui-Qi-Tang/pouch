package candidate

import (
	"context"
	"errors"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// RuntimeEvidence is an externally produced observation record. The caller pins
// its exact bytes and retains the runner/parser provenance and original logs.
// Pouch checks consistency and binding; it does not rerun or authenticate logs.
type RuntimeEvidence struct {
	SchemaVersion         string            `json:"schema_version"`
	MaterializationSHA256 string            `json:"materialization_sha256"`
	PackageSHA256         string            `json:"package_sha256"`
	RunnerSHA256          string            `json:"runner_sha256"`
	ParserSHA256          string            `json:"parser_sha256"`
	ExpectedTests         []string          `json:"expected_tests"`
	PatchFile             string            `json:"patch_file"`
	Artifacts             map[string]string `json:"artifacts"`
	Baseline              Observation       `json:"baseline"`
	Patched               Observation       `json:"patched"`
	Restored              Observation       `json:"restored"`
}
type FileState struct {
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}
type Observation struct {
	Tests    map[string]string    `json:"tests"`
	Files    map[string]FileState `json:"files"`
	LogFiles []string             `json:"log_files"`
}
type RuntimeBinding struct {
	SchemaVersion          string `json:"schema_version"`
	Status                 string `json:"status"`
	SpaceSHA256            string `json:"space_sha256"`
	AuthoritySHA256        string `json:"authority_sha256"`
	PackageSHA256          string `json:"package_sha256"`
	MaterializationSHA256  string `json:"materialization_sha256"`
	EvidenceSHA256         string `json:"evidence_sha256"`
	PatchSHA256            string `json:"patch_sha256"`
	TestCount              int    `json:"test_count"`
	RestoredFileCount      int    `json:"restored_file_count"`
	BaselineDiscriminating bool   `json:"baseline_discriminating"`
	Verification           string `json:"verification"`
}

// BindRuntime returns a portable statement for external review. A successful
// binding is neither a fresh execution nor AHE approval/admission.
func (p *Prepared) BindRuntime(ctx context.Context, pkg, materialRaw, evidenceRaw []byte, expectedEvidence, evidenceRoot string) (RuntimeBinding, error) {
	var result RuntimeBinding
	if validation.Digest(evidenceRaw) != expectedEvidence {
		return result, errors.New("runtime evidence digest mismatch")
	}
	receipt, choices, err := p.Validate(ctx, pkg)
	if err != nil {
		return result, err
	}
	var m Materialization
	var e RuntimeEvidence
	if err = decode(materialRaw, &m); err != nil {
		return result, err
	}
	if err = decode(evidenceRaw, &e); err != nil {
		return result, err
	}
	if m.SchemaVersion != "pouch-materialization/v1" || m.SpaceSHA256 != p.digest || m.PackageSHA256 != receipt.PackageSHA256 || m.AuthoritySHA256 != receipt.AuthoritySHA256 || !maps.Equal(m.Choices, choices) || !reflect.DeepEqual(m.Validation, receipt) || m.RuntimeStatus != "NOT_RUN" || len(m.Files) != len(p.space.Files) {
		return result, errors.New("materialization binding mismatch")
	}
	if e.SchemaVersion != "pouch-runtime-evidence/v1" || e.MaterializationSHA256 != validation.Digest(materialRaw) || e.PackageSHA256 != receipt.PackageSHA256 || !validHash(e.RunnerSHA256) || !validHash(e.ParserSHA256) || len(e.ExpectedTests) == 0 || len(e.ExpectedTests) > 10000 || len(e.Artifacts) == 0 || len(e.Artifacts) > 256 || e.Artifacts[e.PatchFile] != m.PatchSHA256 {
		return result, errors.New("runtime record binding mismatch")
	}
	if !slices.Contains(slices.Collect(maps.Values(e.Artifacts)), e.RunnerSHA256) || !slices.Contains(slices.Collect(maps.Values(e.Artifacts)), e.ParserSHA256) {
		return result, errors.New("runner and parser artifacts required")
	}
	root, err := os.OpenRoot(evidenceRoot)
	if err != nil {
		return result, err
	}
	defer root.Close()
	for path, hash := range e.Artifacts {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !safePath(path) || !validHash(hash) {
			return result, errors.New("invalid runtime artifact")
		}
		b, _, err := readFile(root, path)
		if err != nil {
			return result, err
		}
		if validation.Digest(b) != hash {
			return result, errors.New("runtime artifact digest mismatch")
		}
	}
	for _, o := range []Observation{e.Baseline, e.Patched, e.Restored} {
		if len(o.LogFiles) == 0 || len(o.Files) == 0 || len(o.Tests) == 0 {
			return result, errors.New("incomplete observation")
		}
		for _, f := range o.LogFiles {
			if _, ok := e.Artifacts[f]; !ok {
				return result, errors.New("observation log is not bound")
			}
		}
		for path, state := range o.Files {
			if !safePath(path) || !validHash(state.SHA256) || state.Mode > 0777 {
				return result, errors.New("invalid observed file")
			}
		}
	}
	if !maps.Equal(e.Baseline.Files, e.Restored.Files) || !maps.Equal(e.Baseline.Tests, e.Restored.Tests) {
		return result, errors.New("baseline was not restored in the recorded scope")
	}
	seen := map[string]bool{}
	discriminating := false
	for _, id := range e.ExpectedTests {
		if id == "" || seen[id] || e.Patched.Tests[id] != "PASSED" {
			return result, errors.New("required test missing, duplicated or not passing")
		}
		seen[id] = true
		switch e.Baseline.Tests[id] {
		case "PASSED":
		case "FAILED", "ERROR":
			discriminating = true
		default:
			return result, errors.New("missing baseline test")
		}
	}
	for i, f := range m.Files {
		declared := p.space.Files[i]
		if f.Path != declared.Path || f.BeforeSHA256 != declared.SHA256 || !validHash(f.AfterSHA256) {
			return result, errors.New("file identity mismatch")
		}
		after := declared.Template
		for _, g := range p.space.Groups {
			for _, o := range g.Options {
				if choices[g.ID] == o.ID {
					after = strings.ReplaceAll(after, "{{pouch:"+g.ID+"}}", o.Text[f.Path])
				}
			}
		}
		if validation.Digest([]byte(after)) != f.AfterSHA256 || e.Baseline.Files[f.Path] != (FileState{f.BeforeSHA256, f.Mode}) || e.Patched.Files[f.Path] != (FileState{f.AfterSHA256, f.Mode}) {
			return result, errors.New("observed file does not match materialized candidate")
		}
	}
	if !slices.Equal(sortedKeys(e.Baseline.Files), sortedKeys(e.Patched.Files)) {
		return result, errors.New("file observation scope changed")
	}
	return RuntimeBinding{SchemaVersion: "pouch-runtime-binding/v1", Status: "RECORDED_CHECKS_PASS", SpaceSHA256: p.digest, AuthoritySHA256: receipt.AuthoritySHA256, PackageSHA256: receipt.PackageSHA256, MaterializationSHA256: validation.Digest(materialRaw), EvidenceSHA256: expectedEvidence, PatchSHA256: m.PatchSHA256, TestCount: len(seen), RestoredFileCount: len(e.Restored.Files), BaselineDiscriminating: discriminating, Verification: "caller-pinned recorded observations and artifact hashes; no fresh execution, source entailment, log authentication or native admission"}, nil
}
func sortedKeys(m map[string]FileState) []string { return slices.Sorted(maps.Keys(m)) }
