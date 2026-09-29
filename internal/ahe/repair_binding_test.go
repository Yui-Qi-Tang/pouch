package ahe

// These helpers bind a single already recorded execution. They neither execute
// patches nor promote the model validator into a general execution verifier.
import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

const repairRun = "artifacts/go-repair-20260929-01"
const repairAuthority = "69e7489f7174cab01be065d3e515f87a2cde279681ffe58898d22f56cb0d4f78"
const repairBundle = "499e0065efdbe8ab75de9256ba83bb5ec6ab42d5a6689a5e4791055153b86d27"
const repairReport = "102ec1aed67c316a053e4d68f5db64147d25acfd5c8b544fa92fb46dba8188d1"
const repairManifest = "e28cb567e514d3dd376c138dc1bd76e89e23a7b5a75944b13ae4d2ea51d9c4ad"
const repairRequest = "django-13344-repair-go-20260929-01"
const repairSourcePrefix = "lab:h13:django-13344:"

var repairFiles = []string{"django/contrib/sessions/middleware.py", "django/middleware/cache.py", "django/middleware/security.py"}

type repairFileState struct {
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}
type repairSnapshot map[string]repairFileState
type repairEvent struct {
	Direction string         `json:"direction"`
	Action    string         `json:"action"`
	File      string         `json:"file"`
	Before    repairSnapshot `json:"before"`
	After     repairSnapshot `json:"after"`
}
type repairPath struct {
	ID            string         `json:"id"`
	PackageSHA256 string         `json:"package_sha256"`
	Initial       repairSnapshot `json:"initial"`
	Patched       repairSnapshot `json:"patched"`
	Final         repairSnapshot `json:"final"`
	Events        []repairEvent  `json:"events"`
	Restored      bool           `json:"restored"`
}
type repairExecution struct {
	Schema          string       `json:"schema_version"`
	Status          string       `json:"status"`
	AuthoritySHA256 string       `json:"authority_sha256"`
	BundleSHA256    string       `json:"bundle_sha256"`
	RequestID       string       `json:"request_id"`
	Paths           []repairPath `json:"paths"`
}
type repairInput struct {
	Inputs    run.Inputs
	Bundle    run.Bundle
	Report    repairExecution
	ReportRaw []byte
	Packages  [][]byte
	Receipts  []validation.Receipt
}

func repairJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}
func repairRead(t *testing.T, path, hash string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "" && validation.Digest(raw) != hash {
		t.Fatalf("pinned file mismatch: %s", filepath.Base(path))
	}
	return raw
}
func repairDecode(t *testing.T, raw []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatal(err)
	}
}

