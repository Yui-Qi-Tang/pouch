package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// PathResult binds a separately checked recovery conclusion to a forward path.
type PathResult struct {
	ID              string `json:"id"`
	ForwardVerified bool   `json:"forward_verified"`
	// RETURN_VERIFIED, NO_RETURN_IN_MODEL, or UNKNOWN; require Receipt for
	// acceptance.
	RecoveryStatus string              `json:"recovery_status"`
	Return         *validation.Trace   `json:"return"`
	PackageFile    string              `json:"package_file,omitempty"`
	Receipt        *validation.Receipt `json:"receipt"`
	ClosureFile    string              `json:"closure_file,omitempty"`
	// Unknown-recovery explanation; see README.md.
	Reason string `json:"reason,omitempty"`
}

// Bundle is shared by JSON consumers and the offline HTML renderer.
// Its status is a certification outcome, distinct from Search.Status. Consume
// it with the returned error; rendering does not authenticate these fields.
// See the package-local README.md for values and partial-result semantics.
type Bundle struct {
	SchemaVersion   string `json:"schema_version"`
	ProjectVersion  string `json:"project_version"`
	RequestID       string `json:"request_id"`
	AuthoritySHA256 string `json:"authority_sha256"`
	// FOUND, NO_PATH_WITHIN_BOUND, NO_PATH_IN_MODEL, INCONCLUSIVE, or
	// REJECTED.
	Status string `json:"status"`
	// Search, set comparison and per-path recovery certification finished.
	Complete bool `json:"complete"`
	// model_conditional_conclusion.
	ConclusionKind   string              `json:"conclusion_kind"`
	ObservedFact     bool                `json:"observed_fact"`
	NativeAHEReceipt bool                `json:"native_ahe_receipt"`
	Contract         validation.Contract `json:"contract"`
	Sources          SourceManifest      `json:"sources"`
	EvidenceMatrix   *projection.Matrix  `json:"evidence_matrix,omitempty"`
	// Descriptive provenance, not a status enum.
	EvidenceScope string              `json:"evidence_scope"`
	Search        planning.Result     `json:"search"`
	SetCheck      validation.SetCheck `json:"set_check"`
	Paths         []PathResult        `json:"paths"`
	Diagnostics   []string            `json:"diagnostics"`
}

