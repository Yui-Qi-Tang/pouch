# Project status — 2026-10-04

Version: `0.1.0-dev`, unreleased. The candidate-workflow migration was based on
`480aec39fbce2cca1ca4abbe3780f4fae3c00eca` for integration into `develop`.

## Current capabilities and verification

The existing graph/matrix projection, bounded SAT path search, original-rule
replay, separate model recovery, JSON/HTML reports and optional public AHE MCP
adapter remain available. The model uses finite categorical string values,
AND guards, constant effects, explicit frames and unit costs, with at most
4,096 Cartesian states.

The new [candidate workflow](../internal/candidate/README.md) compiles explicit
finite options and forbidden combinations, revalidates candidate packages,
materializes pinned source templates into patches, and checks caller-pinned
runtime observation records against source/model/package/patch/artifact hashes.
It does not infer the repair grammar or semantic requirements from an issue.
`RECORDED_CHECKS_PASS` means record consistency, not fresh execution, log
authentication or native approval.

AHE depth-zero snapshots with explicitly null edges are normalized only under
checked zero-edge, nontruncated read scope. Original artifact bytes and hashes
are preserved alongside separate projection bytes/hashes. Unknown topology is
not interpreted as globally absent edges.

The candidate-workflow regression completed its bounded evaluation with explicit engineering
continuations: 25 historical SWE cases using known official repair fixtures,
plus four existing Pylint finite-option combinations. All 29 completed arms passed
the selected repair tests, tracked-file reversal and baseline behavior checks.
The 25 cases contain 2,874 official scoring IDs; Pylint additionally checks all
20 parameterized IDs per arm. This is a known-answer regression, not 25 blind
repairs or evidence of newly discovered solutions.

The frozen search used 214 real solver queries (58 SAT, 156 UNSAT), with checked
assignments/proofs and 29 separate model returns. After correcting opaque test
ID handling, the final product regenerated all 26 authorities and 29 rendered
outputs byte-for-byte and revalidated all runtime records. No new SAT searches
were counted for that final compatibility check. Final build, vet and uncached
race tests passed. Earlier setup/parser failures and one race fixture timeout
are retained in the recorded migration report; no test expectations were relaxed.

Fresh native AHE integration separately passed two fixed models: nine sources,
eight derived admissions and eight fresh result readbacks, under explicit test
approval stubs in a disposable database. Actual native null-edge normalization
also passed with the final binary. The 29 new SWE runtime records were not all
submitted as fresh native admissions. The owned test database and containers
were stopped/removed.

## Where the migrated capabilities live

| Capability | Product entry point | Boundary |
|---|---|---|
| Graph incidence projection | `project`, `internal/projection` | Preserves supported topology; does not derive action rules |
| Matrix/CNF/SAT search and separate recovery | `solve`, `internal/planning`, `internal/validation` | Finite declared model, search policy and budgets |
| Finite candidate compilation | `candidate-prepare` | Caller supplies options, constraints and source references |
| Patch materialization | `candidate-materialize` | Checked package and baseline bytes; no patch application |
| Runtime-record binding | `candidate-bind-runtime` | Exact supplied artifacts and observation consistency; no test execution |
| Native evidence read and model-result storage | `ahe-read`, `ahe-stage`, `ahe-admit` | Public MCP plus explicit external approval; runtime bindings need a separate publication workflow |
| Human and AI reports | `solve`, `render` | Saved model replay in HTML and JSON; no implicit execution report |

## Boundaries and remaining work

Evidence identity does not establish source truth or complete original-problem
coverage. The Pylint experiment's problem-specific semantic interpreter remains external; callers
supply finite options and constraints. Model choice withdrawal is not physical
restoration. Actual runtime results and restoration require their own execution
evidence; the record binder checks only the declared observation scope.

Generated HTML content and links were checked. New browser interaction/visual
acceptance, arbitrary execution adapters, autonomous operation invention,
complete problem understanding, external side-effect recovery, path merging,
auto mode and protobuf are not claimed by this port.

## Documentation and local evidence

Source, maintained product documentation and small regression tests are tracked.
Detailed run reports and generated evidence remain local and ignored. The
2026-10-04 migration report and the maintained Lab/product migration ledger
record the port and case-level results. This documentation follow-up does not
rewrite frozen source/document hashes or add experiments; see
[output and archive handling](artifacts.md). Historical results retain their
original status and do not override the current conclusion above.

---

## Historical checkpoint — 2026-09-30

The earlier product acceptance established graph-bound planning and public-MCP
storage/readback for selected fixed models. It also persisted an existing
recorded Django repair/reverse result; that publication did not run new repairs.

A separate Django14089 run used its known official patch: the 44-test module
failed as expected before repair, passed after repair, and reproduced the
baseline failure after restoration. All 6,440 tracked file hashes and modes were
restored. Model and execution statements were admitted and freshly read back
through AHE, as separate records without a native derived edge joining them.
It used a local Python environment, not the official SWE Docker harness.

These are dated results, not new migration runs. Earlier control mismatches and
inconclusive outcomes remain in their original records. Current implementation,
verification and remaining boundaries are described above.
