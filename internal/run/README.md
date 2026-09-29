# Run inputs, bundle fields and validation states

`run` binds local source files, invokes original-rule validation, and saves
portable artifacts. `planning.Result` is search output; `run.Bundle` records
the additional validation. Their `Status` and `Complete` fields are distinct.
This page documents current values, not new enum enforcement or a new API.

## Source inputs

| Type / field | Meaning / values |
|---|---|
| `Source.ID` | Source ID selected by the caller-owned authority. |
| `Source.File` | Confined relative path from the source manifest directory. No absolute paths, traversal or escaping symlinks. |
| `Source.SHA256` | Exact selected content bytes, 64 lowercase hex characters. Must match the authority. |
| `SourceManifest.SchemaVersion` | `pouch-sources/v1`. |
| `SourceManifest.AuthoritySHA256` / `RequestID` | Must match independently supplied caller values; the manifest cannot choose its own trust anchor. |
| `SourceManifest.Sources` | Exactly the authority's selected source set; no duplicates or missing entries. |
| `Inputs.Authority` | Decoded, validated authority used by the original-rule evaluator. |
| `Inputs.AuthorityRaw` | Original authority bytes retained for identity and copying. |
| `Inputs.Sources` | Validated manifest. |
| `Inputs.Content` | Source ID → checked raw content bytes. |

`ReadInputs` reads files. `SaveInputs` copies them into a fresh run and updates
the portable file references. These are concrete file functions, not a common
source-provider interface.

## Optional pinned graph input

`AttachEvidence` preserves the exact graph bytes and projects its topology plus
sidecars. In `solve`, this check completes before any solver is initialized or
invoked. The graph-byte hash is distinct from each selected statement hash.
Every authority source ID must occur as a graph node; either encoding may bind it:

- **Inline:** `content_sha256` is a JSON string equal to the caller's source
  digest. If `content` is present, its decoded UTF-8 bytes must hash to the same
  digest. A digest-only node relies on the separately checked source file.
- **Referenced claim:** `kind` must be exactly `source_claim`, with a nonempty
  string `payload_ref` resolving into the root `payloads` array. Payload IDs are
  unique nonempty strings throughout that table. The referenced `claim` must be
  a nonempty JSON string whose decoded UTF-8 bytes hash to the caller's digest.
  This is the native AHE graph's representation of `ReadRecord.statement_text`.

`source_claim` or the presence of `payload_ref` selects the second form; a broken
reference cannot fall back to an inline digest. If inline content/digest is also
provided, all supplied bindings must agree. Missing fields, null/wrong types,
duplicate payload IDs and unpaired Unicode surrogate escapes are rejected.
Text is not trimmed or Unicode-normalized; newlines are significant. `span`,
serialized JSON, and intake raw-content hashes are not substitutes for `claim`.

This contract is implemented in `run`, without importing the AHE adapter into
planning or projection. It checks selected statement identity, not source truth,
lifecycle freshness, complete graph coverage or source-to-rule correctness.
Unselected claim text and other provider metadata remain opaque. No supplied
graph generates operation rules. Without a graph, a selected-source-record view
is generated explicitly with unknown topology.

## Bundle fields

| Field | Meaning / values |
|---|---|
| `SchemaVersion` | `pouch-bundle/v1`. |
| `ProjectVersion` | Producer version, currently `0.1.0-dev`; not a schema version. |
| `RequestID` / `AuthoritySHA256` | Original request and exact authority-byte binding. |
| `Status` | Bundle-level outcome, below; not the nested Search status. |
| `Complete` | Search completed, independent forward set comparison completed/matched, and every retained path received its recovery certificate. It does not require every path to be recoverable: a validated no-return conclusion can also complete. |
| `ConclusionKind` | `model_conditional_conclusion`. |
| `ObservedFact` / `NativeAHEReceipt` | Both false for locally generated bundles. AHE publication has a separate receipt; it does not turn this model statement into an observed fact. |
| `Contract` / `Sources` | Original model/query contract and portable source manifest. |
| `EvidenceMatrix` | Optional evidence-graph projection, distinct from `Search.Matrix`, which encodes operations. Nil/omitted before evidence attachment. |
| `EvidenceScope` | Descriptive provenance text, not a machine status enum. Either a selected-record-only view or an explicitly pinned graph with scope metadata. |
| `Search` | The [planning result](../planning/README.md), including its own status and completion flags. |
| `SetCheck` | Independent original-rule DFS comparison of the bounded forward action-sequence set; described below. |
| `Paths` | Per-path original-rule checks and recovery conclusions. Can be partial on interruption. |
| `Diagnostics` | Diagnostic codes accumulated during certification. Empty does not independently establish success. |

