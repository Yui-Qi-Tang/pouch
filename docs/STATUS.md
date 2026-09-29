# Project status — 2026-09-29

Version: `0.1.0-dev`, unreleased. Branch: `feature/pouch-core`.
This checkpoint contains the standalone Go implementation and its current evidence. Historical experiment reports and frozen inventories describe their original execution time; their earlier “uncommitted” statements are not the current Git state. The checkpoint commit is identified by Git history rather than a self-referential hash inside this document.

## What works today

- Provider-independent source and authority input. AHE is optional; the core has no AHE/MCP/database/LLM dependency.
- Typed evidence graph ↔ matrix projection; separately declared operation matrices → CNF → SAT.
- Bounded simple first-hit forward path enumeration; separate shortest-return search from each complete endpoint.
- Independent original-rule replay, set comparison, shortest-cost checks, and full finite closure for model no-return conclusions.
- Shared AI-readable JSON and offline HTML replay, with source identity, assumptions, complete baseline, costs and result scope.
- Optional AHE public stdio MCP adapter: source read, local validation, pending submission, exact review, explicit approval, native admission and fresh readback.
- A dedicated Django example adapter that actually applies/reverses known patches on disposable copies of three source files, checking every step's hashes and file modes.

The executable v1 model supports categorical string fields, AND guards, constant effects, explicit frames, forbidden patterns and unit costs, up to 4,096 Cartesian states. Unsupported semantics are rejected. Evidence relations do not automatically become action rules.

## Acceptance inventory

There are **two example families and three declared models** in the Go project. The Lab's historical twenty-case inventory is not a twenty-case Go-port acceptance.

| Model / run | Current evidence | Scope |
| --- | --- | --- |
| Key rotation — local CLI | 29 queries (7 SAT / 22 UNSAT); 6 forward paths; 2 with verified model return and 4 with verified model no-return | Declared finite model; no real key operation |
| Django 13344 observation/staging — local CLI | 10 queries (4 SAT / 6 UNSAT); 2 forward paths, both with model returns | Forward 3 / return 1; `clear_staging` resets local observations, not source files or HTTP effects |
| AHE public-MCP integration of those same two models | Fresh 9 source admissions/readbacks, 39 new queries and 8 packages; all 8 validation/review/admission/readbacks completed | Isolated fixture with explicit TEST APPROVAL STUB; 4 model-return and 4 model-no-return statements. Repeated models, not two additional cases |
| Django 13344 H13 file-edit model — new Go run | 14 queries (7 SAT / 7 UNSAT); 6 forward orderings, each 3 operations; one separately solved 3-operation return from their shared endpoint; 6 receipts | Same known three-file repair in six orders, not new patch synthesis |
| Physical replay of the new file-edit model | Each of the 6 forward paths and its saved return executed on separate copies: 18 apply + 18 reverse operations; all three final file hashes and modes equal baseline | Actual three-file restoration only; no new Django/SWE runtime evaluation. Its record is connected by the separate persistence run below |
| AHE publication of the latest file-repair record | 9 lossless source/report admissions and byte-exact readbacks; 6 model + 6 execution-binding derived admissions; 12 final fresh readbacks; 36 existing events checked | Fresh isolated fixture `pouch-native-25915315729c`; zero new SAT queries, patch operations or official tests |

The local CLI, native-source integration, new file-edit run and later persistence run have separate denominators. Successful model validation, actual file execution and AHE persistence are separate evidence layers.

## Evidence and limits

- [First Go implementation and integration acceptance](implementation-acceptance.md).
- [Native AHE acceptance records](../artifacts/ahe-integration/pouch-native-741478ef7aa1/REPORT.md).
- [Django file-edit and execution report](django-13344-repair-demo.md), [demo entry](../artifacts/go-repair-20260929-01/index.html).
- [Latest AHE persistence report](../artifacts/ahe-repair-integration/pouch-native-25915315729c/REPORT.md), [review outcome and evidence boundaries](repair-ahe-review.md).

The AHE integration binds canonical IDs and exact statement text hashes. It does not certify every lifecycle field or an atomic global currentness snapshot. The Django evidence view was truncated at 128 nodes; model-search completeness does not turn it into a complete evidence world.

The new file-edit run has no fresh official test claim. Historical Lab `h13-restore-20260928-02` retains **FAIL / CONTROL_REPRODUCTION_MISMATCH**: its coroutine test reported FAIL → PASS → FAIL, while its deprecation test already passed at baseline. Local file-restoration PASS does not rewrite that historical result.

HTML data/parity and script syntax have been checked; actual browser interaction and visual acceptance remain unverified. No general physical rollback, external side-effect restoration or technical-debt cost guarantee follows from unit-cost model operations.

## Latest acceptance and review

The latest actual file-execution record is now bound, saved and freshly read back through public AHE MCP. See the [completed integration report](../artifacts/ahe-repair-integration/pouch-native-25915315729c/REPORT.md). The opt-in Go fixture keeps model receipts, historical execution evidence and native admission receipts distinct; it is not a general physical-execution validation API. Original source bytes use explicit lossless envelopes, with raw-byte hashes separated from native statement hashes. All original run artifacts remain unchanged. This checkpoint records the integration harness, documents and portable evidence together; Git history identifies the commit.

The [external review](repair-ahe-review.md) accepted the bounded claim and identified no concrete contradiction requiring withdrawal of PASS. The reviewer reported decoding the complete execution report, checking all 36 recorded file events and inspecting the representative `p000000` native publication/readback chain. This is a review of delivered records, not an independent rerun or certification of all source archives, solver evidence, adapter code or twelve native calls.

The fixed acceptance is complete: **recorded repair/reverse of three named file copies, model/event consistency, and subsequent AHE native persistence/readback binding**. It does not establish full Django repair correctness, restoration of an entire runtime environment or general recovery cost. No additional experiment is required to close this scope.

## Remaining product work

Other open product work is browser/manual display acceptance, a reusable execution-adapter contract beyond this fixed example, and explicitly selected additional Lab model ports. Auto mode, arbitrary patch synthesis, path merging and protobuf are deferred. There is no need to reopen unbounded original-problem coverage research to complete this checkpoint.
