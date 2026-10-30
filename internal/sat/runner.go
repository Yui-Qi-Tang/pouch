// Package sat adapts Calx's pinned CaDiCaL and DRAT-trim runner to the planning
// contract. Successful process output is insufficient: assignments and proofs
// must be checked.
package sat

import (
	"context"
	"errors"
	"time"

	"github.com/Yui-Qi-Tang/calx/digest"
	calxsat "github.com/Yui-Qi-Tang/calx/sat"
	"github.com/Yui-Qi-Tang/calx/sat/cadical"
	"github.com/Yui-Qi-Tang/pouch/internal/planning"
)

// Config explicitly pins tools and a new evidence directory. Paths are local
// execution settings and are not included in portable result metadata.
type Config struct {
	SolverPath    string
	SolverSHA256  string
	CheckerPath   string
	CheckerSHA256 string
	ArtifactDir   string
	// Timeout is shared by the solver and checker of one query.
	Timeout        time.Duration
	MaxOutputBytes int64
	MaxProofBytes  int64
	// Zero selects the Calx default for each formula limit.
	MaxVariables  int
	MaxClauses    int
	MaxLiterals   int
	MaxInputBytes int
}

// Runner keeps private pinned tool copies and never overwrites query evidence.
type Runner struct {
	tools *cadical.Runner
}

// New verifies exact executable hashes and creates a fresh private directory.
// Fixed private copies prevent later edits to the caller's paths changing a run.
func New(c Config) (*Runner, error) {
	tools, err := cadical.New(cadical.Config{
		SolverPath:    c.SolverPath,
		SolverDigest:  digest.Digest(c.SolverSHA256),
		CheckerPath:   c.CheckerPath,
		CheckerDigest: digest.Digest(c.CheckerSHA256),
		ArtifactDir:   c.ArtifactDir,
		Limits:        calxsat.Limits{Timeout: c.Timeout, MaxOutputBytes: c.MaxOutputBytes, MaxVariables: c.MaxVariables, MaxClauses: c.MaxClauses, MaxLiterals: c.MaxLiterals, MaxInputBytes: c.MaxInputBytes},
		MaxProofBytes: c.MaxProofBytes,
	})
	if err != nil {
		return nil, err
	}
	return &Runner{tools: tools}, nil
}

// Solve preserves the formula, proof, process logs and assignment before
// reporting verification. The assignment is returned only after both checks
// pass, because Calx also reports one that failed the model check.
func (r *Runner) Solve(ctx context.Context, id string, cnf calxsat.CNF) (planning.SolveResult, error) {
	result := planning.SolveResult{Status: "INCONCLUSIVE", Artifacts: []planning.Artifact{}}
	solved, err := r.tools.Solve(ctx, id, cnf)
	for _, a := range solved.Artifacts {
		result.Artifacts = append(result.Artifacts, planning.Artifact{ID: a.ID, SHA256: a.Digest.String(), Bytes: a.Bytes})
	}
	if solved.Status == calxsat.SAT || solved.Status == calxsat.UNSAT {
		result.Status = string(solved.Status)
	}
	if err != nil {
		return result, portable(err)
	}
	if err := solved.Require(calxsat.Requirements{SATModelChecked: true, UNSATProofChecked: true}); err != nil {
		return result, portable(err)
	}
	result.Assignment = solved.Assignment
	result.Verified = true
	return result, nil
}

// portable reduces a Calx error to its category. Its messages can name host
// paths, and query errors are saved in shareable bundles.
func portable(err error) error {
	for _, category := range []error{context.DeadlineExceeded, context.Canceled, calxsat.ErrLimit, calxsat.ErrInput, calxsat.ErrProtocol, calxsat.ErrValidation, calxsat.ErrInconclusive, calxsat.ErrGuarantee} {
		if errors.Is(err, category) {
			return category
		}
	}
	return errors.New("tool execution failed")
}
