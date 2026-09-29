// Package presentation renders the same result bundle consumed by AI clients.
package presentation

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"

	"github.com/Yui-Qi-Tang/pouch/internal/run"
)

//go:embed viewer.html
var viewer string

// Render writes a self-contained offline viewer. The embedded JSON is the exact
// marshaled bundle, not a separately computed interpretation of its results.
func Render(w io.Writer, bundle run.Bundle) error {
	for _, source := range bundle.Sources.Sources {
		if !localArtifact(source.File) {
			return fmt.Errorf("source %q has a non-portable artifact path", source.ID)
		}
	}
	for _, p := range bundle.Paths {
		if (p.PackageFile != "" && !localArtifact(p.PackageFile)) || (p.ClosureFile != "" && !localArtifact(p.ClosureFile)) {
			return errors.New("path result has a non-portable artifact path")
		}
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encoding presentation bundle: %w", err)
	}
	// encoding/json escapes HTML delimiters, including </script>, inside strings.
	// Keep that escaping; raw strings must never be inserted into executable JS.
	details, err := json.Marshal(describeBundle(bundle))
	if err != nil {
		return fmt.Errorf("encoding display details: %w", err)
	}
	document := strings.NewReplacer(
		"__POUCH_BUNDLE_JSON__", string(raw),
		"__POUCH_VIEW_DETAILS_JSON__", string(details),
	).Replace(viewer)
	if _, err := io.WriteString(w, document); err != nil {
		return fmt.Errorf("writing presentation: %w", err)
	}
	return nil
}

func localArtifact(name string) bool {
	if name == "" || path.IsAbs(name) || strings.ContainsAny(name, "\\:") || path.Clean(name) != name || name == "." {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return !strings.ContainsFunc(name, unicode.IsControl)
}
