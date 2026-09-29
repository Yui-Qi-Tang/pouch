# CLI outputs and status values

`Run(ctx, args, stdout, stderr) int` returns a process exit code. It does not
return an AHE Go struct. JSON is written to stdout; errors normally go to
stderr. The current handlers also construct concrete dependencies and arrange
workflows. They have not yet been reduced to a provider-neutral service layer.

For `ahe-read`, the current data flow is:

```text
CLI flags/config → ahe.ReadSnapshot → ahe.Snapshot → JSON on stdout
```

This output is the adapter-defined structure. `solve` uses a separate local
authority/source-manifest input contract. There is no automatic source-to-rule
conversion between the two commands.

## Output by command

| Command | Successful stdout | Meaning |
|---|---|---|
| `version` | Plain `pouch 0.1.0-dev` text | Executable version. |
| `solve` | `{status, complete, paths, queries, bundle}` | `status`/`complete` come from [run.Bundle](../run/README.md); paths and queries are counts, and bundle is the relative `bundle.json` filename. This is not the full result. |
| `project` | `projection.Matrix` | Evidence incidence projection; see [projection-format.md](../../docs/projection-format.md). |
| `verify` | `validation.Receipt` | Successful original-rule model-package validation. No `status` field is added to this receipt. |
| `render` | `{status: "RENDERED", verification: "saved claims only; no re-verification"}` | HTML was rendered. No renewed proof/receipt validation. |
| `ahe-discover` | List of `{name, input_schema_sha256, input_schema}` | Discovered tool schemas, no tool-call permission granted. |
| `ahe-read` | [ahe.Snapshot](../ahe/README.md) | Direct JSON encoding of the adapter snapshot, including exact-byte base64 fields. |
| `ahe-stage` | `{status, pending, review}` | Pending source/proposal writes and exact native review; not admission. |
| `ahe-admit` | `{status, publication}` | Admission/readback outcome. Local validation and native receipts remain separate. |

After authority loading, a semantic package refusal from `verify` emits
`{status: "REJECTED", reason: <validation rejection code>}` and exits 1.
Authority-load rejection exits 1 with stderr, without that JSON envelope.

## AHE wrapper statuses

| Command / status | Meaning |
|---|---|
| stage: `AWAITING_EXTERNAL_APPROVAL` | Pending submission and review succeeded. External approval is still required. |
| stage: `RECONCILE_PENDING_SUBMISSION` | Submit returned an error; retain any pending receipts and reconcile the actual writes. This label alone does not prove a write occurred. |
| stage: `REVIEW_INCOMPLETE` | Submission succeeded but the review call/check failed. |
| admit: `ADMITTED_AND_READ_BACK` | Admission receipt and fresh readback both passed their checks. |
| admit: `DELIVERY_UNKNOWN` | Returned error wraps `ahe.ErrDeliveryUnknown`; a trustworthy write outcome is unavailable. |
| admit: `ADMISSION_RECEIVED_READBACK_INCOMPLETE` | An admission response was retained, but the remaining admission/readback flow failed without ErrDeliveryUnknown. |
| admit: `NOT_ADMITTED` | No admission response was retained and no delivery-unknown error was reported; includes pre-write rejection. |

Errors before these branches may produce only stderr, not a status envelope.
Do not interpret an absent stdout field as success or automatically retry a write.

## Exit codes and configuration

`0` means the command finished its own task; `ahe-stage` may still be awaiting
approval, and `render` may merely have displayed saved claims. `1` is used by
`verify` for authority/model-package rejection and by `solve` for a rejected
certification result. `2` covers usage, operational errors and incomplete runs.
Currently `project` and AHE handlers map **all** returned errors to 2, including
semantic refusals. Thus exit codes are command-specific, not a universal
substitute for the result/status/receipt fields.

The local AHE connection config contains `query`, `intake`, and `endpoints`
profiles. Each has a `command` ([ahe.Command](../ahe/README.md)) and `pins`, a
tool-name → exact input-schema SHA-256 map. A read needs only query; stage also
uses intake/endpoints; admit uses endpoints/query. `ahe-discover` instead takes
the Command object directly. Credentials and local launcher paths are not
portable result metadata.

Search length bounds live in the authority. CLI `--max-paths` (default 1,000)
and `--max-queries` (10,000) are positive resource budgets. `solve --timeout`
defaults to 2 minutes for search/certification, `--query-timeout` to 10 seconds
per external tool execution. AHE `--timeout` defaults to 1 minute. Duration flags
use Go duration strings, e.g. `10s`; none of these limits proves no solution.

Implementation: [cli.go](cli.go), [ahe.go](ahe.go).
