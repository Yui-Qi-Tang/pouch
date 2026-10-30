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
| Query | `get_evidence_record`, `open_canonical_read_view`, `get_evidence_use_state`, `check_current_evidence_plan` |
| Intake | `submit_external_source`, `submit_extractor_output` |
| Source claim reviewer | `get_source_claim_review`, `admit_reviewed_source_claim` (fixture ingestion) |
| Endpoint reviewer | `get_endpoint_review`, `admit_reviewed_endpoint` |

Missing tools, mismatched schema pins, or calls outside the selected profile return
`CAPABILITY_UNAVAILABLE`. There is no direct SQL or legacy writer fallback.
Database roles remain the server's authorization boundary; client allowlists do
not replace them. Credentials belong in protected native launcher configuration.

### Upstream schema changes and re-pinning

The governed workflow targets the public AHE schema 59 / role policy v10 contract.
New derived reviews require `derivation.use_policy`. Schema 57 endpoint pins do
not match that contract. Pouch deliberately keeps exact schema matching.

1. Upgrade Pouch before attempting a new stage/admission. Updating pins alone
   does not make an older client send `use_policy`.
2. Run `pouch ahe-discover --config query-command.json` and repeat with the
   independently provisioned endpoint command. Discovery makes no tool calls.
3. Review the advertised schemas against the intended AHE build. Copy each
   returned `input_schema_sha256` into the operator-owned profile config. The
   displayed `input_schema` is formatted for inspection: do not rehash that
   reformatted JSON. Pins identify the exact raw schema bytes received.
4. The governed Query profile must pin `get_evidence_record`,
   `get_evidence_use_state` and `check_current_evidence_plan`; also pin
   `open_canonical_read_view` when using `ahe-read`. Pin both endpoint tools.
   Reuse unchanged intake pins only after verifying discovery still matches.
5. Run the source check below before modeling. Keep the selected policy fixed
   through the manifest, final check and native review. Never auto-update pins,
   downgrade policy on refusal, or enable a legacy writer as a fallback.

These are local operator configuration changes, not new hardcoded digests in
Pouch. Historical native receipts and frozen discovery files remain unchanged.

Requests are serialized, bounded, and never automatically retried. A lost,
cancelled, malformed, or protocol-error write reply returns `DELIVERY_UNKNOWN`.
The caller must reconcile the exact request and any retained receipt before making
another write. A native tool error is retained as a structured error, not treated
as an admission receipt. A readback failure after admission retains the admission
receipt and does not automatically resubmit it.

## Read and hash semantics

`ReadSnapshot` materializes a bounded canonical read view, checks its scope,
counts and snapshot identity, and retains the native `canonical-evidence-graph/v1`
artifact. Supported native graphs pass the strict projection decoder unchanged.
One explicit compatibility case is handled at the adapter boundary: `edges:null`
becomes `edges:[]` only when the checked view has `max_depth=0`, `max_edges=0`,
`edge_count=0`, `truncated=false` and `global_absence_inference_allowed=false`.
Missing edges, null nodes or malformed topology remain errors. The generic graph
decoder is not relaxed, and the bounded zero-edge view proves no global absence.

`Snapshot.Artifact` is a structured convenience view. `ArtifactBytes` and
`RawResponse` are byte copies exported as base64 so JSON pretty printing cannot
change the bytes identified by `ArtifactSHA256` and `ResponseSHA256`. A truncated
view stays explicitly truncated. `ProjectionBytes` / `ProjectionSHA256` are
exported as `projection_bytes_base64` / `projection_sha256`. `normalization` is
`none` when these bytes equal the original artifact, or
`ahe-depth-zero-null-edges/v1` for the explicit conversion above. Preserve both
representations and both hashes. Even a complete bounded view does not prove
global absence.

`ReadRecord` checks the exact canonical ID and admitted record identity. The
existing public v1 authority `source_binding` binds **canonical ID plus
SHA-256 of exact `statement_text` UTF-8**. It does not bind all AHE metadata,
lifecycle state, invalidation state, or a global currentness cut. This digest also
differs from the intake envelope's `sha256:`-prefixed raw-content hash.

