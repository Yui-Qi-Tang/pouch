// Package run coordinates input binding, search, independent validation and artifacts.
package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Source identifies exact source bytes within a portable bundle.
type Source struct {
	ID     string `json:"id"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// SourceManifest binds explicit source files to a caller-selected authority.
type SourceManifest struct {
	SchemaVersion   string   `json:"schema_version"`
	AuthoritySHA256 string   `json:"authority_sha256"`
	RequestID       string   `json:"request_id"`
	Sources         []Source `json:"sources"`
}

// Inputs owns the validated authority and exact source material.
type Inputs struct {
	Authority    *validation.Authority
	AuthorityRaw []byte
	Sources      SourceManifest
	Content      map[string][]byte
}

// ReadInputs requires the caller's digest and request ID independently of the manifest.
func ReadInputs(authorityFile, expectedSHA, requestID, sourceManifest string) (Inputs, error) {
	var in Inputs
	raw, err := readFile(authorityFile, validation.MaxBytes)
	if err != nil {
		return in, err
	}
	a, err := validation.NewAuthority(raw, expectedSHA)
	if err != nil {
		return in, err
	}
	if a.Contract().RequestID != requestID {
		return in, errors.New("caller request mismatch")
	}
	mraw, err := readFile(sourceManifest, validation.MaxBytes)
	if err != nil {
		return in, err
	}
	var m SourceManifest
	if err = decodeExact(mraw, &m); err != nil {
		return in, fmt.Errorf("source manifest: %w", err)
	}
	if m.SchemaVersion != "pouch-sources/v1" || m.AuthoritySHA256 != expectedSHA || m.RequestID != requestID || len(m.Sources) != len(a.SourceIDs()) {
		return in, errors.New("source manifest binding mismatch")
	}
	root, err := os.OpenRoot(filepath.Dir(sourceManifest))
	if err != nil {
		return in, err
	}
	defer root.Close()
	contents := map[string][]byte{}
	for _, s := range m.Sources {
		expected, ok := a.SourceDigest(s.ID)
		if !ok || expected != s.SHA256 || contents[s.ID] != nil {
			return in, errors.New("source identity or digest mismatch")
		}
		if !filepath.IsLocal(s.File) || strings.Contains(s.File, "\\") {
			return in, errors.New("source file must be a confined relative path")
		}
		f, err := root.Open(s.File)
		if err != nil {
			return in, err
		}
		content, readErr := io.ReadAll(io.LimitReader(f, validation.MaxBytes+1))
		closeErr := f.Close()
		if readErr != nil {
			return in, readErr
		}
		if closeErr != nil {
			return in, closeErr
		}
		if len(content) > validation.MaxBytes || validation.Digest(content) != expected {
			return in, errors.New("source bytes mismatch or bound exceeded")
		}
		contents[s.ID] = content
	}
	in = Inputs{Authority: a, AuthorityRaw: raw, Sources: m, Content: contents}
	return in, nil
}

func readFile(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, errors.New("input byte limit")
	}
	return b, nil
}

// decodeExact rejects duplicate/aliased members, missing fields and coercions.
func decodeExact(raw []byte, dst any) error {
	if len(raw) > validation.MaxBytes || !utf8.Valid(raw) {
		return errors.New("json bound or utf8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 32 {
			return errors.New("json depth")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if delim != '{' && delim != '[' {
			return errors.New("json container")
		}
		seen := map[string]bool{}
		for d.More() {
			if delim == '{' {
				k, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := k.(string)
				if !ok {
					return errors.New("json key")
				}
				key := strings.ToLower(name)
				if seen[key] {
					return errors.New("duplicate json key")
				}
				seen[key] = true
			}
			if err := visit(depth + 1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("json trailing data")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	normalized, err := json.Marshal(dst)
	if err != nil {
		return err
	}
	var left, right any
	for i, b := range [][]byte{raw, normalized} {
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return err
		}
		if i == 0 {
			left = v
		} else {
			right = v
		}
	}
	if !reflect.DeepEqual(left, right) {
		return errors.New("json fields or types mismatch")
	}
	return nil
}
