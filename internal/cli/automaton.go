package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/automata"
	"github.com/Yui-Qi-Tang/pouch/internal/run"
	"github.com/Yui-Qi-Tang/pouch/internal/sat"
	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

func automatonPrepare(args []string, out, errout io.Writer) error {
	f := flags("automaton-prepare", errout)
	spec := f.String("spec", "", "caller-declared lattice gas spec JSON")
	hash := f.String("sha256", "", "caller-pinned exact spec digest")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *spec == "" || *hash == "" {
		return errors.New("automaton spec and digest required")
	}
	raw, err := read(*spec, validation.MaxBytes)
	if err != nil {
		return err
	}
	authority, err := automata.Prepare(raw, *hash)
	if err != nil {
		return err
	}
	_, err = out.Write(authority)
	return err
}

// solveAutomaton uses the exit codes of solve: 0 for a complete result, 1 for a
// rejected one, and 2 for incomplete work or errors.
func solveAutomaton(ctx context.Context, args []string, out, errout io.Writer) int {
	f := flags("solve-automaton", errout)
	authority := f.String("authority", "", "caller-owned automaton authority JSON")
	hash := f.String("authority-sha256", "", "caller-pinned exact authority digest")
	request := f.String("request-id", "", "caller-selected request ID")
	sources := f.String("sources", "", "source manifest")
	dir := f.String("out", "", "new result directory")
	solver := f.String("solver", "", "CaDiCaL executable")
	solverHash := f.String("solver-sha256", "", "pinned solver SHA-256")
	checker := f.String("checker", "", "DRAT-trim executable")
	checkerHash := f.String("checker-sha256", "", "pinned checker SHA-256")
	timeout := f.Duration("timeout", 2*time.Minute, "whole search and verification budget")
	queryTimeout := f.Duration("query-timeout", 10*time.Second, "budget shared by the solver and checker of one query")
	maxQueries := f.Int("max-queries", 10000, "solver call resource limit")
	maxVariables := f.Int("max-variables", 100000, "variables per formula")
	maxClauses := f.Int("max-clauses", 1000000, "clauses per formula")
	maxLiterals := f.Int("max-literals", 4000000, "literals per formula")
	maxInputBytes := f.Int("max-input-bytes", 16<<20, "bytes of each saved formula file")
	maxOrbitSteps := f.Int("max-orbit-steps", 100000, "steps walked to confirm a choice-free run")
	if e := f.Parse(args); e != nil {
		return 2
	}
	if f.NArg() != 0 || *authority == "" || *hash == "" || *request == "" || *sources == "" || *dir == "" || *timeout <= 0 || *queryTimeout <= 0 || *maxQueries <= 0 || *maxVariables <= 0 || *maxClauses <= 0 || *maxLiterals <= 0 || *maxInputBytes <= 0 || *maxOrbitSteps <= 0 {
		fmt.Fprintln(errout, "required inputs or limits missing")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	raw, e := read(*authority, validation.MaxBytes)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	a, e := automata.NewAuthority(raw, *hash)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	if a.RequestID() != *request {
		fmt.Fprintln(errout, "caller request mismatch")
		return 2
	}
	manifest, content, e := run.ReadSources(a.SourceBinding(), *hash, *request, *sources)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	if e = os.Mkdir(*dir, 0755); e != nil {
		fmt.Fprintln(errout, "output directory must be new:", e)
		return 2
	}
	in, e := run.SaveInputs(run.Inputs{AuthorityRaw: raw, Sources: manifest, Content: content}, *dir)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	runner, e := sat.New(sat.Config{SolverPath: *solver, SolverSHA256: *solverHash, CheckerPath: *checker, CheckerSHA256: *checkerHash, ArtifactDir: filepath.Join(*dir, "solver"), Timeout: *queryTimeout, MaxVariables: *maxVariables, MaxClauses: *maxClauses, MaxLiterals: *maxLiterals, MaxInputBytes: *maxInputBytes})
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	result, searchErr := automata.Search(ctx, a, runner, automata.Options{MaxQueries: *maxQueries, MaxVariables: *maxVariables, MaxClauses: *maxClauses, MaxLiterals: *maxLiterals, MaxOrbitSteps: *maxOrbitSteps})
	bundle, certErr := automata.Certify(ctx, a, *hash, in.Sources, result)
	encoded, e := json.MarshalIndent(bundle, "", "  ")
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	if e = saveGovernanceFile(*dir, "bundle.json", append(encoded, '\n')); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	if e = run.Seal(*dir); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	summary := struct {
		Status         string `json:"status"`
		Complete       bool   `json:"complete"`
		RecoveryStatus string `json:"recovery_status"`
		Reason         string `json:"reason,omitempty"`
		Queries        int    `json:"queries"`
		Bundle         string `json:"bundle"`
	}{bundle.Status, bundle.Complete, bundle.RecoveryStatus, bundle.Reason, len(result.Queries), "bundle.json"}
	if e = output(out, summary); e != nil {
		fmt.Fprintln(errout, e)
		return 2
	}
	switch {
	case certErr != nil:
		fmt.Fprintln(errout, certErr)
		if bundle.Status == "REJECTED" {
			return 1
		}
		return 2
	case searchErr != nil:
		fmt.Fprintln(errout, searchErr)
		return 2
	case !bundle.Complete:
		return 2
	}
	return 0
}
