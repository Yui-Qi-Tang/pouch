# Automaton authorities

This package holds the `pouch-automaton/v1` authority for synchronous Boolean
automata, such as lattice gases. All cells update at once, from the previous
state of their inputs. This differs from `pouch-finite-validation/v1`, where one
declared action runs at each step. The two formats and their limits are separate.

The package validates authorities and replays recorded runs. It does not search:
it invokes no solver, and no `solve` command accepts this schema yet.

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

The whole file is at most 4 MiB and follows the same strict JSON rules as other
authorities: no nulls, unknown fields, missing fields or duplicate keys. The size
limit is reached before the cell limit: an HPP lattice of 256×256 takes 3.4 MiB.
The digest of its exact bytes is the caller's pin.

A state bit's address is `cell*bits+index`. A cell's `inputs` list distinct
addresses, and their count must equal the rule's `inputs`.

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
authority to stdout. A spec has the authority's fields, with `lattice` in place of
`model`:

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
