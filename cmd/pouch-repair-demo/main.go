// Command pouch-repair-demo replays the fixed Django three-file edits on
// disposable copies. It is an example adapter, not a general execution engine.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

const sourcePrefix = "lab:h13:django-13344:"

var filePaths = []string{
	"django/contrib/sessions/middleware.py",
	"django/middleware/cache.py",
	"django/middleware/security.py",
}

type fileState struct {
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}
type snapshot map[string]fileState

type historicalFiles struct {
	Initial snapshot `json:"initial"`
	Patched snapshot `json:"patched"`
	Final   snapshot `json:"final"`
}
type event struct {
	Direction   string   `json:"direction"`
	Action      string   `json:"action"`
	File        string   `json:"file"`
	Before      snapshot `json:"before"`
	After       snapshot `json:"after"`
	CheckOutput string   `json:"check_output"`
	ApplyOutput string   `json:"apply_output"`
}
type replay struct {
	ID            string   `json:"id"`
	PackageSHA256 string   `json:"package_sha256"`
	Initial       snapshot `json:"initial"`
	Patched       snapshot `json:"patched"`
	Final         snapshot `json:"final"`
	Events        []event  `json:"events"`
	Restored      bool     `json:"restored"`
}
type report struct {
	SchemaVersion   string   `json:"schema_version"`
	Status          string   `json:"status"`
	Started         string   `json:"started_at"`
	Finished        string   `json:"finished_at"`
	AuthoritySHA256 string   `json:"authority_sha256"`
	BundleSHA256    string   `json:"bundle_sha256"`
	RequestID       string   `json:"request_id"`
	GitVersion      string   `json:"git_version"`
	Scope           string   `json:"scope"`
	ModelHTML       string   `json:"model_html"`
	Paths           []replay `json:"paths"`
	Failure         string   `json:"failure,omitempty"`
}

