// Package cli implements the local Pouch commands; main owns signals and exit.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/planning"
	"github.com/Yui-Qi-Tang/pouch/internal/presentation"
	"github.com/Yui-Qi-Tang/pouch/internal/projection"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/sat"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Run dispatches commands and writes command-specific stdout, usually JSON.
// It returns 0 for completion, 1 for solve/verify model rejection, and 2 for
// incomplete work or errors. Other handlers also map semantic errors to 2.
// The package-local README.md defines output shapes and status/exit meanings.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: pouch candidate-prepare|candidate-materialize|solve|project|verify|render|ahe-discover|ahe-read|ahe-stage|ahe-admit|version")
		return 2
	}
	var err error
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, "pouch 0.1.0-dev")
		return 0
	case "ahe-discover", "ahe-read", "ahe-stage", "ahe-admit":
		err = aheCommand(ctx, args[0], args[1:], stdout, stderr)
	case "candidate-prepare", "candidate-materialize", "candidate-bind-runtime":
		err = candidateCommand(ctx, args[0], args[1:], stdout, stderr)
	case "solve":
		return solve(ctx, args[1:], stdout, stderr)
	case "project":
		err = project(args[1:], stdout, stderr)
	case "verify":
		return verify(ctx, args[1:], stdout, stderr)
	case "render":
		err = render(args[1:], stdout, stderr)
	default:
		err = errors.New("unknown command")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}
