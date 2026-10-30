# Automaton authorities

This package holds the `pouch-automaton/v1` authority for synchronous Boolean
automata, such as lattice gases. All cells update at once, from the previous
state of their inputs. This differs from `pouch-finite-validation/v1`, where one
declared action runs at each step. The two formats and their limits are separate.

The package validates authorities, searches them through pinned solver tools and
checks the results against the rule tables. Use `pouch solve-automaton`; `pouch
solve` does not accept this schema.

## Authority

| Field | Meaning |
|---|---|
| `schema_version` | `pouch-automaton/v1`. |
| `request_id` | Caller-selected nonempty identity, at most 200 bytes. |
| `source_binding` | 1–32 source IDs mapped to exact-content SHA-256 strings. |
| `assumptions` | Explicit string-to-string declarations; may be empty, not absent. |
| `model.bits` | State bits per cell, 1–16. |
| `model.rules` | 1–256 rule tables. |
| `model.cells` | 1–1,048,576 cells, each with `rule` (an index into `rules`) and `inputs`. |
| `start` / `baseline` | Complete states, as bit strings. |
| `goal` | `mask` and `value` bit strings. `mask` must set a bit; `value` may set only masked bits. |
| `forward_limit` / `return_limit` | Inclusive maximum step counts, 0–4,095. |
| `display` | Optional layout for viewers: `kind` (`hpp`, `fhp` or `grid`), `columns`, `rows` and `walls`. |

The whole file is at most 4 MiB and follows the same strict JSON rules as other
authorities: no nulls, unknown fields, missing fields or duplicate keys. The size
limit is reached before the cell limit: an HPP lattice of 256×256 takes 3.4 MiB.
The digest of its exact bytes is the caller's pin.

A state bit's address is `cell*bits+index`. A cell's `inputs` list distinct
addresses, and their count must equal the rule's `inputs`.

`display` has no effect on the rule, the search or any check; its `walls` only
mark cells in the viewer. `columns` times `rows` must equal the cell count, `hpp`
needs 4 bits per cell and `fhp` needs 6. Without it, a viewer shows one row.

## Rule tables

A rule has `inputs` and `choices`, whose sum is at most 12. Choices are free bits,
which make a rule nondeterministic. Its row number is built from the input values
followed by the choice values, with the first input as the lowest bit:

```text
row = in[0] + 2*in[1] + … + 2^(inputs)*choice[0] + …
```

`table` holds `bits` output bits for each of the `2^(inputs+choices)` rows, in row
order: output bit `i` of row `r` is table bit `r*bits+i`.

A bit string is lowercase hexadecimal. Bit `i` is bit `i%8` of byte `i/8`. Its
length is exact and unused high bits must be zero, so each value has one spelling.

## Replay

`Authority.Replay` recomputes every step from the tables and compares the result
with the recorded state. It uses Calx types but not Calx's simulator or encoder,
so it checks a trajectory independently of the CNF encoding. It shows that a run follows the declared rule,
not that the rule models anything real.

## Preparing a lattice gas

`pouch automaton-prepare --spec spec.json --sha256 "$SPEC_SHA256"` writes an
authority to stdout. A spec has `schema_version` `pouch-automaton-spec/v1`, and
otherwise the authority's fields, with `lattice` in place of `model` and no
`display`, which the command fills in from `lattice`:

| `lattice` field | Meaning |
|---|---|
| `rule` | `hpp` or `fhp`, from the Calx `lga` package. |
| `width` / `height` | Lattice size; their product is at most 1,048,576. |
| `walls` | Wall cell indices; may be empty, not absent. |

The command evaluates each cell's rule on every row and shares equal tables, so
the same spec produces the same bytes. The spec digest is recorded in the
`pouch-automaton-spec-sha256` assumption, which a spec may not set itself. After
that, the authority stands alone: the tables are the rule, and later Calx changes
cannot alter what it means.

Tests compare these tables with `lga.Run`, Calx's separate bit-packed simulator,
and compare their CNF encoding digests with those of the `lga` types.

## Lattice gas state layout

A state lists every cell's bits in cell order, so the bit of direction `d` in cell
`c` is at address `c*bits+d`. Cells are numbered along rows from the south edge.
The lattice wraps at its edges. A set bit is a particle leaving that cell in that
direction; on the next step it arrives at the neighbor and collides there.

| Rule | Bits per cell | Cell number | Direction bits |
|---|---:|---|---|
| `hpp` | 4 | `y*width+x`, with `y` increasing north | 0 east, 1 north, 2 west, 3 south |
| `fhp` | 6 | `r*width+q`, in axial coordinates | 0 E, 1 NE, 2 NW, 3 W, 4 SW, 5 SE |

An FHP step along a direction changes `(q, r)` by `(1,0)`, `(0,1)`, `(-1,1)`,
`(-1,0)`, `(0,-1)` or `(1,-1)`, in that order. Walls are listed by cell number.

[`testdata/automaton-hpp`](../../testdata/automaton-hpp) is a complete example on a
3×3 HPP lattice. An east-moving particle starts in the center, cell 4, at bit
`4*4+0 = 16`. A north-moving particle starts in the south-west corner, cell 0, at
bit `0*4+1 = 1`. Together they are `0200010000`. One step later they are at bits
`5*4+0 = 20` and `3*4+1 = 13`, or `0020100000`. Its goal is that exact state, so
the search finds 1 forward step. With no collision each particle returns after 3
steps, so the return takes 2.

## Search

