package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Yui-Qi-Tang/pouch/internal/validation"
)

// Search resource limits are operational limits, never evidence of unreachability.
var (
	ErrQueryLimit    = errors.New("solver query budget exhausted")
	ErrPathLimit     = errors.New("forward path budget exhausted")
	ErrEncodingLimit = errors.New("formula size budget exhausted")
)

// Artifact identifies immutable solver evidence relative to the runner's directory.
type Artifact struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// SolveResult reports one independently checked SAT assignment or UNSAT proof.
type SolveResult struct {
	Status     string       `json:"status"`
	Assignment map[int]bool `json:"-"`
	Verified   bool         `json:"verified"`
	Artifacts  []Artifact   `json:"artifacts"`
}

// Solver is supplied by the application; the planner never selects executables.
type Solver interface {
	Solve(context.Context, string, CNF) (SolveResult, error)
}

// Options caps work without changing the declared query or claiming exhaustion.
// Zero selects conservative defaults; negative values are rejected.
type Options struct {
	MaxQueries   int `json:"max_queries"`
	MaxPaths     int `json:"max_paths"`
	MaxVariables int `json:"max_variables"`
	MaxClauses   int `json:"max_clauses"`
}

func (o Options) defaults() (Options, error) {
	if o.MaxQueries < 0 || o.MaxPaths < 0 || o.MaxVariables < 0 || o.MaxClauses < 0 {
		return o, fmt.Errorf("negative search budget")
	}
	if o.MaxQueries == 0 {
		o.MaxQueries = 10000
	}
	if o.MaxPaths == 0 {
		o.MaxPaths = 1000
	}
	if o.MaxVariables == 0 {
		o.MaxVariables = 100000
	}
	if o.MaxClauses == 0 {
		o.MaxClauses = 1000000
	}
	return o, nil
}

// Query records every solver invocation, including an unsuccessful tool call.
type Query struct {
	ID        string     `json:"id"`
	Direction string     `json:"direction"`
	Horizon   int        `json:"horizon"`
	Status    string     `json:"status"`
	Verified  bool       `json:"verified"`
	Artifacts []Artifact `json:"artifacts"`
	Error     string     `json:"error,omitempty"`
}

// Path is one action-sequence-distinct forward trace and its return search index.
type Path struct {
	ID          string           `json:"id"`
	Trace       validation.Trace `json:"trace"`
	QueryID     string           `json:"query_id"`
	ReturnIndex int              `json:"return_index"`
}

// Return is a separate shortest-path search from a complete reached state.
// NO_RETURN_WITHIN_BOUND does not establish no-return in the full finite model.
type Return struct {
	Endpoint            validation.State  `json:"endpoint"`
	Status              string            `json:"status"`
	Trace               *validation.Trace `json:"trace"`
	CompleteWithinBound bool              `json:"complete_within_bound"`
	QueryIDs            []string          `json:"query_ids"`
}

// Result keeps accepted evidence on interruption; Complete never survives an error.
type Result struct {
	SchemaVersion   string   `json:"schema_version"`
	Policy          string   `json:"policy"`
	Matrix          Matrix   `json:"matrix"`
	Options         Options  `json:"options"`
	ForwardLimit    int      `json:"forward_limit"`
	ReturnLimit     int      `json:"return_limit"`
	Forward         []Path   `json:"forward"`
	Returns         []Return `json:"returns"`
	Queries         []Query  `json:"queries"`
	ForwardComplete bool     `json:"forward_complete"`
	Complete        bool     `json:"complete"`
	Status          string   `json:"status"`
	Error           string   `json:"error,omitempty"`
}

