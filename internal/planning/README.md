# Planning fields and values

This package compiles an explicit finite model into an operation matrix and CNF,
then searches it through the caller-supplied `Solver`. It does not read AHE,
invent actions, execute a repair, or issue the final original-rule receipt.

Definitions below describe the current implementation. String values are still
Go `string` fields, not compiler-enforced enums. A zero-value result is not a
completed search. Unknown or empty status values must not be interpreted as
success. Check the returned Go error as well as the fields.

## Inputs and units

`Search(ctx, contract, solver, options)` takes a caller-selected
`validation.Contract`; see [its types](../validation/validation.go).

| Contract field | Meaning and allowed values |
|---|---|
| `SchemaVersion` | `pouch-finite-validation/v1`. |
| `RequestID` | Caller-selected nonempty identity, at most 200 bytes. |
| `SourceBinding` | 1–8 source IDs mapped to exact-content SHA-256 strings; lowercase hexadecimal, 64 characters. IDs are nonempty and at most 200 bytes. |
| `Assumptions` | Explicit string-to-string declarations. An empty object is permitted; null or missing is not. Assumptions are not observed facts. |
| `Model.Fields` | 1–16 categorical fields, each with 1–64 distinct nonempty string values. Field names and values are at most 200 bytes. The product of domain sizes is at most 4,096. Boolean or numeric JSON values are not coerced into strings. |
| `Model.Forbidden` | At most 256 nonempty partial state patterns. A complete state matching every entry of any one pattern is excluded. |
| `Model.Actions` | 1–64 actions with unique nonempty IDs, at most 200 bytes. Guards are exact conjunctions; effects assign constant values. Every field occurs exactly once in either effect or frame. An empty guard is allowed; an empty effect is not. |
| `Start` / `Baseline` | Complete valid assignments, with one declared value for every field. Baseline is the recovery target; it need not equal Start. |
| `Goal` | Nonempty partial or complete assignment. All listed conditions must match. |
| `ForwardLimit` / `ReturnLimit` | Inclusive maximum action counts, integers 0–4,095. Zero means only the initial state is checked, not unlimited. |

One action costs one model step. A trace with `n` actions has `n+1` complete
states. Neither model steps nor these limits measure elapsed time, physical
rollback effort, or technical debt.

`Options` are resource budgets, separate from the problem's length bounds:

| Field | Unit | Zero selects | Negative |
|---|---|---:|---|
| `MaxQueries` | Solver calls across forward and return searches | 10,000 | Rejected |
| `MaxPaths` | Enumerated forward paths | 1,000 | Rejected |
| `MaxVariables` | DIMACS variables per formula | 100,000 | Rejected |
| `MaxClauses` | Clauses per formula | 1,000,000 | Rejected |

Budget exhaustion returns `ErrQueryLimit`, `ErrPathLimit`, or `ErrEncodingLimit`;
it never establishes that a path is impossible. At exactly `MaxPaths`, one more
solver call may establish exhaustion; another SAT path makes the result partial.

## `Result`: the whole bounded search

| Field | Meaning / values |
|---|---|
| `SchemaVersion` | `pouch-search/v1`. |
| `Policy` | Fixed `simple-first-hit/action-sequence`; defined below. It is an output label, not a selectable input strategy. |
| `Matrix` | Operation projection used by the encoder, not an evidence adjacency matrix. May be empty if input validation failed. |
| `Options` | Effective resource budgets after defaults; not initialized if option validation failed. |
| `ForwardLimit` / `ReturnLimit` | Copies of the contract's inclusive length bounds. |
| `Forward` | Forward paths discovered so far, possibly a partial collection. |
| `Returns` | Separate recovery searches for distinct complete forward endpoints. Several forward paths may refer to the same entry. |
| `Queries` | Solver invocation ledger. Failures before a solver call do not create a query entry. |
| `ForwardComplete` | Every length 0 through `ForwardLimit` reached verified UNSAT after blocking all found action sequences. This can remain true if recovery later fails. |
| `Complete` | Forward enumeration and the requested per-endpoint return searches finished without error. A completed bounded return miss also counts as finished. This does not mean every endpoint is recoverable or every receiver check passed. |
| `Status` | One of the search statuses below. |
| `Error` | Diagnostic text when Search returns an error; empty/omitted on success. Not a stable error-code enum. |

`Policy` has three parts:

- **simple**: no repeated complete state within a path, including self-loops.
- **first-hit**: stop at the first state satisfying the target; no later actions.
- **action-sequence**: distinguish paths by their ordered action IDs. Two
  different actions can distinguish paths even if the state sequences coincide.

Each transition selects exactly one declared action; the encoder adds no idle
action. Forward search enumerates all such paths within its bound. Recovery
uses the same simple/first-hit constraints but finds only one shortest witness
per distinct endpoint, not all return paths. CNF helper assignments do not create
new path identities.

| `Result.Status` | Condition in the current Search implementation |
|---|---|
| `FOUND` | Search completed and `Forward` is nonempty. The receiver has not yet checked the result against the original rules. |
| `NO_PATH_WITHIN_BOUND` | Search completed with no forward paths under this policy and length bound. No claim of global/model-wide impossibility. |
| `INCONCLUSIVE` | Initial status, or any returned error, including cancellation, budgets and unverified solver output. Earlier paths/queries are retained; `Complete=false`. |

For results returned by Search: `Complete=true` implies `ForwardComplete=true`
and a nil error. A partial result can have paths while its status is
`INCONCLUSIVE`. These rules do not authenticate an arbitrary deserialized struct.
The later [run.Bundle status](../run/README.md) is a different contract.

