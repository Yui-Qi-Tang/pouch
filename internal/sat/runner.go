// Package sat runs caller-pinned CaDiCaL and DRAT-trim executables. Successful
// process output is insufficient: assignments and proofs must be checked.
package sat

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Yui-Qi-Tang/pouch/internal/planning"
)

// Config explicitly pins tools and a new evidence directory. Paths are local
// execution settings and are not included in portable result metadata.
type Config struct {
	SolverPath     string
	SolverSHA256   string
	CheckerPath    string
	CheckerSHA256  string
	ArtifactDir    string
	Timeout        time.Duration
	MaxOutputBytes int64
	MaxProofBytes  int64
}

// Runner keeps private pinned tool copies and never overwrites query evidence.
// One runner serializes calls so execution and resource accounting stay simple.
type Runner struct {
	mu        sync.Mutex
	root      string
	solver    string
	checker   string
	timeout   time.Duration
	maxOutput int64
	maxProof  int64
}

// New verifies exact executable hashes and creates a fresh private directory.
// Fixed private copies prevent later edits to the caller's paths changing a run.
func New(c Config) (*Runner, error) {
	if c.ArtifactDir == "" {
		return nil, fmt.Errorf("artifact directory is required")
	}
	if c.Timeout < 0 || c.MaxOutputBytes < 0 || c.MaxProofBytes < 0 {
		return nil, fmt.Errorf("negative tool limit")
	}
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 16 << 20
	}
	if c.MaxProofBytes == 0 {
		c.MaxProofBytes = 64 << 20
	}
	solver, err := readTool(c.SolverPath, c.SolverSHA256)
	if err != nil {
		return nil, fmt.Errorf("solver pin: %w", err)
	}
	checker, err := readTool(c.CheckerPath, c.CheckerSHA256)
	if err != nil {
		return nil, fmt.Errorf("checker pin: %w", err)
	}
	root, err := filepath.Abs(c.ArtifactDir)
	if err != nil {
		return nil, fmt.Errorf("invalid artifact directory")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, fmt.Errorf("artifact directory must be new and its parent writable")
	}
	if err := os.Mkdir(filepath.Join(root, "tools"), 0700); err != nil {
		return nil, fmt.Errorf("creating private tool directory")
	}
	r := &Runner{root: root, solver: filepath.Join(root, "tools", "cadical"), checker: filepath.Join(root, "tools", "drat-trim"), timeout: c.Timeout, maxOutput: c.MaxOutputBytes, maxProof: c.MaxProofBytes}
	if err := os.WriteFile(r.solver, solver, 0500); err != nil {
		return nil, fmt.Errorf("saving pinned solver")
	}
	if err := os.WriteFile(r.checker, checker, 0500); err != nil {
		return nil, fmt.Errorf("saving pinned checker")
	}
	profile := struct {
		SchemaVersion string `json:"schema_version"`
		SolverSHA256  string `json:"solver_sha256"`
		CheckerSHA256 string `json:"checker_sha256"`
		Verification  string `json:"verification"`
	}{"pouch-sat-tools/v1", c.SolverSHA256, c.CheckerSHA256, "SAT: exit 10 and exact s SATISFIABLE and every clause; UNSAT: exit 20 and exact s UNSATISFIABLE, then checker exit 0 and exact s VERIFIED"}
	if err := writeJSON(filepath.Join(root, "tools.json"), profile); err != nil {
		return nil, err
	}
	return r, nil
}
func readTool(path, expected string) ([]byte, error) {
	hash, err := hex.DecodeString(expected)
	if err != nil || len(hash) != 32 || expected != hex.EncodeToString(hash) {
		return nil, fmt.Errorf("lowercase sha256 is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("executable unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil, fmt.Errorf("not an executable regular file")
	}
	if info.Size() > 64<<20 {
		return nil, fmt.Errorf("executable exceeds size limit")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil || len(raw) > 64<<20 {
		return nil, fmt.Errorf("reading executable")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expected {
		return nil, fmt.Errorf("executable hash mismatch")
	}
	return raw, nil
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

// Solve preserves DIMACS, variables, proof, process logs and assignment before
// reporting verification. A nonzero checker exit is failure even with VERIFIED.
func (r *Runner) Solve(ctx context.Context, id string, cnf planning.CNF) (result planning.SolveResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result.Status = "INCONCLUSIVE"
	result.Artifacts = []planning.Artifact{}
	if !safeID.MatchString(id) {
		return result, fmt.Errorf("invalid query artifact id")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	dir := filepath.Join(r.root, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		return result, fmt.Errorf("query artifact directory must be new")
	}
	defer func() {
		artifacts, artifactErr := inventory(dir, id)
		result.Artifacts = artifacts
		if artifactErr != nil {
			result.Verified = false
			err = errors.Join(err, artifactErr)
		}
	}()
	input, err := os.OpenFile(filepath.Join(dir, "input.cnf"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, fmt.Errorf("creating formula evidence")
	}
	writeErr := cnf.WriteDIMACS(input)
	closeErr := input.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return result, fmt.Errorf("saving formula: %w", err)
	}
	if err := writeJSON(filepath.Join(dir, "variables.json"), cnf.Variables); err != nil {
		return result, err
	}
	process, err := r.execute(ctx, dir, "solver", r.solver, "--no-binary", "--seed=0", "input.cnf", "proof.drat")
	if err != nil {
		return result, err
	}
	stdout, err := os.ReadFile(filepath.Join(dir, "solver.stdout"))
	if err != nil {
		return result, fmt.Errorf("reading solver output")
	}
	switch {
	case process.ExitCode == 10 && exactLine(stdout, "s SATISFIABLE") && !exactLine(stdout, "s UNSATISFIABLE"):
		result.Status = "SAT"
		values, err := assignment(stdout, len(cnf.Variables))
		if err != nil {
			return result, err
		}
		if err := cnf.CheckAssignment(values); err != nil {
			return result, err
		}
		if err := writeJSON(filepath.Join(dir, "assignment.json"), values); err != nil {
			return result, err
		}
		result.Assignment = values
		result.Verified = true
		return result, nil
	case process.ExitCode == 20 && exactLine(stdout, "s UNSATISFIABLE") && !exactLine(stdout, "s SATISFIABLE"):
		result.Status = "UNSAT"
		proof, err := os.Lstat(filepath.Join(dir, "proof.drat"))
		if err != nil || !proof.Mode().IsRegular() || proof.Size() > r.maxProof {
			return result, fmt.Errorf("proof missing, nonregular or over size limit")
		}
		checked, err := r.execute(ctx, dir, "checker", r.checker, "input.cnf", "proof.drat")
		if err != nil {
			return result, err
		}
		output, err := os.ReadFile(filepath.Join(dir, "checker.stdout"))
		if err != nil {
			return result, fmt.Errorf("reading checker output")
		}
		if checked.ExitCode != 0 || !exactLine(output, "s VERIFIED") || exactLine(output, "s NOT VERIFIED") {
			return result, fmt.Errorf("proof checker did not verify with exit zero")
		}
		result.Verified = true
		return result, nil
	default:
		return result, fmt.Errorf("solver status and exit code do not match the tool contract")
	}
}

type execution struct {
	ExitCode   int      `json:"exit_code"`
	TimedOut   bool     `json:"timed_out"`
	Canceled   bool     `json:"canceled"`
	DurationMS int64    `json:"duration_ms"`
	Arguments  []string `json:"arguments"`
}

func (r *Runner) execute(ctx context.Context, dir, stem, executable string, args ...string) (execution, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	record := execution{ExitCode: -1, Arguments: append([]string{filepath.Base(executable)}, args...)}
	stdout, err := os.OpenFile(filepath.Join(dir, stem+".stdout"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return record, fmt.Errorf("creating tool output")
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(filepath.Join(dir, stem+".stderr"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return record, fmt.Errorf("creating tool error output")
	}
	defer stderr.Close()
	out := &limitedWriter{w: stdout, remaining: r.maxOutput}
	errOut := &limitedWriter{w: stderr, remaining: r.maxOutput}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = errOut
	controlProcess(cmd)
	start := time.Now()
	runErr := cmd.Run()
	record.DurationMS = time.Since(start).Milliseconds()
	if cmd.ProcessState != nil {
		record.ExitCode = cmd.ProcessState.ExitCode()
	}
	record.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	record.Canceled = errors.Is(ctx.Err(), context.Canceled)
	if err := writeJSON(filepath.Join(dir, stem+".json"), record); err != nil {
		return record, err
	}
	if ctx.Err() != nil {
		return record, ctx.Err()
	}
	if out.exceeded || errOut.exceeded {
		return record, fmt.Errorf("tool output exceeded size limit")
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return record, fmt.Errorf("tool failed to execute")
		}
	}
	return record, nil
}

type limitedWriter struct {
	w         io.Writer
	remaining int64
	exceeded  bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		w.exceeded = true
		return 0, fmt.Errorf("tool output limit")
	}
	n, err := w.w.Write(p)
	w.remaining -= int64(n)
	return n, err
}
func exactLine(raw []byte, want string) bool {
	for _, line := range strings.FieldsFunc(string(raw), func(r rune) bool { return r == '\n' || r == '\r' }) {
		if line == want {
			return true
		}
	}
	return false
}
func assignment(raw []byte, count int) (map[int]bool, error) {
	values := make(map[int]bool, count)
	ended := false
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) == 0 || parts[0] != "v" {
			continue
		}
		for _, part := range parts[1:] {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("malformed assignment literal")
			}
			if n == 0 {
				ended = true
				continue
			}
			if ended {
				return nil, fmt.Errorf("assignment continues after terminator")
			}
			id := n
			if id < 0 {
				id = -id
			}
			if id < 1 || id > count {
				return nil, fmt.Errorf("assignment literal outside variable range")
			}
			value := n > 0
			if prior, ok := values[id]; ok && prior != value {
				return nil, fmt.Errorf("contradictory assignment literals")
			}
			values[id] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading assignment: %w", err)
	}
	if !ended || len(values) != count {
		return nil, fmt.Errorf("incomplete assignment")
	}
	return values, nil
}
func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding evidence: %w", err)
	}
	raw = append(raw, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("creating evidence file")
	}
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if errors.Join(writeErr, closeErr) != nil {
		return fmt.Errorf("writing evidence file")
	}
	return nil
}
func inventory(dir, id string) ([]planning.Artifact, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing evidence files")
	}
	artifacts := make([]planning.Artifact, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return artifacts, fmt.Errorf("unexpected nonregular evidence")
		}
		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return artifacts, fmt.Errorf("opening evidence")
		}
		hash := sha256.New()
		n, copyErr := io.Copy(hash, f)
		closeErr := f.Close()
		if errors.Join(copyErr, closeErr) != nil {
			return artifacts, fmt.Errorf("hashing evidence")
		}
		artifacts = append(artifacts, planning.Artifact{ID: id + "/" + entry.Name(), SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: n})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].ID < artifacts[j].ID })
	return artifacts, nil
}
