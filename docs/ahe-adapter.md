# Optional AHE public-MCP adapter

The `internal/ahe` package connects Pouch to an independently operated AHE MCP
server over standard newline-delimited stdio JSON-RPC. It imports no AHE code,
uses no database API, and requires no Pouch-specific AHE tool. Its current
protocol version is `2025-06-18`. Files and caller-declared authorities remain
valid standalone inputs when AHE is unavailable.

## Connection and capability boundary

`Start` initializes a caller-selected absolute executable and discovers tools.
Discovery alone permits no tool calls. `Client.Bind` requires externally supplied
SHA-256 pins of the exact advertised `inputSchema` bytes. The operator must review
and pin discovery independently of any returned Pouch package. The native test
fixture freezes its known build and discovery before admitting fixture sources;
that fixture process is not a substitute for operator trust configuration.

Profiles have separate allowlists and can use separately authorized subprocesses:

| Profile | Public tools |
|---|---|
| Query | `get_evidence_record`, `open_canonical_read_view` |
| Intake | `submit_external_source`, `submit_extractor_output` |
| Source claim reviewer | `get_source_claim_review`, `admit_reviewed_source_claim` (fixture ingestion) |
| Endpoint reviewer | `get_endpoint_review`, `admit_reviewed_endpoint` |

Missing tools, mismatched schema pins, or calls outside the selected profile return
`CAPABILITY_UNAVAILABLE`. There is no direct SQL or legacy writer fallback.
Database roles remain the server's authorization boundary; client allowlists do
not replace them. Credentials belong in protected native launcher configuration.

Requests are serialized, bounded, and never automatically retried. A lost,
cancelled, malformed, or protocol-error write reply returns `DELIVERY_UNKNOWN`.
The caller must reconcile the exact request and any retained receipt before making
another write. A native tool error is retained as a structured error, not treated
as an admission receipt. A readback failure after admission retains the admission
receipt and does not automatically resubmit it.

## Read and hash semantics

`ReadSnapshot` materializes a bounded canonical read view, checks its scope,
counts and snapshot identity, and retains the native `canonical-evidence-graph/v1`
artifact. The native artifact is directly accepted by Pouch's projection decoder;
no synthetic snapshot ID or silent field remapping is added.

`Snapshot.Artifact` is a structured convenience view. `ArtifactBytes` and
`RawResponse` are byte copies exported as base64 so JSON pretty printing cannot
change the bytes identified by `ArtifactSHA256` and `ResponseSHA256`. A truncated
view stays explicitly truncated. Even a complete bounded view does not prove
global absence.

`ReadRecord` checks the exact canonical ID and admitted record identity. The
existing public v1 authority `source_binding` binds **canonical ID plus
SHA-256 of exact `statement_text` UTF-8**. It does not bind all AHE metadata,
lifecycle state, invalidation state, or a global currentness cut. This digest also
differs from the intake envelope's `sha256:`-prefixed raw-content hash.

## Explicit result review and admission

1. `Prepare` independently validates the exact package against caller-held
   authority bytes/hash and request ID. It then fetches every exact source ID and
   checks each statement digest. A valid solver assignment alone cannot pass this
   step.
2. `Prepared.Submit` submits the validated conditional statement as a source and
   a pending proposal. It does not admit a canonical fact. Partial intake receipts
   are returned for reconciliation if the second call fails.
3. `Prepared.Review` requests `derived_spec` review with the exact sorted parent
   set and package hash. It checks the pending proposal's exact statement,
   proposal ID, request and native display. The complete display is available to
   the caller for inspection.
4. `Prepared.Admit` requires external approval binding request ID, native subject,
   display hash, package hash and reason. It checks sources again, then submits
   that unchanged review once. It checks the native receipt's request, subject,
   reviewer, proposal, canonical ID and parent set.
5. A fresh Query must return the exact validated statement, `derived_claim`
   kind and matching native endpoint receipt. Validation, admission and readback
   remain separate records. The model statement retains its conditional status;
   it is not an observed fact or proof of source-to-model semantic fidelity.

The library does not invent approval, an approver identity, missing evidence,
a relaxed goal, or alternative rules. The current human-facing CLI has its own
explicit supported-command boundary; library availability does not imply an
interactive approval UI or automatic admission workflow has shipped.

## Native acceptance fixture

`scripts/ahe-integration-bootstrap.py` creates a new PostgreSQL cluster and fresh
roles for isolated synthetic/public fixture evidence. SQL is used only to create
that disposable cluster/schema/roles; all case sources, proposals, admissions
and readbacks use the real public MCP tools. Ordinary `go test ./...` skips these
native stages unless explicitly configured. Fixture approvals are labelled
`TEST APPROVAL STUB`, never human or semantic certification.

The completed local fixture `pouch-native-741478ef7aa1` covers key rotation and
Django 13344: nine source admissions/readbacks, eight newly searched packages,
four return and four no-return conclusions, and eight derived admissions with
exact fresh readback. See the portable [acceptance report](../artifacts/ahe-integration/pouch-native-741478ef7aa1/REPORT.md).

This is bounded integration acceptance. It does not prove original-problem
coverage, lifecycle currentness, official SWE runtime behavior, or physical
restore. The accompanying report records the two earlier fixture setup failures,
actual dirty-checkout/binary identity, and the supplementary-snapshot retention
limitation.

## Latest recorded file-repair integration

The separate fixture `pouch-native-25915315729c` connects the already completed
Django 13344 three-file execution to native AHE. It saves eight original artifacts
and the complete execution log as lossless `gzip+base64` source envelopes, checks
the decoded bytes against independently pinned originals, revalidates six
identity-rebound packages, and uses `Prepare` / `Submit` / `Review` / `Admit` for
six model results. A scoped Go test harness then saves six separate
`recorded_file_replay_binding` derived claims, each requiring its matching model
result and the complete execution-log source. A final Query checks all twelve.

The production model adapter API is unchanged. The execution-record binding is
an opt-in example integration, not a shipped general physical-repair validator.
Raw file hashes, envelope statement hashes and native receipts are distinct.
All approvals are TEST APPROVAL STUB in a new isolated DB; no production DB is
accessed, no new repair/solver/runtime test is run, and no historical result is
relabelled. The [report and complete public payloads](../artifacts/ahe-repair-integration/pouch-native-25915315729c/REPORT.md) retain exact
reviews, parents, admission receipts and final readbacks.

For this fixed fixture only, provision with the existing bootstrap's
`--discovery-only` option, set `POUCH_AHE_NATIVE_CONFIG` to the generated private
config and `POUCH_AHE_TEST_APPROVAL=isolated-synthetic-public-fixtures-only`, then
run `go test ./internal/ahe -run '^TestNativeRepairPublication$' -count=1`.
Use a new owned cluster; the test refuses to reuse its result directory. Stop it
with the bootstrap `--stop` option before running `scripts/ahe-repair-export.py`.
The source constants and frozen plan deliberately pin this historical input;
changing them is a new experiment, not a generic importer invocation.