`pouch solve-automaton` takes the authority, digest, request ID, source manifest,
output directory and pinned tools of `pouch solve`, plus `--timeout` and
`--query-timeout`. It has no graph, AHE or `--max-paths` options, and adds the
budgets listed below. It writes `authority.json`, `sources.json`, `sources/`,
`solver/`, `bundle.json`, `index.html` and `manifest.json` to a new directory.

The policy is `shortest-run/synchronous-boolean/v2`. For each length from 0 to the
limit, one query asks for a run of exactly that length from the start to the goal.
The first SAT answer is the shortest run. It is also a first hit, because an
earlier hit would be a shorter run. A separate search then looks for the shortest
run from that endpoint to the baseline. The return uses the same rule, so it is
not the forward run reversed: on a reversible lattice it continues around the
cycle.

[Theory](../../docs/THEORY.md#lattice-gas-examples) shows recorded examples as
state diagrams. This differs from `simple-first-hit/action-sequence`. Choice-free systems have one
run, so `deterministic` is true and that run is the whole path set. With choices,
such as FHP collisions, `forward` is one shortest witness; other paths are not
enumerated. Its length is shortest because every shorter length ended in a checked
UNSAT. Tests compare these lengths with an exhaustive search on small FHP lattices.
Calx's `automaton.Query` has no way to block a found run, so listing every path
would first need a Calx change.

| Option | Meaning | Default |
|---|---|---:|
| `--max-queries` | Solver calls across both searches | 10,000 |
| `--max-variables` | Variables per formula | 100,000 |
| `--max-clauses` | Clauses per formula | 1,000,000 |
| `--max-literals` | Literals per formula | 4,000,000 |
| `--max-input-bytes` | Bytes of each of `input.cnf` and `input.json` | 16 MiB |
| `--max-orbit-steps` | Steps walked to confirm a choice-free run | 100,000 |

Raising these changes what can be attempted, not what a result means. Exceeding one
gives `INCONCLUSIVE`, never UNSAT. These budgets are separate from the 4,096-state
and 64-action limits of the finite format, which do not apply here. Queries for
every shorter length are repeated in full, so total cost grows with the square of
the limit: a 4×4 return of 206 steps took 218 queries and about 29 seconds.

## Certification

`bundle.json` has schema `pouch-automaton-bundle/v1`, with the search result under
`search` as `pouch-automaton-search/v1`. Its `status` is `FOUND`,
`NO_PATH_WITHIN_BOUND`, `NO_PATH_IN_MODEL`, `INCONCLUSIVE` or `REJECTED`. Forward
existence and `recovery_status` are separate.

Every returned run, including its choice bits, is replayed from the tables. For a
choice-free system,
`Certify` also walks the only run step by step, which checks the solver's UNSAT
answers by a separate method and rejects a false shortest or exhausted claim. It
keeps one saved state to detect a repeat, so it can follow runs far longer than
memory could list.

| `recovery_status` | `reason` | Meaning |
|---|---|---|
| `RETURN_VERIFIED` | | A replayed return exists; `return_steps` is its length. |
| `NO_RETURN_IN_MODEL` | | The run closed without reaching the baseline. |
| `UNKNOWN` | `RETURN_EXISTS_BEYOND_SEARCH_BOUND` | The walk reached the baseline after `return_steps`, past the return limit. |
| `UNKNOWN` | `ORBIT_NOT_CLOSED` | The walk stopped at its limit. A return may or may not exist. |
| `UNKNOWN` | `CHOICES_NOT_ENUMERATED` | No return was found within the limit, and other runs were not ruled out. |
| `UNKNOWN` | `RETURN_SEARCH_INCOMPLETE` | The return search did not finish. |

`complete` is true only when recovery is settled, and the exit code follows `solve`:
0 for complete, 1 for rejected, 2 otherwise. An `UNKNOWN` recovery therefore exits
with 2, even when the forward run is verified. Random lattices of 6×6 and larger
are usually in this state: each of 15 sampled states took more than 200,000 steps
to come back.

Certification also requires the query ledger to support each claim: verified
UNSAT at every shorter length, then a verified answer at the claimed length. With
choices no run can be walked, so this is what ties a shortest claim to the solver.

## Verification

```sh
pouch automaton-verify --authority authority.json --authority-sha256 "$AUTHORITY_SHA256" \
  --request-id "$REQUEST_ID" --bundle bundle.json
```

The command recomputes the certification of a saved bundle from the caller's
authority and digest, and accepts it only if every conclusion matches: status,
completeness, recovery, reason, display and diagnostics. It also rejects a bundle
bound to another authority, request or search limit. Exit code 0 means the saved
claims hold, 1 a rejected authority or bundle, and 2 a usage or I/O error. A bundle
whose recovery is `UNKNOWN` can still pass: the receipt then reports that result.

On success, stdout is a `pouch-automaton-receipt/v1` object with the digests and
conclusions. `walk_checked` is true when walking the only run confirmed the length
claims, and false when they rest on the recorded verified queries.

This does not rerun the solver or recheck the saved CNF, assignments or proofs, so
with choices a shortest or exhausted claim is only as good as its recorded queries.
A receipt is a conditional model conclusion, not approval to act. Bundles written
before `bits` and `display` existed do not pass, because they lack those fields.

## Report

`index.html` is an offline replay of the saved claims: forward and return runs,
step and play controls, and a count of set bits at each step, plus the choice bits
used at each step of a system with choices. Lattices with room
show each particle's direction; larger ones show density. It draws `hpp` with
north up and `fhp` as a sheared hexagonal grid, as `lga` defines them.
`pouch render --bundle bundle.json --out replay.html` writes it again from a saved
bundle. Neither checks the claims it displays.
