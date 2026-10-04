# AHE adapter fields and boundaries

This package is Pouch's concrete adapter for AHE's public MCP tools. Its Go types
are defined here; they are not imported from the ahe-mcp project. They contain
provider-specific IDs, profiles and response envelopes. They are not a generic
Pouch source interface.

The current `ahe-read` command prints `ahe.Snapshot` directly as JSON. `solve`
instead reads local authority/source files; it does not automatically convert
this snapshot into an operation model. See [CLI outputs](../cli/README.md).

## Connection and capabilities

| Type / field | Meaning / values |
|---|---|
| `Command.Path` | Absolute executable path for the local MCP launcher; no shell. Local configuration, not portable evidence. |
| `Command.Args` | Individual arguments passed without shell expansion. |
| `Command.Env` | Process environment entries. Nil inherits the environment; an explicitly empty non-nil slice supplies an empty environment. |
| `Tool.Name` | Discovered MCP tool name. Discovery is not call authorization. |
| `Tool.InputSchema` | Exact input-schema JSON bytes used by `SchemaSHA256()`. Do not hash a reformatted copy. |
| `Tool.Annotations.ReadOnly` | Server-declared `readOnlyHint`; must be true for pinned read tools. |
| `Client` | Owns one serialized stdio connection, process lifecycle and request IDs; not a JSON result. Cancellation closes the connection. No automatic retry. |
| `Profile` | A private binding of one client, profile kind and caller-pinned tool schemas. Construct through `Client.Bind`, not an unchecked struct. |

`ProfileKind` has these existing constants/values:

| Constant | Value | Tool allowlist |
|---|---|---|
| `ProfileQuery` | `query` | `open_canonical_read_view`, `get_evidence_record` |
| `ProfileIntake` | `intake` | `submit_external_source`, `submit_extractor_output` |
| `ProfileEndpointReviewer` | `endpoint-reviewer` | `get_endpoint_review`, `admit_reviewed_endpoint` |
| `ProfileSourceClaimReviewer` | `source-claim-reviewer` | `get_source_claim_review`, `admit_reviewed_source_claim` |

Binding also requires a nonempty caller-provided tool-name → schema SHA-256 map.
The allowlist alone grants no calls. `Start` uses MCP version `2025-06-18`;
discovery is bounded at 32 pages and incoming messages at 16 MiB.

## Read request, view and snapshot

| Field | Meaning / values |
|---|---|
| `ReadRequest.RootNodeIDs` | 1–32 distinct IDs starting `canon-node:`, at most 200 bytes each. |
| `ReadRequest.Relations` | Provider relation labels. A nonempty list must match the response scope; empty leaves selection to the provider. No inference of action guards. |
| `ReadRequest.MaxDepth` | Inclusive requested traversal bound, 0–8. |
| `ReadRequest.MaxNodes` | Requested node cap, 1–1,024. |
| `ReadRequest.MaxEdges` | Requested edge cap, 0–4,096; must be positive when MaxDepth is positive. |
| `View.Handle` | Provider read-view handle. A handle alone is not materialized evidence. |
| `View.SnapshotID` | Identity also required in the returned graph. |
| `View.RootNodeIDs`, `Relations`, `MaxDepth`, `MaxNodes`, `MaxEdges` | Actual view scope; checked against the request as above. |
| `View.NodeCount` / `EdgeCount` | Counts checked against the embedded graph and requested caps. |
| `View.Truncated` | Required `*bool`. Nil is rejected. True records truncation; false describes the requested view, not global completeness. A truncated view can be returned and must retain this flag. |
| `View.GlobalAbsenceInferenceAllowed` | Required `*bool`, must be false. Nil or true is rejected. No global absence claim from this bounded view. |
| `Snapshot.SchemaVersion` | Wrapper schema `canonical-read-view-query-v1`; distinct from the embedded `canonical-evidence-graph/v1` graph schema. |
| `Snapshot.View` | Checked view descriptor. |
| `Snapshot.Artifact` | Embedded graph JSON for convenient structured consumption. |
| `Snapshot.ArtifactBytes` | Exact embedded graph JSON bytes, emitted as `artifact_bytes_base64` to preserve byte identity. |
| `Snapshot.RawResponse` | Exact tool `structuredContent` JSON bytes, emitted as `raw_response_base64`; not the entire stdio/JSON-RPC transcript. |
| `Snapshot.ArtifactSHA256` | SHA-256 of ArtifactBytes. |
| `Snapshot.ResponseSHA256` | SHA-256 of RawResponse. |
| `Snapshot.ProjectionBytes` / `ProjectionSHA256` | Explicit matrix-input bytes and their exact digest; original artifact identity remains separate. |
| `Snapshot.Normalization` | `none`, or `ahe-depth-zero-null-edges/v1` for a complete depth-zero view with zero requested/observed edges. |

