package candidate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

type FileBinding struct {
	Path         string `json:"path"`
	BeforeSHA256 string `json:"before_sha256"`
	AfterSHA256  string `json:"after_sha256"`
	Mode         uint32 `json:"mode"`
}
type Materialization struct {
	SchemaVersion   string             `json:"schema_version"`
	SpaceSHA256     string             `json:"space_sha256"`
	AuthoritySHA256 string             `json:"authority_sha256"`
	PackageSHA256   string             `json:"package_sha256"`
	PatchSHA256     string             `json:"patch_sha256"`
	Choices         validation.State   `json:"choices"`
	Files           []FileBinding      `json:"files"`
	Validation      validation.Receipt `json:"validation"`
	RuntimeStatus   string             `json:"runtime_status"`
}

func safePath(path string) bool {
	if !filepath.IsLocal(path) || filepath.ToSlash(filepath.Clean(path)) != path || strings.HasPrefix(path, ".git/") || path == ".git" {
		return false
	}
	for _, c := range path {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/_-.", c)) {
			return false
		}
	}
	return true
}
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func readFile(root *os.Root, path string) ([]byte, os.FileMode, error) {
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, 0, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, 0, errors.New("candidate baseline symlink unsupported")
		}
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("regular baseline required")
	}
	b, err := io.ReadAll(io.LimitReader(f, validation.MaxBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(b) > validation.MaxBytes || !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return nil, 0, errors.New("bounded UTF-8 text file required")
	}
	return b, info.Mode().Perm(), nil
}

// Materialize validates first, reads only pinned regular baseline files and
// writes a new output directory. It never modifies or executes the checkout.
// Selection and withdrawal are model operations, not physical restore claims.
func (p *Prepared) Materialize(ctx context.Context, pkg []byte, rootDir, outDir string) (Materialization, error) {
	var m Materialization
	receipt, choices, err := p.Validate(ctx, pkg)
	if err != nil {
		return m, err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return m, err
	}
	defer root.Close()
	m = Materialization{SchemaVersion: "pouch-materialization/v1", SpaceSHA256: p.digest, AuthoritySHA256: receipt.AuthoritySHA256, PackageSHA256: receipt.PackageSHA256, Choices: choices, Files: []FileBinding{}, Validation: receipt, RuntimeStatus: "NOT_RUN"}
	contents := map[string][]byte{}
	var patch strings.Builder
	total := 0
	for _, f := range p.space.Files {
		if err = ctx.Err(); err != nil {
			return m, err
		}
		before, mode, err := readFile(root, f.Path)
		if err != nil {
			return m, err
		}
		if validation.Digest(before) != f.SHA256 {
			return m, fmt.Errorf("baseline digest mismatch: %s", f.Path)
		}
		after := f.Template
		for _, g := range p.space.Groups {
			for _, o := range g.Options {
				if choices[g.ID] == o.ID {
					after = strings.ReplaceAll(after, "{{pouch:"+g.ID+"}}", o.Text[f.Path])
				}
			}
		}
		if strings.Contains(after, "{{pouch:") || strings.ContainsRune(after, 0) || !utf8.ValidString(after) {
			return m, errors.New("invalid materialized text")
		}
		total += len(after)
		if total > validation.MaxBytes {
			return m, errors.New("materialization byte limit")
		}
		contents[f.Path] = []byte(after)
		m.Files = append(m.Files, FileBinding{f.Path, f.SHA256, validation.Digest([]byte(after)), uint32(mode)})
		if string(before) != after {
			writeDiff(&patch, f.Path, string(before), after)
		}
	}
	m.PatchSHA256 = validation.Digest([]byte(patch.String()))
	// Output must be new. Partial writes remain diagnostic artifacts on I/O failure.
	if err = os.Mkdir(outDir, 0755); err != nil {
		return m, err
	}
	for _, f := range m.Files {
		target := filepath.Join(outDir, "files", f.Path)
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return m, err
		}
		if err = os.WriteFile(target, contents[f.Path], os.FileMode(f.Mode)); err != nil {
			return m, err
		}
	}
	if err = os.WriteFile(filepath.Join(outDir, "candidate.patch"), []byte(patch.String()), 0644); err != nil {
		return m, err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	err = os.WriteFile(filepath.Join(outDir, "materialization.json"), append(raw, '\n'), 0644)
	return m, err
}
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
func writeDiff(w io.Writer, path, before, after string) {
	// Full-file hunks keep this renderer deterministic without an external diff tool.
	fmt.Fprintf(w, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", path, path, path, path)
	a, b := lineCount(before), lineCount(after)
	as, bs := 1, 1
	if a == 0 {
		as = 0
	}
	if b == 0 {
		bs = 0
	}
	fmt.Fprintf(w, "@@ -%d,%d +%d,%d @@\n", as, a, bs, b)
	lines := func(text, prefix string) {
		if text == "" {
			return
		}
		for _, s := range strings.SplitAfter(text, "\n") {
			if s == "" {
				continue
			}
			fmt.Fprint(w, prefix, s)
			if !strings.HasSuffix(s, "\n") {
				fmt.Fprint(w, "\n\\ No newline at end of file\n")
			}
		}
	}
	lines(before, "-")
	lines(after, "+")
}
