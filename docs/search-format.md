# Go planning and SAT evidence

The current implementation compiles `pouch-finite-validation/v1` contracts. It
supports categorical string coordinates, exact conjunctive guards, constant
effects, explicit frame partitions, forbidden state patterns and unit costs.
It does not synthesize operations or interpret evidence edge labels as guards.

## Projection and formulas

`planning.Project` sorts fields, values and action IDs. Rows are actions. Guard
and effect columns identify `(field, value)` coordinates. A guard cell of 1
requires that exact value; an absent guard field has all-zero cells. Effect
cells of 1 assign the value. Frame columns identify the unchanged fields.
`Matrix.Actions` reconstructs the actions of a valid projection. Source identity,
assumptions, forbidden patterns and queries remain in the original authority;
the matrix alone is not the complete problem.

Each exact-length formula has one-hot state values at every step and one-hot
action choices at every transition. It includes the complete initial state,
domain exclusions, final Goal, guard/effect/frame implications, no earlier Goal,
and pairwise nonrepetition of complete states. The encoder consumes the operation
matrix and the declared domain/query, never a reference transition table.

Forward search blocks complete action sequences after each SAT model. Helpers
and state variables do not define path identity: two distinct declared action
IDs can produce distinct paths even when their state traces are identical.
Every length 0 through `forward_limit` must reach verified UNSAT after blocking
its paths before `forward_complete` becomes true. A zero-step path is valid if
the initial state already meets Goal; it too is exhausted with an empty blocking
clause. Complete means complete only under these fixed bounds and policies.

Return search is separate. Every distinct complete forward endpoint gets one
shortest-return search, in increasing length, up to `return_limit`. A miss is
`NO_RETURN_WITHIN_BOUND`, not proof of impossibility in the full finite model.
The receiver may independently establish full closure and build a no-return
certificate. A return trace is a model operation trace, not a physical rollback.

## Tool contract and files

`sat.New` requires explicit executable paths and SHA-256 pins. It copies the
verified bytes into a new private artifact directory. No historical checker is
silently selected. The caller provides the tool profile; executable files and
portable `tools.json` preserve the actual tool identity.

Each query uses a safe generated `qNNNNNN` directory containing the exact
`input.cnf`, `variables.json`, process stdout/stderr and exit records. SAT adds a
checked `assignment.json`; UNSAT retains `proof.drat` and checker records.
Artifact references are relative IDs with exact byte hashes. Process arguments
use local relative formula/proof names; tool metadata contains no host paths.

Acceptance requires:

- SAT: CaDiCaL exit 10, exact `s SATISFIABLE` status, a total noncontradictory
  assignment, and every original clause satisfied.
- UNSAT: CaDiCaL exit 20, exact `s UNSATISFIABLE`, and DRAT-trim exit 0 plus exact
  `s VERIFIED`. CR and LF are line separators; extra status text is not accepted.

Proof validity applies to the saved CNF. Independent original-rule replay and
set comparison remain required to establish correspondence to the authority.

## Operational bounds and partial results

Default limits are 10,000 solver queries, 1,000 forward paths, 100,000 variables
and 1,000,000 clauses per formula. The quadratic nonrepetition encoding is
estimated before allocating time-indexed rows. The runner defaults to 30 seconds
per tool, 16 MiB per output stream and a 64 MiB proof check limit. The proof size
limit is checked after solver exit; it is not a filesystem quota.

At the exact forward path cap, another query is allowed to establish exhaustion.
If that query finds another path, the preserved set is partial and the call
returns `ErrPathLimit`. Query/encoding limits, cancellation, malformed tool
output and proof failures yield `INCONCLUSIVE`, preserving earlier evidence.
They are never converted to UNSAT. `forward_complete` may remain true if a later
return search was interrupted; overall `complete` is false on any error.

## Tests

The small propositional oracle in `planning/search_test.go` is test-only. Normal
regressions cover every cell of a tiny one-step relation, same-length multiple
solutions, action identity, zero-step paths, legal cycles excluded by policy,
budget edges and bounded-return misses. Process tests cover wrong assignments,
VERIFIED with exit 1, tool pin mismatches, immutable private copies, timeouts and
artifact traversal/overwrite refusal.

Optional real-tool tests use `POUCH_TEST_CADICAL`, `POUCH_TEST_CADICAL_SHA256`,
`POUCH_TEST_DRAT_TRIM`, and `POUCH_TEST_DRAT_TRIM_SHA256`. With
`POUCH_TEST_AUTHORITY_DIR`, the two existing migration fixtures are searched
first; only afterward are historical package path sets read for comparison.
These tests also submit the new certificates to the independent Go receiver.