func flags(name string, errout io.Writer) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(errout)
	return f
}
func read(path string, limit int) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if e == nil && len(b) > limit {
		e = errors.New("input byte limit")
	}
	return b, e
}
func output(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func solve(ctx context.Context, args []string, out, errout io.Writer) int {
	f := flags("solve", errout)
	authority := f.String("authority", "", "caller-owned authority JSON")
	hash := f.String("authority-sha256", "", "caller-pinned exact authority digest")
	request := f.String("request-id", "", "caller-selected request ID")
	sources := f.String("sources", "", "source manifest")
	dir := f.String("out", "", "new result directory")
	solver := f.String("solver", "", "CaDiCaL executable")
	solverHash := f.String("solver-sha256", "", "pinned solver SHA-256")
	checker := f.String("checker", "", "DRAT-trim executable")
	checkerHash := f.String("checker-sha256", "", "pinned checker SHA-256")
	graph := f.String("graph", "", "optional source-bound canonical graph")
	graphHash := f.String("graph-sha256", "", "caller-pinned graph digest")
	timeout := f.Duration("timeout", 2*time.Minute, "whole search and verification budget")
	queryTimeout := f.Duration("query-timeout", 10*time.Second, "individual tool budget")
	maxPaths := f.Int("max-paths", 1000, "forward path resource limit")
	maxQueries := f.Int("max-queries", 10000, "solver call resource limit")
	if e := f.Parse(args); e != nil {
		return 2
	}
	if f.NArg() != 0 || *authority == "" || *hash == "" || *request == "" || *sources == "" || *dir == "" || *timeout <= 0 || *queryTimeout <= 0 || *maxPaths <= 0 || *maxQueries <= 0 {
		fmt.Fprintln(errout, "required inputs or limits missing")
		return 2
	}
	in, e := run.ReadInputs(*authority, *hash, *request, *sources)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	var graphRaw []byte
	if (*graph == "") != (*graphHash == "") {
		fmt.Fprintln(errout, "graph and graph-sha256 required together")
		return 2
	}
	if *graph != "" {
		graphRaw, e = read(*graph, projection.MaxBytes)
		if e != nil {
			fmt.Fprintln(errout, e)
			return 2
		}
	}
	if e = os.Mkdir(*dir, 0755); e != nil {
		fmt.Fprintln(errout, "output directory must be new:", e)
		return 2
	}
	in, e = run.SaveInputs(in, *dir)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	evidence := run.Bundle{RequestID: *request}
	if e = run.AttachEvidence(&evidence, in, graphRaw, *graphHash, *dir); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	runner, e := sat.New(sat.Config{SolverPath: *solver, SolverSHA256: *solverHash, CheckerPath: *checker, CheckerSHA256: *checkerHash, ArtifactDir: filepath.Join(*dir, "solver"), Timeout: *queryTimeout})
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result, searchErr := planning.Search(ctx, in.Authority.Contract(), runner, planning.Options{MaxPaths: *maxPaths, MaxQueries: *maxQueries})
	bundle, certErr := run.Certify(ctx, in, result, *dir)
	bundle.EvidenceMatrix = evidence.EvidenceMatrix
	bundle.EvidenceScope = evidence.EvidenceScope
	if e = run.SaveBundle(*dir, bundle); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	html, e := os.OpenFile(filepath.Join(*dir, "index.html"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	e = presentation.Render(html, bundle)
	closeErr := html.Close()
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	if closeErr != nil {
		fmt.Fprintln(errout, closeErr)
		return 2
	}
	if e = run.Seal(*dir); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	summary := struct {
		Status   string `json:"status"`
		Complete bool   `json:"complete"`
		Paths    int    `json:"paths"`
		Queries  int    `json:"queries"`
		Bundle   string `json:"bundle"`
	}{bundle.Status, bundle.Complete, len(bundle.Paths), len(result.Queries), "bundle.json"}
	if e = output(out, summary); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	if certErr != nil {
		fmt.Fprintln(errout, certErr)
		if bundle.Status == "REJECTED" {
			return 1
		}
		return 2
	}
	if searchErr != nil {
		fmt.Fprintln(errout, searchErr)
		return 2
	}
	if !bundle.Complete {
		return 2
	}
	return 0
}

func project(args []string, out, errout io.Writer) error {
	f := flags("project", errout)
	file := f.String("graph", "", "canonical evidence graph JSON")
	hash := f.String("sha256", "", "exact graph digest")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	b, e := read(*file, projection.MaxBytes)
	if e != nil {
		return e
	}
	s, e := projection.Decode(b, *hash)
	if e != nil {
		return e
	}
	m, e := projection.Project(s)
	if e != nil {
		return e
	}
	return output(out, m)
}
func verify(ctx context.Context, args []string, out, errout io.Writer) int {
	f := flags("verify", errout)
	file := f.String("authority", "", "authority JSON")
	hash := f.String("authority-sha256", "", "caller-pinned digest")
	id := f.String("request-id", "", "caller request")
	pkg := f.String("package", "", "path certificate JSON")
	if e := f.Parse(args); e != nil {
		return 2
	}
	if f.NArg() != 0 {
		return 2
	}
	raw, e := read(*file, validation.MaxBytes)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	a, e := validation.NewAuthority(raw, *hash)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	raw, e = read(*pkg, validation.MaxBytes)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	receipt, e := a.Validate(ctx, *id, raw)
	if e != nil {
		var rejection *validation.Rejection
		if errors.As(e, &rejection) {
			if er := output(out, map[string]string{"status": "REJECTED", "reason": rejection.Code}); er != nil {
				fmt.Fprintln(errout, er)
				return 2
			}
			return 1
		}
		fmt.Fprintln(errout, e)
		return 2
	}
	if e = output(out, receipt); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	return 0
}
func render(args []string, out, errout io.Writer) error {
	f := flags("render", errout)
	file := f.String("bundle", "", "saved bundle JSON (display only; not re-verification)")
	target := f.String("out", "", "new HTML file")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	raw, e := read(*file, 64<<20)
	if e != nil {
		return e
	}
	var b run.Bundle
	if e = json.Unmarshal(raw, &b); e != nil {
		return e
	}
	if b.SchemaVersion != "pouch-bundle/v1" {
		return errors.New("unsupported bundle version")
	}
	html, e := os.OpenFile(*target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	e = presentation.Render(html, b)
	closeErr := html.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return output(out, map[string]string{"status": "RENDERED", "verification": "saved claims only; no re-verification"})
}
