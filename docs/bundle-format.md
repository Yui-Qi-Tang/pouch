# Local inputs and result bundles

`authority.json` is the preserved strict `pouch-finite-validation/v1` contract. A trusted caller independently supplies its exact byte SHA-256 and request ID. It contains finite fields/actions, start, Goal, full baseline, search bounds, assumptions and selected source bindings. It is not inferred from evidence graph edges.

The local source manifest has this separate schema:

```json
{
  "schema_version": "pouch-sources/v1",
  "authority_sha256": "<caller-held authority digest>",
  "request_id": "<same caller-selected request>",
  "sources": [
    {"id": "<authority source ID>", "file": "sources/source.txt", "sha256": "<exact content digest>"}
  ]
}
```

Source IDs/digests must exactly match the authority. Paths are relative to the manifest directory and are confined with `os.OpenRoot`; absolute paths, traversal, escaping symlinks, duplicate IDs, missing/unknown fields and altered bytes are rejected. The portable run copies the original authority and exact selected source bytes.

Optional canonical graphs are separately pinned. Selected node IDs must have explicit `content_sha256` attributes matching the authority. AHE graph snapshots can be projected independently; supplemental source record bindings must be mapped explicitly before a graph becomes a planning display. Without a graph, the generated evidence view has selected record nodes and no inferred edges, with absence marked unknown.

## Shared result

`pouch-bundle/v1` contains the original contract/source manifest, evidence and action matrices, bounded search ledger, forward paths, separate return searches, original-rule set check, per-path receipts, and diagnostic status. `index.html` embeds the same JSON data; its export reproduces that data. Rendering a supplied bundle is display only, not authentication or renewed verification.

Path identity is the sequence of declared action IDs. Search enumerates simple first-hit paths within `forward_limit`. For each distinct endpoint it searches for one shortest return within `return_limit`, separately from the forward query. The original evaluator independently checks the complete bounded forward set and each trace.

Recovery is `RETURN_VERIFIED`, `NO_RETURN_IN_MODEL`, or `UNKNOWN`. A bounded return miss alone cannot establish `NO_RETURN_IN_MODEL`: this requires a complete original-rule reachable closure, checked again by the v1 receiver. If a return exists beyond the supplied bound, recovery remains `UNKNOWN` with `RETURN_EXISTS_BEYOND_SEARCH_BOUND`; it is not silently substituted by a different query. Conditional receipts always retain `observed_fact=false` and `native_ahe_receipt=false`.

`FOUND` means a path exists, while `complete` separately records whether the whole requested bounded enumeration and all recovery checks finished. `NO_PATH_WITHIN_BOUND` differs from `NO_PATH_IN_MODEL`. Timeouts, solver/checker failures, reference traversal limits and interrupted work retain incomplete status. Default independent DFS budgets are 10,000 paths and 1,000,000 visited prefixes; a successful SAT enumeration cannot override an incomplete independent comparison.

## Files and hashes

- `authority.json`, `sources.json`, `sources/`: frozen query inputs.
- `evidence-graph.json`, `evidence-matrix.json`: explicitly scoped graph view and incidence projection.
- `solver/tools/`, `solver/tools.json`, `solver/q*/`: pinned tool bytes, formulas, maps, assignments/proofs and process records.
- `packages/`: v1 packages accepted by the original-rule receiver.
- `closures/` or `forward-closure.json`: full finite reachable states when used to justify impossibility.
- `bundle.json`, `index.html`: machine and human views.
- `manifest.json`: exact SHA-256 and byte length of every other saved file; it does not hash itself.

Output directories must be new and files are not overwritten. Hash identity does not constitute semantic approval. The producer's manifest is inventory, not independent trust. Runtime error text is retained locally; portable summaries should not include launch credentials, host paths or private provider configuration.

## Explicit optional MCP commands

`pouch ahe-discover --config query-command.json` only initializes/discovers tools. The file is an `ahe.Command` object with `path`, `args`, and optional `env`. Review the returned tool schemas and pin them in a separate local configuration before allowing calls.

`pouch ahe-read --config profiles.json --request bounded-read.json` performs a bounded Query read. `profiles.json` contains `query`, `intake`, and `endpoints` objects, each with a `command` and a `pins` map of allowed tool name to exact input-schema SHA-256. A read only needs `query`. Credentials stay in private launcher files; these local configurations are not copied into bundles. The returned snapshot includes base64 exact bytes and hashes alongside convenient structured JSON.

`pouch ahe-stage` additionally requires `--authority`, `--authority-sha256`, `--request-id`, `--package`, `--intake-request-id`, and an explicit `--observed-at` RFC3339 timestamp. It validates the package and freshly queries source bindings before creating a pending proposal. It returns the exact native review for external inspection with `AWAITING_EXTERNAL_APPROVAL`.

`pouch ahe-admit` uses the same authority/package flags, `--proposal-id`, and `--approval approval.json`. Approval supplies `request_id`, `subject`, `display_sha256`, `package_sha256` and `reason`, all tied to the inspected review. The command freshly validates and reviews again; changed bindings are rejected. This is an explicit write command, never part of `solve` or `render`.

Admission outcomes distinguish `NOT_ADMITTED`, `DELIVERY_UNKNOWN`, `ADMISSION_RECEIVED_READBACK_INCOMPLETE` and `ADMITTED_AND_READ_BACK`. A partial or uncertain write must be reconciled before another write; no automatic retry is provided. These commands provide file/JSON coordination, not an interactive approval UI. Native acceptance exercises the same Go adapter APIs; it is not a separate end-to-end CLI approval-interface acceptance.