func main() {
	input := flag.String("run", "", "sealed Pouch model run directory")
	authoritySHA := flag.String("authority-sha256", "", "caller-pinned authority digest")
	bundleSHA := flag.String("bundle-sha256", "", "caller-pinned bundle digest")
	requestID := flag.String("request-id", "", "caller-selected request ID")
	output := flag.String("out", "", "new output directory; no existing checkout is modified")
	flag.Parse()
	if *input == "" || *output == "" || *authoritySHA == "" || *bundleSHA == "" || *requestID == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := execute(ctx, *input, *authoritySHA, *bundleSHA, *requestID, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, input, authoritySHA, bundleSHA, requestID, output string) error {
	in, err := run.ReadInputs(filepath.Join(input, "authority.json"), authoritySHA, requestID, filepath.Join(input, "sources.json"))
	if err != nil {
		return err
	}
	if err := checkModel(in); err != nil {
		return err
	}
	var history historicalFiles
	if err := json.Unmarshal(in.Content[sourcePrefix+"materializer"], &history); err != nil {
		return err
	}
	if len(history.Initial) != 3 || len(history.Patched) != 3 || !maps.Equal(history.Initial, history.Final) {
		return errors.New("invalid historical file checkpoints")
	}
	for i, path := range filePaths {
		before, ok := history.Initial[path]
		after, patchedOK := history.Patched[path]
		if !ok || !patchedOK || before.Mode != 0644 || after.Mode != 0644 || before.SHA256 == after.SHA256 || validation.Digest(in.Content[fmt.Sprintf("%ssource:%d", sourcePrefix, i)]) != before.SHA256 {
			return errors.New("file checkpoint binding mismatch")
		}
	}
	root, err := os.OpenRoot(input)
	if err != nil {
		return err
	}
	defer root.Close()
	bundleRaw, err := readBounded(root, "bundle.json")
	if err != nil {
		return err
	}
	if validation.Digest(bundleRaw) != bundleSHA {
		return errors.New("bundle digest mismatch")
	}
	var b run.Bundle
	if err := json.Unmarshal(bundleRaw, &b); err != nil {
		return err
	}
	if !b.Complete || b.Status != "FOUND" || b.AuthoritySHA256 != authoritySHA || !reflect.DeepEqual(b.Contract, in.Authority.Contract()) {
		return errors.New("model bundle mismatch or incomplete")
	}
	packages := make([]validation.Package, len(b.Paths))
	digests := make([]string, len(b.Paths))
	traces := make([]validation.Trace, len(b.Paths))
	for i, p := range b.Paths {
		raw, err := readBounded(root, p.PackageFile)
		if err != nil {
			return err
		}
		receipt, err := in.Authority.Validate(ctx, requestID, raw)
		if err != nil {
			return fmt.Errorf("package %s: %w", p.ID, err)
		}
		if receipt.ReturnKind != "return" || receipt.ForwardCost != 3 || receipt.ReturnCost != 3 {
			return errors.New("expected three apply and three reverse operations")
		}
		if err := json.Unmarshal(raw, &packages[i]); err != nil {
			return err
		}
		if !reflect.DeepEqual(p.Receipt, &receipt) || !reflect.DeepEqual(p.Return, packages[i].Return) {
			return errors.New("display receipt/return differs from validated package")
		}
		digests[i] = validation.Digest(raw)
		traces[i] = packages[i].Forward
	}
	set, err := in.Authority.CheckSet(ctx, traces, 100, 10000)
	if err != nil {
		return err
	}
	if len(packages) != 6 || !set.Complete || !set.Matches {
		return errors.New("expected complete six-path set")
	}
	if err := os.Mkdir(output, 0755); err != nil {
		return err
	}
	modelLink, err := filepath.Rel(output, filepath.Join(input, "index.html"))
	if err != nil {
		return err
	}
	r := report{SchemaVersion: "pouch-fixed-file-replay/v1", Status: "INCONCLUSIVE", Started: time.Now().UTC().Format(time.RFC3339Nano), AuthoritySHA256: authoritySHA, BundleSHA256: bundleSHA, RequestID: requestID, ModelHTML: filepath.ToSlash(modelLink), Paths: []replay{}, Scope: "Current run: patch/reverse on fresh copies of three pinned source files, checking every step's hashes and file modes. No Django tests, network effects, database restore, live AHE admission, or general rollback guarantee. Historical SWE acceptance remains FAIL / CONTROL_REPRODUCTION_MISMATCH."}
	version, err := exec.CommandContext(ctx, "git", "--version").Output()
	if err == nil {
		r.GitVersion = strings.TrimSpace(string(version))
		for i, pkg := range packages {
			var one replay
			one, err = replayFiles(ctx, in, history, pkg, filepath.Join(output, fmt.Sprintf("work-%06d", i)))
			one.ID = b.Paths[i].ID
			one.PackageSHA256 = digests[i]
			r.Paths = append(r.Paths, one)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		r.Status = "PASS"
	} else {
		r.Failure = err.Error()
	}
	r.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	if saveErr := saveReport(output, r); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	if sealErr := run.Seal(output); sealErr != nil {
		return errors.Join(err, sealErr)
	}
	return err
}

func readBounded(root *os.Root, name string) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, errors.New("non-local package file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		return nil, err
	}
	if len(b) == 64<<20 {
		return nil, errors.New("file size limit")
	}
	return b, nil
}

// checkModel ensures this example cannot reinterpret unrelated operations as edits.
func checkModel(in run.Inputs) error {
	var original struct {
		Fields    map[string][]string `json:"fields"`
		Forbidden []validation.State  `json:"domain_forbidden"`
		Actions   []struct {
			ID     string           `json:"id"`
			Guard  validation.State `json:"guard"`
			Effect validation.State `json:"effect"`
			Frame  []string         `json:"frame"`
			Cost   int              `json:"cost"`
		} `json:"actions"`
		Baseline validation.State `json:"baseline"`
		Goal     validation.State `json:"goal"`
	}
	if err := json.Unmarshal(in.Content[sourcePrefix+"model"], &original); err != nil {
		return err
	}
	c := in.Authority.Contract()
	if len(c.SourceBinding) != 8 || len(original.Actions) != 6 || len(c.Model.Actions) != 6 || len(original.Fields) != 5 || c.ForwardLimit != 3 || c.ReturnLimit != 3 || !reflect.DeepEqual(c.Model.Fields, original.Fields) || !reflect.DeepEqual(c.Model.Forbidden, original.Forbidden) || !maps.Equal(c.Start, original.Baseline) || !maps.Equal(c.Baseline, original.Baseline) || !maps.Equal(c.Goal, original.Goal) {
		return errors.New("authority differs from fixed Django file-edit model")
	}
	for i, a := range original.Actions {
		if a.Cost != 1 || !reflect.DeepEqual(c.Model.Actions[i], validation.Action{ID: a.ID, Guard: a.Guard, Effect: a.Effect, Frame: a.Frame}) {
			return errors.New("fixed file-edit operation changed")
		}
	}
	return nil
}

func capture(dir string) (snapshot, error) {
	s := snapshot{}
	for _, path := range filePaths {
		name := filepath.Join(dir, path)
		info, err := os.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("non-regular replay file")
		}
		b, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		s[path] = fileState{SHA256: validation.Digest(b), Mode: uint32(info.Mode().Perm())}
	}
	return s, nil
}
func matchesState(s snapshot, state validation.State, history historicalFiles) error {
	if state["checkpoint"] != "present" || state["source_binding"] != "current" {
		return errors.New("replay checkpoint/source is not current")
	}
	for i, path := range filePaths {
		var want fileState
		switch state[fmt.Sprintf("edit_%d", i)] {
		case "original":
			want = history.Initial[path]
		case "patched":
			want = history.Patched[path]
		default:
			return errors.New("unknown edit state")
		}
		if s[path] != want {
			return fmt.Errorf("file bytes/mode disagree with model at %s", path)
		}
	}
	return nil
}
func replayFiles(ctx context.Context, in run.Inputs, history historicalFiles, pkg validation.Package, dir string) (replay, error) {
	r := replay{Events: []event{}}
	if err := os.Mkdir(dir, 0700); err != nil {
		return r, err
	}
	// This directory was created exclusively by this call. Inputs and user files
	// are never modified; failed runs retain the copies for diagnosis.
	for i, path := range filePaths {
		name := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return r, err
		}
		if err := os.WriteFile(name, in.Content[fmt.Sprintf("%ssource:%d", sourcePrefix, i)], 0644); err != nil {
			return r, err
		}
		if err := os.Chmod(name, 0644); err != nil {
			return r, err
		}
	}
	var err error
	r.Initial, err = capture(dir)
	if err != nil {
		return r, err
	}
	for _, leg := range []struct {
		name  string
		trace validation.Trace
	}{{"forward", pkg.Forward}, {"return", *pkg.Return}} {
		for n, id := range leg.trace.Actions {
			var i int
			reverse := false
			switch id {
			case "apply_0":
				i = 0
			case "apply_1":
				i = 1
			case "apply_2":
				i = 2
			case "reverse_0":
				i = 0
				reverse = true
			case "reverse_1":
				i = 1
				reverse = true
			case "reverse_2":
				i = 2
				reverse = true
			default:
				return r, errors.New("unsupported file action")
			}
			before, err := capture(dir)
			if err != nil {
				return r, err
			}
			if err := matchesState(before, leg.trace.States[n], history); err != nil {
				return r, err
			}
			patch := in.Content[fmt.Sprintf("%spatch:%d", sourcePrefix, i)]
			// Refuse additional file targets. The accepted Django patches edit one
			// existing regular file each, without renames or mode transitions.
			if err := checkPatch(patch, filePaths[i]); err != nil {
				return r, err
			}
			e := event{Direction: leg.name, Action: id, File: filePaths[i], Before: before}
			for _, check := range []bool{true, false} {
				args := []string{"apply", "--whitespace=nowarn"}
				if reverse {
					args = append(args, "--reverse")
				}
				if check {
					args = append(args, "--check")
				}
				args = append(args, "-")
				cmd := exec.CommandContext(ctx, "git", args...)
				cmd.Dir = dir
				cmd.Stdin = bytes.NewReader(patch)
				// Ignore inherited repository/worktree overrides.
				cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir), "LC_ALL=C"}
				out, err := cmd.CombinedOutput()
				if check {
					e.CheckOutput = string(out)
				} else {
					e.ApplyOutput = string(out)
				}
				if err != nil {
					r.Events = append(r.Events, e)
					return r, fmt.Errorf("%s %s: %w", leg.name, id, err)
				}
			}
			e.After, err = capture(dir)
			r.Events = append(r.Events, e)
			if err != nil {
				return r, err
			}
			if err := matchesState(e.After, leg.trace.States[n+1], history); err != nil {
				return r, err
			}
		}
		if leg.name == "forward" {
			r.Patched, err = capture(dir)
		} else {
			r.Final, err = capture(dir)
		}
		if err != nil {
			return r, err
		}
	}
	r.Restored = maps.Equal(r.Initial, r.Final)
	if !r.Restored {
		return r, errors.New("final file checkpoint differs from initial")
	}
	return r, os.RemoveAll(dir)
}
func checkPatch(patch []byte, path string) error {
	s := string(patch)
	if strings.Count(s, "diff --git ") != 1 || strings.Count(s, "\n--- ") != 1 || strings.Count(s, "\n+++ ") != 1 || !strings.HasPrefix(s, "diff --git a/"+path+" b/"+path+"\n--- a/"+path+"\n+++ b/"+path+"\n@@ ") {
		return errors.New("unsupported patch target or metadata")
	}
	return nil
}