func loadRepairInput(t *testing.T, project string) repairInput {
	t.Helper()
	base := filepath.Join(project, repairRun)
	manifest := repairRead(t, filepath.Join(base, "manifest.json"), repairManifest)
	var m struct {
		Files []struct {
			ID     string `json:"id"`
			SHA256 string `json:"sha256"`
			Bytes  int    `json:"bytes"`
		} `json:"files"`
	}
	repairDecode(t, manifest, &m)
	if len(m.Files) != 205 {
		t.Fatal("wrong frozen inventory")
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if !filepath.IsLocal(f.ID) || seen[f.ID] {
			t.Fatal("invalid inventory entry")
		}
		seen[f.ID] = true
		if len(repairRead(t, filepath.Join(base, f.ID), f.SHA256)) != f.Bytes {
			t.Fatal("frozen byte length mismatch")
		}
	}
	in, err := run.ReadInputs(filepath.Join(base, "model/authority.json"), repairAuthority, repairRequest, filepath.Join(base, "model/sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := repairInput{Inputs: in, ReportRaw: repairRead(t, filepath.Join(base, "replay/result.json"), repairReport)}
	repairDecode(t, repairRead(t, filepath.Join(base, "model/bundle.json"), repairBundle), &out.Bundle)
	repairDecode(t, out.ReportRaw, &out.Report)
	if !out.Bundle.Complete || out.Bundle.Status != "FOUND" || !reflect.DeepEqual(out.Bundle.Contract, in.Authority.Contract()) || out.Report.Schema != "pouch-fixed-file-replay/v1" || out.Report.Status != "PASS" || out.Report.AuthoritySHA256 != repairAuthority || out.Report.BundleSHA256 != repairBundle || out.Report.RequestID != repairRequest || len(out.Report.Paths) != 6 || len(out.Bundle.Paths) != 6 {
		t.Fatal("fixed recorded run binding mismatch")
	}
	var history struct{ Initial, Patched, Final repairSnapshot }
	repairDecode(t, in.Content[repairSourcePrefix+"materializer"], &history)
	if len(history.Initial) != 3 || len(history.Patched) != 3 || !maps.Equal(history.Initial, history.Final) {
		t.Fatal("historical checkpoint mismatch")
	}
	for n, file := range repairFiles {
		if history.Initial[file].SHA256 != validation.Digest(in.Content[fmt.Sprintf("%ssource:%d", repairSourcePrefix, n)]) || history.Initial[file].Mode != 0644 || history.Patched[file].Mode != 0644 || history.Initial[file].SHA256 == history.Patched[file].SHA256 {
			t.Fatal("source/checkpoint mismatch")
		}
	}
	traces := []validation.Trace{}
	for n, p := range out.Bundle.Paths {
		if !filepath.IsLocal(p.PackageFile) {
			t.Fatal("invalid package path")
		}
		raw := repairRead(t, filepath.Join(base, "model", p.PackageFile), out.Report.Paths[n].PackageSHA256)
		receipt, err := in.Authority.Validate(t.Context(), repairRequest, raw)
		if err != nil {
			t.Fatal(err)
		}
		var pkg validation.Package
		repairDecode(t, raw, &pkg)
		if receipt.ReturnKind != "return" || receipt.ForwardCost != 3 || receipt.ReturnCost != 3 || !reflect.DeepEqual(p.Receipt, &receipt) || out.Report.Paths[n].ID != p.ID {
			t.Fatal("model/record receipt mismatch")
		}
		if err := checkRepairPath(out.Report.Paths[n], pkg, history.Initial, history.Patched); err != nil {
			t.Fatal(err)
		}
		out.Packages = append(out.Packages, raw)
		out.Receipts = append(out.Receipts, receipt)
		traces = append(traces, pkg.Forward)
	}
	set, err := in.Authority.CheckSet(t.Context(), traces, 100, 10000)
	if err != nil || !set.Complete || !set.Matches {
		t.Fatal("recorded six-path set mismatch: ", err)
	}
	return out
}

func checkRepairPath(p repairPath, pkg validation.Package, initial, patched repairSnapshot) error {
	if !p.Restored || len(p.Events) != 6 || pkg.Return == nil || !maps.Equal(p.Initial, initial) || !maps.Equal(p.Patched, patched) || !maps.Equal(p.Final, initial) {
		return errors.New("execution checkpoint or event count mismatch")
	}
	current := maps.Clone(initial)
	for step, e := range p.Events {
		trace, idx, direction := pkg.Forward, step, "forward"
		if step >= 3 {
			trace, idx, direction = *pkg.Return, step-3, "return"
		}
		if len(trace.Actions) != 3 || len(trace.States) != 4 || e.Action != trace.Actions[idx] || e.Direction != direction || !maps.Equal(e.Before, current) {
			return errors.New("execution action/order/before mismatch")
		}
		target := -1
		for n := range repairFiles {
			action := fmt.Sprintf("apply_%d", n)
			if step >= 3 {
				action = fmt.Sprintf("reverse_%d", n)
			}
			if e.Action == action {
				target = n
			}
		}
		if target < 0 || e.File != repairFiles[target] {
			return errors.New("execution target mismatch")
		}
		expected := maps.Clone(current)
		expected[e.File] = patched[e.File]
		if step >= 3 {
			expected[e.File] = initial[e.File]
		}
		if !maps.Equal(expected, e.After) {
			return errors.New("execution effect/frame/hash/mode mismatch")
		}
		for n, file := range repairFiles {
			before, after := initial[file], initial[file]
			if trace.States[idx][fmt.Sprintf("edit_%d", n)] == "patched" {
				before = patched[file]
			}
			if trace.States[idx+1][fmt.Sprintf("edit_%d", n)] == "patched" {
				after = patched[file]
			}
			if e.Before[file] != before || e.After[file] != after {
				return errors.New("execution/model state mismatch")
			}
		}
		current = expected
	}
	if !maps.Equal(current, initial) {
		return errors.New("incomplete recorded restoration")
	}
	return nil
}

// A lossless source envelope avoids interpreting JSON layout as hundreds of
// independent source citations. It is explicitly distinct from its raw bytes.
type repairEnvelope struct {
	Schema    string   `json:"schema_version"`
	OriginID  string   `json:"origin_id"`
	RawSHA256 string   `json:"raw_sha256"`
	RawBytes  int      `json:"raw_bytes"`
	Encoding  string   `json:"encoding"`
	Chunks    []string `json:"chunks"`
}

func encodeRepairArtifact(id string, raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(raw); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	env := repairEnvelope{Schema: "pouch-lossless-artifact/v1", OriginID: id, RawSHA256: validation.Digest(raw), RawBytes: len(raw), Encoding: "gzip+base64", Chunks: []string{}}
	for len(encoded) > 0 {
		n := min(4096, len(encoded))
		env.Chunks = append(env.Chunks, encoded[:n])
		encoded = encoded[n:]
	}
	return json.MarshalIndent(env, "", "  ")
}
func decodeRepairArtifact(raw []byte, id, digest string) ([]byte, error) {
	var env repairEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Schema != "pouch-lossless-artifact/v1" || env.Encoding != "gzip+base64" || env.OriginID != id || env.RawSHA256 != digest || env.RawBytes < 1 || env.RawBytes > validation.MaxBytes {
		return nil, errors.New("artifact envelope binding mismatch")
	}
	encoded, err := base64.StdEncoding.DecodeString(strings.Join(env.Chunks, ""))
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, validation.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) != env.RawBytes || validation.Digest(decoded) != digest {
		return nil, errors.New("decoded artifact byte binding mismatch")
	}
	return decoded, nil
}

func TestRepairPublicationBindings(t *testing.T) {
	project, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	in := loadRepairInput(t, project)
	for id, raw := range in.Inputs.Content {
		enc, err := encodeRepairArtifact(id, raw)
		if err != nil {
			t.Fatal(err)
		}
		dec, err := decodeRepairArtifact(enc, id, validation.Digest(raw))
		if err != nil || !bytes.Equal(dec, raw) {
			t.Fatal("lossless source binding: ", err)
		}
		if _, err := decodeRepairArtifact(enc, "wrong-source", validation.Digest(raw)); err == nil {
			t.Fatal("wrong source accepted")
		}
		if _, err := decodeRepairArtifact(enc, id, strings.Repeat("0", 64)); err == nil {
			t.Fatal("wrong digest accepted")
		}
	}
	mutations := map[string]func(*repairPath){
		"changed_target_hash": func(p *repairPath) {
			s := p.Events[0].After[p.Events[0].File]
			s.SHA256 = strings.Repeat("0", 64)
			p.Events[0].After[p.Events[0].File] = s
		},
		"changed_mode": func(p *repairPath) {
			s := p.Events[0].After[p.Events[0].File]
			s.Mode = 0600
			p.Events[0].After[p.Events[0].File] = s
		},
		"changed_frame": func(p *repairPath) {
			s := p.Events[0].After[repairFiles[1]]
			s.Mode = 0600
			p.Events[0].After[repairFiles[1]] = s
		},
		"wrong_action":   func(p *repairPath) { p.Events[0].Action = "apply_2" },
		"missing_return": func(p *repairPath) { p.Events = p.Events[:3] },
	}
	var pkg validation.Package
	repairDecode(t, in.Packages[0], &pkg)
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var p repairPath
			repairDecode(t, repairJSON(t, in.Report.Paths[0]), &p)
			mutate(&p)
			if err := checkRepairPath(p, pkg, in.Report.Paths[0].Initial, in.Report.Paths[0].Patched); err == nil {
				t.Fatal("wrong execution binding accepted")
			}
		})
	}
	// The pinned report/package digests are checked independently, before the
	// semantic event checks above; this is record checking, not new observation.
	_, err = in.Inputs.Authority.Validate(context.Background(), "wrong-request", in.Packages[0])
	if err == nil {
		t.Fatal("wrong request accepted")
	}
}