## `Path` and `Return`

| `Path` field | Meaning / values |
|---|---|
| `ID` | Run-local generated ID such as `p000000`; not a global identity or content hash. |
| `Trace` | Full states, action IDs and `ClaimShortest`. Search sets that claim for the first successful length and all other paths of that length. It is not yet an original-rule receiver verdict. |
| `QueryID` | ID of the SAT query producing the trace. |
| `ReturnIndex` | Zero-based index into `Result.Returns`; `-1` means recovery has not been assigned. Zero is a valid index. |

| `Return` field | Meaning / values |
|---|---|
| `Endpoint` | Complete final state of the corresponding forward path(s); recovery starts here and targets the complete Baseline. |
| `Status` | `UNKNOWN`, `RETURN_FOUND`, or `NO_RETURN_WITHIN_BOUND`. |
| `Trace` | One shortest model return when found; nil if absent or unfinished. A non-nil trace with zero actions is a valid zero-step return. |
| `CompleteWithinBound` | The shortest-witness search has finished: either a witness was found after all shorter lengths failed, or every allowed length failed. It does not mean all return paths were enumerated. |
| `QueryIDs` | Attempted recovery solver calls, in increasing length. |

`UNKNOWN` is assigned when a return search starts and remains on interruption.
`RETURN_FOUND` means a SAT witness was decoded and all shorter lengths were
verified UNSAT. `NO_RETURN_WITHIN_BOUND` means lengths 0 through `ReturnLimit`
were verified UNSAT. It does **not** mean `NO_RETURN_IN_MODEL`.

Example: with return bound 2, a legal 3-step return produces
`NO_RETURN_WITHIN_BOUND`. Search can still be complete. The original-rule
receiver can discover that longer return and leave the bundle's recovery
conclusion `UNKNOWN` with `RETURN_EXISTS_BEYOND_SEARCH_BOUND`.

## Solver result, query ledger and artifacts

| Field | Meaning / values |
|---|---|
| `SolveResult.Status` | Successful calls must be `SAT` or `UNSAT`. The supplied SAT runner starts at `INCONCLUSIVE`; failure may preserve `SAT`/`UNSAT` if verification failed after the solver answered. |
| `SolveResult.Verified` | The solver implementation reports assignment/proof checking complete. Search requires this to be true, status SAT/UNSAT, and a nil error. The bundled runner checks the original CNF; it does not establish source meaning. |
| `SolveResult.Assignment` | For SAT, a total map of one-based variable IDs to Boolean values; omitted from this struct's JSON. For UNSAT, no assignment. |
| `SolveResult.Artifacts` | References to preserved formula, proof and process evidence; not the files themselves. |
| `Query.ID` | Run-local `q000000`-style identity. |
| `Query.Direction` | `forward` or `return`. |
| `Query.Horizon` | Exact nonnegative action count for this formula, not the overall maximum. |
| `Query.Status` / `Verified` | The solver response fields copied to the ledger, including unsuccessful calls. Check `Error` as well. |
| `Query.Artifacts` | Preserved files for that invocation. |
| `Query.Error` | Solver-call diagnostic, empty/omitted if that call returned no error. Later decoding or search checks may still fail at Result level. |
| `Artifact.ID` | File ID relative to the runner's artifact directory, e.g. `q000000/input.cnf`. |
| `Artifact.SHA256` / `Bytes` | Lowercase hex digest of exact file bytes and nonnegative byte count. These establish identity, not truth. |

## Operation matrix and CNF variables

`Matrix.Fields` is sorted by field name; `Coordinates` by field then categorical
value; `ActionIDs` by action ID. Every `Coordinate` is a `(Field, Value)` pair
from the declared model. Indexing is zero-based:

| Matrix field | Shape | Cell meaning |
|---|---|---|
| `Guard` | actions × coordinates | 1 requires that exact value; 0 adds no such requirement. An unconstrained field has all-zero guard cells. |
| `Effect` | actions × coordinates | 1 assigns that exact value; 0 adds no such assignment. |
| `Frame` | actions × fields | 1 preserves the field; 0 means the action assigns it through Effect. |

All matrix cells are 0 or 1. Effect and Frame partition the fields. A guard's
zero is **not** a requirement that the field be false. Example: an action that
requires `mode=old` and assigns `mode=new` has Guard 1 at `(mode,old)`, Effect 1
at `(mode,new)`, and Frame 0 for `mode`.

`CNF.Variables` maps contiguous DIMACS IDs 1…N to meanings; `Clauses` is a
conjunction of disjunctions of signed IDs. Positive means true, negative means
false. Literal 0 is not stored; WriteDIMACS adds it as a clause terminator.
An empty clause is unsatisfiable; an empty clause list is trivially satisfied.

| `Variable.Kind` | Meaning of true | Fields used |
|---|---|---|
| `state` | Field has Value at Time | `Time` 0…horizon, `Field`, `Value` |
| `action` | Action is chosen for Time → Time+1 | `Time` 0…horizon-1, `Action` |
| `different` | Auxiliary witness requiring Field to differ at two times | `Time < OtherTime`, `Field` |

Every Variable also has its positive `ID`. Unused strings are empty/omitted;
`OtherTime` is omitted outside `different`. `Time=0` is a real initial index.
A false `different` helper does **not** assert equality: it is only a one-way
witness used to ensure at least one field differs. Do not treat helper values
as physical state or path identity.

Implementation: [matrix.go](matrix.go), [cnf.go](cnf.go), [search.go](search.go).
Tool acceptance rules: [search-format.md](../../docs/search-format.md).