func saveReport(dir string, r report) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "result.json"), append(raw, '\n'), 0644); err != nil {
		return err
	}
	t, err := template.New("report").Parse(reportHTML)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "index.html"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	err = t.Execute(f, r)
	return errors.Join(err, f.Close())
}

const reportHTML = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Django 13344 — file patch and reverse</title><style>body{font:16px system-ui;max-width:1150px;margin:40px auto;padding:0 20px;color:#203a44;background:#f7fafb}section{background:white;border:1px solid #ccdce2;border-radius:12px;padding:22px;margin:24px 0}td,th{text-align:left;padding:8px;border-bottom:1px solid #ddd}table{width:100%;font-size:14px}code{overflow-wrap:anywhere}.note{border-left:4px solid #bc8524;padding:12px;background:#fff4dd}a{color:#086f80}</style><h1>Django 13344 · three-file patch and reverse</h1><p><strong>{{.Status}}</strong> · actual file replay on disposable copies</p><p>Forward: 3 patch operations. Separate return: 3 reverse-patch operations. Six orderings of the same known repair; no new repair was invented.</p><p><a href="{{.ModelHTML}}">Open interactive Go path / matrix replay →</a> · <a href="result.json">Current execution evidence (JSON)</a> · <a href="manifest.json">File hashes</a></p><p class="note">{{.Scope}}</p><p>This page records actual file changes. The linked model viewer records model validation. The model alone does not certify physical restoration. Counts are operations, not elapsed time or general rollback cost.</p>{{if .Failure}}<pre>{{.Failure}}</pre>{{end}}{{range .Paths}}<section><h2>{{.ID}} · files restored: {{.Restored}}</h2><p><a href="{{$.ModelHTML}}#path={{.ID}}&amp;direction=forward&amp;step=0">Forward graph</a> · <a href="{{$.ModelHTML}}#path={{.ID}}&amp;direction=return&amp;step=0">Separate return graph</a></p><table><tr><th>Direction</th><th>Action</th><th>Real file</th></tr>{{range .Events}}<tr><td>{{.Direction}}</td><td><code>{{.Action}}</code></td><td><code>{{.File}}</code></td></tr>{{end}}</table><h3>Final checkpoint (matched initial bytes and mode)</h3><table><tr><th>File</th><th>SHA-256</th><th>Mode (decimal)</th></tr>{{range $name,$state:=.Final}}<tr><td><code>{{$name}}</code></td><td><code>{{$state.SHA256}}</code></td><td>{{$state.Mode}}</td></tr>{{end}}</table><p>Validated package SHA-256: <code>{{.PackageSHA256}}</code></p></section>{{end}}<p>Started {{.Started}} · Finished {{.Finished}} · {{.GitVersion}}</p><p>Authority: <code>{{.AuthoritySHA256}}</code><br>Model bundle: <code>{{.BundleSHA256}}</code></p></html>`