| `Bundle.Status` | Meaning |
|---|---|
| `FOUND` | Forward paths exist and reached this stage of certification. `Complete` can still be false; inspect per-path results and the returned error. |
| `NO_PATH_WITHIN_BOUND` | Independent bounded set comparison confirms no submitted forward paths, but original-rule full reachability finds a route outside the bound. |
| `NO_PATH_IN_MODEL` | Bounded enumeration/set comparison and full original-rule reachable closure establish no forward goal in this finite model. Not real-world impossibility. |
| `INCONCLUSIVE` | Initial outcome or an interruption that prevents a stronger conclusion. |
| `REJECTED` | Detected invalid path, binding, shortest claim, duplicate/extra path, or a claimed complete set that disagrees with the reference. |

Examples: `Search.Status=INCONCLUSIVE` and `Bundle.Status=FOUND` can coexist if
some forward paths survive but later work failed. `Bundle.Complete=false` keeps
that result incomplete. `Search.Status=NO_PATH_WITHIN_BOUND` can become bundle
`NO_PATH_IN_MODEL` only through the separate original-rule closure check.
Always consume `(Bundle, error)` together; an error can leave intermediate
fields populated without a final receipt.

## Path results and recovery

| `PathResult` field | Meaning / values |
|---|---|
| `ID` | Correlates with `Search.Forward[].ID`. |
| `ForwardVerified` | Original-rule replay and any claimed shortest forward cost passed. It says nothing by itself about recovery. |
| `RecoveryStatus` | `RETURN_VERIFIED`, `NO_RETURN_IN_MODEL`, or `UNKNOWN`. |
| `Return` | Separately searched recovery trace when present, otherwise nil. A zero-action trace differs from nil. |
| `PackageFile` | Relative certificate file, set when written after validation. Empty/omitted if none was saved. |
| `Receipt` | Successful original-rule `validation.Receipt`; nil until validation and package writing succeed. A recovery label alone is not a receipt. |
| `ClosureFile` | Relative full-closure evidence for a no-return conclusion; empty/omitted otherwise. |
| `Reason` | Explanation for unknown recovery; values below. |

`RETURN_VERIFIED` is the return-witness branch, finalized by the receiver's
successful receipt. `NO_RETURN_IN_MODEL` is the full finite closure branch,
also checked by the receiver. `UNKNOWN` leaves recovery unresolved. On error,
a branch label may already be assigned; do not promote it to an accepted
conclusion without the receipt.

| `Reason` | Meaning |
|---|---|
| `RETURN_EXISTS_BEYOND_SEARCH_BOUND` | The bounded SAT return search missed, but the original-rule evaluator found a longer route. It is not substituted into the requested query. |
| `RETURN_SEARCH_INCOMPLETE` | The referenced return entry did not supply a conclusive recognized result. |
| `RETURN_SEARCH_NOT_COMPLETED` | No valid return entry index was available for the path. |
| Empty / omitted | No unknown-recovery explanation recorded; not proof of acceptance. |

`Diagnostics` currently uses `REFERENCE_SET_INCOMPLETE`,
`PATH_SET_MISMATCH`, `REFERENCE_REACHABILITY_INCOMPLETE`, and
`RUN_NOT_FULLY_VERIFIED`. Respectively these mean unfinished independent DFS,
disagreement with a claimed complete set, unfinished original-rule reachability,
and a bundle that did not reach complete certification.

`SetCheck.Complete` records finished reference enumeration; `Matches` records
set equality. `ReferencePaths` and `SubmittedPaths` count paths;
`Missing`, `Extra`, and `Duplicates` distinguish the mismatch kinds. `Visits`
counts DFS visits; `StopReason` retains interruption diagnostics. Counts from
an incomplete comparison are not a completeness certificate. See
[inspection.go](../validation/inspection.go).

Receipt costs are action counts. `ForwardShortest` and `ReturnShortest` come
from original-rule reachability, not from sender claims. In a successful
`no_return` receipt, `ReturnCost=ReturnShortest=-1`; zero is a real zero-step
cost. `ReachableStates` counts the closure from the forward endpoint, including
that endpoint. Receipts retain the exact authority/package hashes and false
observed-fact/native-receipt flags. See [validation.go](../validation/validation.go).

Implementation: [input.go](input.go), [bundle.go](bundle.go),
[artifacts.go](artifacts.go). Portable layout: [bundle-format.md](../../docs/bundle-format.md).