// Certify independently checks the solver's traces and set against original rules.
// OutputDir must already be a fresh application-owned directory.
func Certify(ctx context.Context, in Inputs, result planning.Result, outputDir string) (Bundle, error) {
	c := in.Authority.Contract()
	b := Bundle{SchemaVersion: "pouch-bundle/v1", ProjectVersion: "0.1.0-dev", RequestID: c.RequestID, AuthoritySHA256: validation.Digest(in.AuthorityRaw), Status: "INCONCLUSIVE", ConclusionKind: "model_conditional_conclusion", Contract: c, Sources: in.Sources, Search: result, Paths: []PathResult{}, Diagnostics: []string{}, EvidenceScope: "selected source records; graph topology not supplied"}
	traces := make([]validation.Trace, len(result.Forward))
	for i, p := range result.Forward {
		traces[i] = p.Trace
	}
	check, setErr := in.Authority.CheckSet(ctx, traces, 10000, 1000000)
	b.SetCheck = check
	if setErr != nil {
		b.Diagnostics = append(b.Diagnostics, "REFERENCE_SET_INCOMPLETE")
	}
	if result.ForwardComplete && check.Complete && !check.Matches {
		b.Status = "REJECTED"
		b.Diagnostics = append(b.Diagnostics, "PATH_SET_MISMATCH")
		return b, errors.New("search path set mismatch")
	}
	if check.Duplicates > 0 || check.Extra > 0 {
		b.Status = "REJECTED"
		return b, errors.New("duplicate or extra forward path")
	}
	complete := result.Complete && check.Complete && check.Matches && setErr == nil
	cache := map[string]validation.Reachability{}
	forwardReach, err := in.Authority.Reachable(ctx, c.Start, c.Goal)
	if err != nil {
		b.Diagnostics = append(b.Diagnostics, "REFERENCE_REACHABILITY_INCOMPLETE")
		return b, err
	}
	for _, path := range result.Forward {
		pr := PathResult{ID: path.ID, RecoveryStatus: "UNKNOWN"}
		end, err := in.Authority.ReplayForward(ctx, path.Trace)
		if err != nil {
			b.Status = "REJECTED"
			return b, err
		}
		if path.Trace.ClaimShortest && forwardReach.Shortest != len(path.Trace.Actions) {
			b.Status = "REJECTED"
			return b, errors.New("false shortest forward claim")
		}
		pr.ForwardVerified = true
		keyBytes, _ := json.Marshal(end)
		key := string(keyBytes)
		closure, ok := cache[key]
		if !ok {
			closure, err = in.Authority.Reachable(ctx, end, c.Baseline)
			if err != nil {
				return b, err
			}
			cache[key] = closure
		}
		pkg := validation.Package{SchemaVersion: validation.Version, RequestID: c.RequestID, AuthoritySHA256: b.AuthoritySHA256, SourceBinding: maps.Clone(c.SourceBinding), Assumptions: maps.Clone(c.Assumptions), Forward: path.Trace, ClosedStates: []validation.State{}}
		if path.ReturnIndex >= 0 && path.ReturnIndex < len(result.Returns) {
			ret := result.Returns[path.ReturnIndex]
			if !maps.Equal(ret.Endpoint, end) {
				b.Status = "REJECTED"
				return b, errors.New("return endpoint mismatch")
			}
			switch ret.Status {
			case "RETURN_FOUND":
				if ret.Trace == nil {
					b.Status = "REJECTED"
					return b, errors.New("return witness missing")
				}
				pkg.ReturnKind = "return"
				pkg.Return = ret.Trace
				pr.Return = ret.Trace
				pr.RecoveryStatus = "RETURN_VERIFIED"
			case "NO_RETURN_WITHIN_BOUND":
				if !ret.CompleteWithinBound {
					b.Status = "REJECTED"
					return b, errors.New("bounded return exhaustion missing")
				}
				if closure.Witness == nil {
					pkg.ReturnKind = "no_return"
					pkg.ClosedStates = closure.States
					pr.RecoveryStatus = "NO_RETURN_IN_MODEL"
					pr.ClosureFile = "closures/" + path.ID + ".json"
					if err := writeJSON(outputDir, pr.ClosureFile, closure); err != nil {
						return b, err
					}
				} else {
					pr.Reason = "RETURN_EXISTS_BEYOND_SEARCH_BOUND"
				}
			default:
				pr.Reason = "RETURN_SEARCH_INCOMPLETE"
			}
		} else {
			pr.Reason = "RETURN_SEARCH_NOT_COMPLETED"
		}
		if pkg.ReturnKind != "" {
			raw, err := json.MarshalIndent(pkg, "", "  ")
			if err != nil {
				return b, err
			}
			raw = append(raw, '\n')
			receipt, err := in.Authority.Validate(ctx, c.RequestID, raw)
			if err != nil {
				b.Status = "REJECTED"
				return b, err
			}
			pr.PackageFile = "packages/" + path.ID + ".json"
			if err = writeBytes(outputDir, pr.PackageFile, raw); err != nil {
				return b, err
			}
			pr.Receipt = &receipt
		} else {
			complete = false
		}
		b.Paths = append(b.Paths, pr)
	}
	b.Complete = complete
	if len(result.Forward) > 0 {
		b.Status = "FOUND"
	} else if result.ForwardComplete && check.Complete && check.Matches {
		if forwardReach.Witness == nil {
			b.Status = "NO_PATH_IN_MODEL"
			if err := writeJSON(outputDir, "forward-closure.json", forwardReach); err != nil {
				return b, err
			}
		} else {
			b.Status = "NO_PATH_WITHIN_BOUND"
		}
	}
	if !complete {
		b.Diagnostics = append(b.Diagnostics, "RUN_NOT_FULLY_VERIFIED")
	}
	return b, nil
}

func writeJSON(root, name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeBytes(root, name, append(raw, '\n'))
}
func writeBytes(root, name string, raw []byte) error {
	if !filepath.IsLocal(name) {
		return errors.New("non-local artifact name")
	}
	target := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// SaveInputs freezes exact authority/source bytes with portable file identifiers.
func SaveInputs(in Inputs, dir string) (Inputs, error) {
	if err := writeBytes(dir, "authority.json", in.AuthorityRaw); err != nil {
		return in, err
	}
	for i := range in.Sources.Sources {
		s := &in.Sources.Sources[i]
		s.File = fmt.Sprintf("sources/s%03d.txt", i)
		if err := writeBytes(dir, s.File, in.Content[s.ID]); err != nil {
			return in, err
		}
	}
	if err := writeJSON(dir, "sources.json", in.Sources); err != nil {
		return in, err
	}
	return in, nil
}

// Seal inventories exact final bundle bytes, including solver evidence and HTML.
func Seal(dir string) error {
	type file struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	}
	entries := []file{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("artifact symlink")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "manifest.json" {
			return errors.New("manifest already exists")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, file{filepath.ToSlash(rel), validation.Digest(raw), int64(len(raw))})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return writeJSON(dir, "manifest.json", struct {
		Version string `json:"schema_version"`
		Files   []file `json:"files"`
	}{"pouch-manifest/v1", entries})
}