Decode `projection_bytes_base64` into the exact file passed to `solve --graph`,
and use `projection_sha256` as `--graph-sha256`. With `normalization=none`, this
pair equals the original artifact pair; after normalization it does not.
The run layer resolves each selected
`source_claim.payload_ref` to `payloads[].claim` and hashes that exact decoded
UTF-8 statement against caller-owned `source_binding`. It does not use the
payload's `span` or add synthetic digest fields to the native graph. Conflicting
inline content/digests, malformed references and duplicate payload IDs fail
before SAT starts. See the [full binding rules](../internal/run/README.md#optional-pinned-graph-input).
The source manifest and explicit model authority are still required; this does
not compile graph relations into operations or establish an atomic currentness
snapshot. Extract the base64 byte copy, not a JSON reserialization of the
convenience `Artifact` field. The matrix source hash identifies the projection
input; the native artifact and response retain their own original identities.

## Explicit result review and admission

1. `Prepare` independently validates the exact package against caller-held
   authority bytes/hash and request ID. It then fetches every exact source ID and
   checks each statement digest. It requires a separately pinned action-basis
   manifest and performs fresh source and final action checks. A valid solver
   assignment alone cannot pass this step.
2. `Prepared.Submit` submits the validated conditional statement as a source and
   a pending proposal, after repeating the governance checks. It does not admit a canonical fact. Partial intake receipts
   are returned for reconciliation if the second call fails.
3. `Prepared.Review` requests `derived_spec` review with the exact sorted parent
   set, selected `use_policy` and package hash. The derivation method includes
   the caller's exact manifest digest, so changing the policy or mapping needs a
   new exact review/approval. It checks the pending proposal's exact statement,
   proposal ID, request and native display. The complete display is available to
   the caller for inspection.
4. `Prepared.Admit` requires external approval binding request ID, native subject,
   display hash, package hash and reason. It checks source bytes and governance again, then submits
   that unchanged review once. It checks the native receipt's request, subject,
   reviewer, proposal, canonical ID and parent set.
5. A fresh Query must return the exact validated statement, `derived_claim`
   kind and matching native endpoint receipt. Validation, admission and readback
   remain separate records. The model statement retains its conditional status;
   it is not an observed fact or proof of source-to-model semantic fidelity.

The AHE endpoint itself rechecks the selected policy over the complete AND
ancestry and the new result's bound source inside its committing transaction.
Pouch's earlier read cannot close that transaction race. Under grounded policy,
the newly submitted result source also needs native positive source assessments;
checking only its parents is insufficient. Pouch does not create those reviews
or silently downgrade the requested policy.

The library does not invent approval, an approver identity, missing evidence,
a relaxed goal, or alternative rules. The current human-facing CLI has its own
explicit supported-command boundary; library availability does not imply an
interactive approval UI or automatic admission workflow has shipped.

## Governed planning and the caller's action-basis manifest

Before constructing the model, check the selected source IDs:

```sh
pouch ahe-check-sources --config profiles.json --request basis-ids.json
```

`basis-ids.json` is `{"canonical_ids":["canon-node:example"]}`. The command
defaults to `current-action/v1`; select `--use-policy grounded-current-action/v1`
explicitly for positive source retention, correspondence and coverage checks.
Current-action still rejects recorded failures/unresolved findings, withdrawn
or superseded bases and active reviewed contradictions. A read is historical,
not proof of truth or a reusable execution permit.

After declaring the model, supply an independent caller-owned manifest:

```json
{
  "schema_version": "pouch-ahe-basis/v1",
  "authority_sha256": "<exact authority.json SHA-256>",
  "request_id": "<authority request_id>",
  "use_policy": "current-action/v1",
  "actions": [
    {"action_id": "change", "basis_ids": ["canon-node:example"]},
    {"action_id": "recover", "basis_ids": ["canon-node:example"]}
  ]
}
```

Every model action, including recovery actions, must appear exactly once with
1–32 distinct canonical basis IDs drawn from the authority's `source_binding`.
Unknown/missing actions, duplicate bases, unknown policy, ambiguous JSON keys,
changed hashes and request/authority mismatches are rejected. The manifest must
explicitly select a policy; omission is not a compatibility default. Declaring
every source for every action is permitted only when that is the caller's actual
AND-basis choice. Pouch does not infer the mapping or prove omitted premises.

Add these three flags to the ordinary `solve` invocation:

```sh
--ahe-config profiles.json \
--action-basis action-basis.json \
--action-basis-sha256 <exact action-basis.json SHA-256>
```

Pouch checks all authority sources before starting SAT. After search it calls
`check_current_evidence_plan` for every distinct action emitted in all forward
and recovery traces, using the pinned mapping. Repeated occurrences of one model
action share its fixed mapping. There is no union of separate snapshot passes:
the authority has at most 32 sources and all declared action bases fit one final
native request. This optional mode does not inject provider code into the finite
model or solver. Offline `solve` still works without AHE and claims no AHE check.

The portable output retains exact `action-basis.json` and
`ahe-governance.json`. The latter binds the authority, request, manifest, search
JSON digest (`json.Marshal(bundle.Search)`) and emitted package byte hashes.
Native response bytes are preserved as `raw_response_base64`, with their exact
`response_sha256`; `assessment` is a convenience display of native reasons.
Checks bind the selected policy, roots and returned action mapping. Missing,
malformed, mismatched or unavailable responses fail closed.

`accepted=false` becomes `GOVERNANCE_REJECTED`, never UNSAT. An unavailable or
unusable check becomes `GOVERNANCE_UNAVAILABLE`. Governed `solve` returns exit 2
for either and exposes both `model_status` and `governance_status`; the bundle's
model status remains separate, with governance recorded in scope/diagnostics.
A preflight failure returns before solver setup and retains its report without
a model bundle. If no actions were emitted, Pouch rechecks all authority bases
and reports `NO_ACTIONS_BASIS_CHECKED`; it does not invent an action to satisfy
the native tool's nonempty-input rule or claim empty-plan acceptance.

Use the same `--action-basis` and `--action-basis-sha256` with `ahe-stage` and
`ahe-admit`. Both recheck current governance; saved PASS results are never inputs.
Publication checks also bind the exact package digest. Native reasons and
original model results remain inspectable after rejection. A policy PASS checks
declared evidence use only, not source-to-action entailment, completeness of the
caller's premises, physical restoration, or permission for later execution.

`ahe-stage` and `ahe-admit` accept model packages under the existing authority
contract. A `pouch-runtime-binding/v1` record is not a substitute package and is
not automatically published by `candidate-bind-runtime`. Persisting execution
evidence requires a separate authorized review/publication workflow retaining
the model, materialization and runtime bindings. The historical fixture below
does not provide a general runtime-record publication command.

## Native acceptance fixture

`scripts/ahe-integration-bootstrap.py` creates a new PostgreSQL cluster and fresh
roles for isolated synthetic/public fixture evidence. SQL is used only to create
that disposable cluster/schema/roles; all case sources, proposals, admissions
and readbacks use the real public MCP tools. Ordinary `go test ./...` skips these
native stages unless explicitly configured. Fixture approvals are labelled
`TEST APPROVAL STUB`, never human or semantic certification.

These opt-in tests exercise actual MCP processes and a disposable PostgreSQL
store. Their bounded results do not establish original-problem coverage,
lifecycle currentness, official SWE runtime behavior or physical restoration.
Detailed per-run reports are local ignored output; the
[experimental evaluation](EVALUATION.md) records the measured scope.

## Optional recorded file-repair test

`TestNativeRepairPublication` validates a fixed existing Django execution record,
rebinds its source identities, and persists model results plus separate
`recorded_file_replay_binding` claims through native review/admission and fresh
readback. It checks decoded source envelopes against the pinned original bytes.
The record-binding harness is an example integration, not a general production
physical-repair validator. It does not execute new repairs or official tests.

Raw file hashes, envelope statement hashes and native receipts remain distinct.
Approvals are TEST APPROVAL STUB in a new isolated DB. The full historical
`artifacts/go-repair-20260929-01` directory is required; follow the
[archive guide](artifacts.md) if it is absent. Ordinary tests use the smaller
`testdata/recorded-repair` subset instead.

For this fixed fixture only, provision with the existing bootstrap's
`--discovery-only` option, set `POUCH_AHE_NATIVE_CONFIG` to the generated private
config and `POUCH_AHE_TEST_APPROVAL=isolated-synthetic-public-fixtures-only`, then
run `go test ./internal/ahe -run '^TestNativeRepairPublication$' -count=1`.
Use a new owned cluster; the test refuses to reuse its result directory. Stop it
with the bootstrap `--stop` option before running `scripts/ahe-repair-export.py`.
The source constants and frozen plan deliberately pin this historical input;
changing them is a new experiment, not a generic importer invocation.
