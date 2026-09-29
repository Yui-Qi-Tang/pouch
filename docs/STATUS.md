# Project status — 2026-09-30

Version: `0.1.0-dev`, unreleased.

## Available capabilities

- Provider-independent source files and caller-owned model/query authority.
- Evidence graph ↔ matrix projection, with explicit selected-source content binding.
- Declared operation matrices → CNF → bounded SAT forward-path enumeration.
- Original-rule replay, independent path-set comparison, and separate recovery search or finite-model no-return checks.
- Shared JSON results and offline HTML replay showing sources, assumptions, state changes and model-operation costs.
- Optional AHE public stdio MCP adapter for read, local validation, pending submission, exact review, approved admission and fresh readback.
- A fixed Django example adapter that applies/reverses known edits on disposable copies of three source files.

The current executable model supports categorical string fields, AND guards,
constant effects, explicit frames and unit costs, up to 4,096 Cartesian states.
Evidence relationships do not automatically become operation rules.

## Verification scope

Local verification on 2026-09-30 passed Go build, vet, uncached race
tests, pinned real SAT/checker tests, and fresh isolated AHE MCP integration.
Both key-rotation and Django observation/staging models were tested with the
original native graph passed directly to `solve --graph`, followed by model
validation, result admission and fresh readback. Approvals were explicit test
stubs in a disposable database, not production approvals.

A separate integration revalidated and persisted the existing recorded three-file
repair/reverse execution. It did not perform new repairs or official SWE tests.
The recorded file-copy restoration is distinct from the staging model's
`clear_staging` operation, which only resets model observations. The historical
Django official-test control mismatch remains recorded; file restoration does
not establish complete runtime repair correctness.

A separate fresh integration on 2026-09-30 used SWE-bench Verified
`django__django-14089` with its known official patch. Nine solver queries and
forward/return replay passed; the actual 44-test Django module showed the
expected failing regression before the patch, all tests passing after it, and
the expected failure after restoration. All 6,440 baseline file hashes and modes
were restored. The model result and separate execution report were admitted and
freshly read back through AHE MCP in an isolated database. This used a local
Python environment, not the official SWE Docker evaluation harness; no new patch
synthesis or general native execution validator is claimed. The execution report
references the model hashes but has no native derived edge joining it to the
model result.

HTML generation and automated content checks passed. Actual browser interaction
and visual acceptance remain unverified. This Go acceptance covers selected
models; it is not acceptance of all twenty historical Lab cases.

## Limits and next work

Source binding establishes selected byte identity, not source truth, complete
original-problem coverage or atomic lifecycle/currentness. Model recovery does
not guarantee physical rollback, external side-effect restoration or technical-debt
cost. Bounded absence and complete finite-model no-return remain distinct.

Remaining product work includes browser/display acceptance, a reusable execution
adapter beyond the fixed example, and explicitly selected additional model ports.
Auto mode, arbitrary patch synthesis, path merging and protobuf are deferred.

## Documentation and local evidence

The repository keeps product documentation, source code and small regression
fixtures. Detailed run reports, review records, manifests and generated output
remain local and ignored; see [output and archive handling](artifacts.md).
Earlier failed or partial runs are retained without changing their outcomes.
