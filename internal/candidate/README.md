# Finite candidate spaces

`candidate-prepare` compiles caller-declared options and forbidden partial
assignments into the existing finite validation contract. `solve` performs the
matrix/CNF/SAT search. `candidate-materialize` independently replays a returned
certificate before rendering text. This package has no AHE dependency.

This package implements finite choice models and patch materialization.
The problem-specific Python interpreter that derives constraints from regex
requirements remains an external semantic producer. Pouch does not infer the
meaning or completeness of requirements, invent edits, or translate arbitrary
source programs into constraints.

## Input: `pouch-candidate-space/v1`

Every field is required, with exact spelling. Unknown fields, duplicate keys,
nulls, type coercions and missing fields are rejected. The entire UTF-8 JSON
file is pinned by an externally supplied lowercase SHA-256, before decoding.

| Field | Meaning |
|---|---|
| `schema_version` | Exactly `pouch-candidate-space/v1`. |
| `request_id` | Caller-selected problem identity. |
| `source_binding` | 1–8 evidence IDs mapped to exact source digests; follows the existing authority convention. |
| `assumptions` | Explicit modeling assumptions. `candidate_space_sha256` is reserved for the compiler. |
| `groups` | Ordered choice groups. Each has `id` and `options`; each option has `id` and `text` mapping target file paths to literal text. |
| `constraints` | Forbidden partial assignments: `id`, `when` (group → option ID), `evidence` (nonempty source-ID list). IDs are provenance, not entailment proofs. An empty list permits all combinations. |
| `files` | Target `path`, exact baseline `sha256`, and full-file `template`. |

Group/option/constraint IDs use `[a-z][a-z0-9_]{0,39}`; `phase` and `unset` are
reserved where applicable. Templates use literal `{{pouch:group_id}}` tokens.
Every option of a referenced group must supply text for that file. Values are
inserted literally, without shell or template execution, and cannot contain
another Pouch token. Files must already exist as regular UTF-8 files; creation,
deletion, renaming, binary files and symlink targets are unsupported. File modes
are retained, not edited. Paths are confined relative paths with portable ASCII
letters, digits, `_`, `-`, `.`, `/`; `.git` is prohibited.

Limits: 4 MiB input, 1–8 groups, 1–16 options per group, 1–32 target files,
0–256 constraints. The existing model limits (4,096 Cartesian states, 64 actions)
also apply; the smaller effective bound wins.

The compiler produces a canonical group-selection order, then `submit_candidate`.
A forbidden assignment disables submission. Withdrawal in reverse group order
returns to the unset baseline. These are **selection-state operations**. Their
cost is not patch execution time, technical debt or physical restoration cost.
The space digest binds constraints, source references and renderer text into the
authority. Changing any of them invalidates old packages.

## Commands

```sh
pouch candidate-prepare --space space.json --sha256 "$SPACE_SHA" > authority.json
# Construct the ordinary source manifest, then run `pouch solve` with pinned tools.
pouch candidate-materialize --space space.json --sha256 "$SPACE_SHA" \
  --package result/packages/p000000.json --root baseline --out candidate
```

The package path above is illustrative; use the exact `package_file` in the
actual bundle. Materialization reads the baseline and writes only a **new**
output directory: rendered `files/`, `candidate.patch`, `materialization.json`.
It never applies or executes the patch. The patch uses deterministic full-file
hunks, so textual equality with an official minimal diff is not expected.
The materialization record permanently retains `runtime_status=NOT_RUN`: it
records what this command did. Later external execution is represented by a
separate runtime-evidence record and binding; it does not rewrite this artifact.
A model return certificate is not evidence that a real checkout was restored.

## Recorded runtime binding

After running a pinned external test/restore workflow:

```sh
pouch candidate-bind-runtime --space space.json --sha256 "$SPACE_SHA" \
  --package result/packages/p000000.json --root runtime-evidence \
  --materialization candidate/materialization.json \
  --evidence runtime-evidence/evidence.json --evidence-sha256 "$EVIDENCE_SHA"
```

`pouch-runtime-evidence/v1` is an independently pinned observation record with:

- `materialization_sha256`, `package_sha256`, `runner_sha256`, `parser_sha256`;
- `expected_tests` (nonempty unique IDs), `patch_file`, and `artifacts` (relative
  filenames → digests, including patch, logs, runner and parser files);
- `baseline`, `patched`, `restored`, each containing `tests` (ID → status),
  `files` (path → `{sha256, mode}`), and `log_files` (bound artifact names).

Opaque map keys such as source IDs and test IDs retain exact case and Unicode;
do not case-fold, normalize or merge them. Structural schema field names remain
strictly checked.

Required patched tests must be `PASSED`; each required baseline test must be
`PASSED`, `FAILED`, or `ERROR`. Full supplied baseline/restored status maps and
file maps must match. Patched and baseline file scopes must match; target file
hashes must match the materialization. Missing observations fail closed.

The output status `RECORDED_CHECKS_PASS` means record consistency and exact-byte
binding only. `baseline_discriminating=false` explicitly identifies an already
passing baseline. The supplied observation scope may omit untracked files,
external state, and tests not listed; no claims are made about those. The checker
does not authenticate logs, prove the parser correct, or rerun tests. A dishonest
producer can fabricate a self-consistent record. Keep original logs and runner
provenance available for review. For AHE, stage this record as separately reviewed
evidence and preserve the existing source/model/result authority boundaries;
this command grants no admission or automatic approval.