// Search enumerates all simple first-hit forward paths within the declared bound,
// then seeks one shortest return for every distinct complete endpoint. It uses no
// direct evaluator, expected path collection or historical answer as search input.
func Search(ctx context.Context, c validation.Contract, solver Solver, options Options) (result Result, err error) {
	result = Result{SchemaVersion: "pouch-search/v1", Policy: "simple-first-hit/action-sequence", Status: "INCONCLUSIVE", ForwardLimit: c.ForwardLimit, ReturnLimit: c.ReturnLimit, Forward: []Path{}, Returns: []Return{}, Queries: []Query{}}
	defer func() {
		if err != nil {
			result.Complete = false
			result.Status = "INCONCLUSIVE"
			result.Error = err.Error()
		}
	}()
	opts, err := options.defaults()
	if err != nil {
		return result, err
	}
	result.Options = opts
	if solver == nil {
		return result, fmt.Errorf("missing solver")
	}
	matrix, err := Project(c)
	if err != nil {
		return result, err
	}
	result.Matrix = matrix
	solve := func(direction string, h int, blocked [][]string, start, goal validation.State) (encoding, SolveResult, string, error) {
		if err := ctx.Err(); err != nil {
			return encoding{}, SolveResult{}, "", err
		}
		if len(result.Queries) >= opts.MaxQueries {
			return encoding{}, SolveResult{}, "", ErrQueryLimit
		}
		enc, err := encode(ctx, matrix, c.Model.Forbidden, start, goal, h, blocked, opts)
		if err != nil {
			return encoding{}, SolveResult{}, "", err
		}
		id := fmt.Sprintf("q%06d", len(result.Queries))
		response, err := solver.Solve(ctx, id, enc.CNF)
		q := Query{ID: id, Direction: direction, Horizon: h, Status: response.Status, Verified: response.Verified, Artifacts: response.Artifacts}
		if err != nil {
			q.Error = err.Error()
		}
		result.Queries = append(result.Queries, q)
		if err != nil {
			return enc, response, id, err
		}
		if !response.Verified || (response.Status != "SAT" && response.Status != "UNSAT") {
			return enc, response, id, fmt.Errorf("solver result lacks verified status")
		}
		return enc, response, id, nil
	}
	shortest := -1
	for h := 0; h <= c.ForwardLimit; h++ {
		blocked := [][]string{}
		for {
			enc, response, id, solveErr := solve("forward", h, blocked, c.Start, c.Goal)
			if solveErr != nil {
				return result, solveErr
			}
			if response.Status == "UNSAT" {
				break
			}
			trace, decodeErr := enc.trace(response.Assignment)
			if decodeErr != nil {
				return result, decodeErr
			}
			// The next UNSAT is permitted at the exact path budget, so a fully enumerated
			// set is not mislabeled incomplete merely because its size equals the cap.
			if len(result.Forward) >= opts.MaxPaths {
				return result, ErrPathLimit
			}
			if shortest < 0 {
				shortest = h
			}
			trace.ClaimShortest = h == shortest
			result.Forward = append(result.Forward, Path{ID: fmt.Sprintf("p%06d", len(result.Forward)), Trace: trace, QueryID: id, ReturnIndex: -1})
			blocked = append(blocked, trace.Actions)
		}
	}
	result.ForwardComplete = true
	endpoints := map[string]int{}
	for i, path := range result.Forward {
		endpoint := path.Trace.States[len(path.Trace.States)-1]
		keyBytes, _ := json.Marshal(endpoint) // State consists only of strings.
		key := string(keyBytes)
		if index, ok := endpoints[key]; ok {
			result.Forward[i].ReturnIndex = index
			continue
		}
		index := len(result.Returns)
		endpoints[key] = index
		result.Forward[i].ReturnIndex = index
		result.Returns = append(result.Returns, Return{Endpoint: endpoint, Status: "UNKNOWN", QueryIDs: []string{}})
		for h := 0; h <= c.ReturnLimit; h++ {
			enc, response, id, solveErr := solve("return", h, nil, endpoint, c.Baseline)
			if id != "" {
				result.Returns[index].QueryIDs = append(result.Returns[index].QueryIDs, id)
			}
			if solveErr != nil {
				return result, solveErr
			}
			if response.Status == "SAT" {
				trace, decodeErr := enc.trace(response.Assignment)
				if decodeErr != nil {
					return result, decodeErr
				}
				trace.ClaimShortest = true
				result.Returns[index].Trace = &trace
				result.Returns[index].Status = "RETURN_FOUND"
				result.Returns[index].CompleteWithinBound = true
				break
			}
		}
		if result.Returns[index].Trace == nil {
			result.Returns[index].Status = "NO_RETURN_WITHIN_BOUND"
			result.Returns[index].CompleteWithinBound = true
		}
	}
	result.Complete = true
	result.Status = "FOUND"
	if len(result.Forward) == 0 {
		result.Status = "NO_PATH_WITHIN_BOUND"
	}
	return result, nil
}