Only explicit `edges:null` in that checked complete zero-edge scope is converted
to `[]`. Missing edges, null nodes, duplicate fields and malformed topology are
rejected. Truncation or a nonzero edge/depth scope does not authorize conversion.
The generic graph decoder remains strict. Retain both byte representations;
never label the projection digest as the original AHE artifact digest.

All adapter-generated SHA-256 strings above are lowercase hex. JSON formatting
can change byte hashes, so the base64 copies matter. Identity checks do not
establish truth or a globally current provider snapshot.

`ReadRecord` returns a `Record`: `CanonicalID` is the exact admitted node ID,
`Statement` is its nonempty `statement_text`, `StatementSHA256` hashes that
statement's UTF-8 bytes only, and `Raw` preserves the structured record response.
The statement digest does not cover all native provenance or source-document
bytes. `Prepare` compares this statement digest with the selected authority
source binding; the caller must use the same hash scope.

## Prepared results, review and publication

| Type / field | Meaning / values |
|---|---|
| `Prepared` | Private validated model receipt, freshly read source records and sorted parent IDs. Created by `Prepare`; conveys no admission approval. |
| `Prepared.Receipt()` / `Statement()` / `Sources()` | Local validation receipt, its exact JSON statement, and copied native source records. |
| `Derivation.ParentNodeIDs` | Exact sorted AND basis of selected sources; not alternative parents. |
| `Derivation.Method` | Fixed explanation of conditional finite-model validation and its limits. |
| `Derivation.Producer` / `TraceRef` | `pouch-finite-validation/v1` and `sha256:<package digest>`. |
| `ReviewRequest.Kind` | `derived_spec` for this adapter flow. |
| `ReviewRequest.ProposalOccurrenceID` | Exact pending `occ:` ID, at most 200 bytes. |
| `ReviewRequest.Derivation` | The declared parent set and validation method. |
| `Review.Subject` | Native identity of the exact review. |
| `Review.DisplaySHA256` | Digest of the exact native display bytes, not the full response. |
| `Review.PackageSHA256` | Digest of the locally validated package bytes. |
| `Review.Raw` | Native review structured response. Private fields additionally bind the display, request and Prepared instance. |
| `Approval.RequestID` | Explicit admission request ID, nonblank, at most 200 bytes; distinct from the planning request ID. |
| `Approval.Subject`, `DisplaySHA256`, `PackageSHA256` | Must match the externally inspected unchanged review and package. |
| `Approval.Reason` | External approval reason, nonblank, at most 2,000 bytes. Pouch does not generate approval from SAT success. |
| `Pending.ProposalID` | Exact proposal occurrence ID once intake succeeds; empty before a valid proposal receipt. |
| `Pending.SourceReceipt` / `ProposalReceipt` | Raw responses from the two successive intake writes. A source may exist even if proposal creation fails. |
| `Publication.Validation` | Original local model receipt, separate from native admission. |
| `Publication.Admission` | Raw admission response once received; it can be populated before receipt validation succeeds. |
| `Publication.CanonicalID` | Native `canon-node:` ID after admission receipt validation. |
| `Publication.Readback` | Fresh native record response. May be populated before final binding validation; consume with the returned error. |

`Submit` takes an explicit nonzero observation time and an intake request ID
(nonempty, at most 180 bytes). The timestamp records submission of the model
statement, not observation of a real repair. Intake creates source/proposal
records; `Admit` separately requires the exact review and external approval.

`ErrCapabilityUnavailable` (`CAPABILITY_UNAVAILABLE`) means the required
profile, tool, schema pin or capability is unavailable. `ErrDeliveryUnknown`
(`DELIVERY_UNKNOWN`) means a write may have occurred without a trustworthy
acknowledgement; reconcile instead of retrying blindly. `ToolError.Payload`
preserves a provider-reported tool refusal. Other errors remain ordinary Go
errors. Publication status labels are owned by the CLI wrapper, not by
`Publication`; see [the status table](../cli/README.md).

Implementation: [client.go](client.go), [read.go](read.go), [publish.go](publish.go).
Protocol workflow: [ahe-adapter.md](../../docs/ahe-adapter.md).
