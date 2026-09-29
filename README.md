# Pouch

Project version: `0.1.0-dev` (in development, unreleased). Current executable contract: `pouch-finite-validation/v1`.

Pouch is an evidence-grounded execution staging tool. Given a fixed evidence snapshot, an explicitly declared action model, and a goal, Pouch aims to enumerate legal paths within a specified scope, show how state evolves at each step, and separately verify whether a forward path is legal and whether its result can return to a specified baseline.

Every path must be traceable to its original sources, model, and query for verification. Matrices and SAT make the representation and search rules precise; they do not establish that natural language was interpreted correctly or guarantee that reality outside the model will behave as simulated.

Current checkpoint: [project status and acceptance scope](docs/STATUS.md).

## Current implementation

The Go implementation now includes local source acquisition, evidence incidence matrices, declared action matrices, symbolic CNF encoding, bounded SAT path enumeration, independent original-rule validation, and an offline HTML/JSON result bundle. The optional AHE adapter communicates through public stdio MCP tools. Core has no AHE, SQL, MCP, browser, or LLM dependency.

| Capability | Implemented scope |
| --- | --- |
| Source graph → matrix → graph | Preserves typed topology and opaque records in the supported canonical graph format; no inferred action guards |
| Finite planning | String-enumerated fields, conjunctive guards, constant effects, explicit frames, forbidden patterns, unit costs |
| Search | All simple first-hit action sequences within the declared forward bound, subject to explicit resource limits |
| Proof checks | Pinned external CaDiCaL/DRAT-trim; clause-checked SAT assignments and exit-0 VERIFIED UNSAT proofs |
| Original-rule checks | Separate evaluator/DFS/BFS checks path sets, replay, shortest claims and complete finite no-return closure |
| Human and AI outputs | Offline HTML replay and the same JSON bundle, sources, matrices, raw solver evidence and manifest |
| Optional AHE | Bounded Query reads, local validation, explicit pending submission, exact external approval, native admission and new Query readback |
| Deferred | Auto mode, protobuf/gRPC, new operation or patch synthesis, path merging, general physical rollback |

The first Go acceptance uses one existing synthetic key-rotation model and one Django 13344 model. It is not a new official SWE-bench run or acceptance of all historical Lab cases. See [implementation acceptance](docs/implementation-acceptance.md) for actual results and remaining display-validation limits.

The separate [Django file-repair demo](docs/django-13344-repair-demo.md) imports the fixed Django three-file edit model. It searches and validates apply/reverse paths in Go, then uses an explicit example adapter on disposable file copies. This is distinct from the first Django observation/staging model and its `clear_staging` operation.

Existing versioned JSON and typed Go structures remain the boundary. Hashes identify exact bytes. Source graph data and caller-owned `authority.json` remain separate; neither topology nor successful persistence establishes source-to-rule semantic coverage.

## Build and local search

Go 1.27.1 is required. The module uses only the standard library. Search additionally requires caller-supplied, SHA-256-pinned CaDiCaL and DRAT-trim executables; the standalone validator does not.

```sh
go build -o bin/pouch ./cmd/pouch
go build -o bin/pouch-validate ./cmd/pouch-validate
go test ./...
go vet ./...

bin/pouch solve \
  --authority authority.json --authority-sha256 "$POUCH_AUTHORITY_SHA256" \
  --request-id "$POUCH_REQUEST_ID" --sources sources.json --out new-result \
  --solver "$POUCH_CADICAL" --solver-sha256 "$POUCH_CADICAL_SHA256" \
  --checker "$POUCH_DRAT_TRIM" --checker-sha256 "$POUCH_DRAT_TRIM_SHA256"
```

`new-result` must not exist. `index.html` is an offline view of `bundle.json`; both come from one result. `manifest.json` inventories saved input, formula, proof, receipt and display bytes. `--graph graph.json --graph-sha256 "$POUCH_GRAPH_SHA256"` adds a separately pinned source graph. Without it, the evidence view explicitly contains selected source records only, with topology marked **not supplied**.

`pouch verify` checks one package against external authority. `pouch project` projects a pinned graph. `pouch render` renders a stored bundle; rendering alone never revalidates its claims. Exit 0 means the command completed, 1 means a solve/verify semantic rejection, and 2 means usage, input, tool, resource, or incomplete execution. A `FOUND` partial run may exit 2: existence and set completeness are distinct.

See [bundle/input format](docs/bundle-format.md), [search and tool contract](docs/search-format.md), [graph projection](docs/projection-format.md), and [AHE adapter](docs/ahe-adapter.md).

## Using the ported validator

Go 1.27.1 is required, as specified in `go.mod`. The validator uses only the Go standard library and requires no AHE, PostgreSQL, Python, or SAT binary.

```sh
go build -o bin/pouch-validate ./cmd/pouch-validate
go test ./...
go vet ./...
```

A trusted caller supplies the complete `authority.json`, the SHA-256 of its original bytes, and the request ID. The candidate producer supplies only `package.json` and must not also replace the validation authority.

```sh
bin/pouch-validate \
  --authority authority.json \
  --authority-sha256 "$POUCH_AUTHORITY_SHA256" \
  --request-id "$POUCH_REQUEST_ID" < package.json
```

The caller must set `POUCH_AUTHORITY_SHA256` and `POUCH_REQUEST_ID` in advance; values asserted by the candidate package must not be trusted on their own. The JSON structures are defined by [Contract, Package, and Receipt](internal/validation/validation.go). The current language supports string-enumerated fields, AND guards, constant effects, a complete effect/frame partition, and unit costs. Unsupported types and semantics are rejected; arbitrary string conversion must not be used to bypass these limits.

| Exit | JSON status | Meaning |
| --- | --- | --- |
| 0 | `ACCEPTED` | The package passed the finite-model and binding checks for this request |
| 1 | `REJECTED` | Types, model, witness, or bindings violate the contract |
| 2 | `INCONCLUSIVE` (when output is possible) | Usage, input acquisition, cancellation, timeout, or output failure; this is not evidence that the model has no solution |

An accepted receipt remains a `model_conditional_conclusion` and grants no external write authority. The v1 field `native_ahe_receipt=false` is retained for wire compatibility; it does not enable an AHE connection or admission.

## Relationship with AHE

Pouch and ahe-mcp are independent Go modules. Neither imports the other's internal code, and there is no module dependency between them. AHE is an optional evidence provider. The optional Pouch AHE adapter uses general MCP contracts to obtain snapshots and submit authorized material for review.

Pouch handles planning and model validation. AHE handles evidence governance, native review, persistence, and readback. Successful AHE persistence does not imply successful Pouch validation; the two receipts must be bound separately. The former Pouch-specific internal AHE wrapper is not used. Pouch validates locally; a separately approved AHE endpoint saves a conditional derived claim. The two receipts remain separate.

## Documentation

- [ARCH: Architecture and behavioral specification](docs/ARCH.md) — module responsibilities, inputs, communication, outputs, no-path diagnostics, two review tracks, and acceptance scope.
- [THEORY: Representation, search, and validation](docs/THEORY.md) — graphs, matrices, finite models, SAT, replay, return paths, completeness, and limitations.
- [Port provenance and file hashes](docs/port-manifest.json) — pinned source commit and identities of the original and ported files.

The original MIT notice for the ported code is preserved in [third_party/ahe-mcp/LICENSE](third_party/ahe-mcp/LICENSE). Go implementation follows `go-project` and `go-style`, prioritizing explicit types, one-way dependencies, and verifiable boundaries.
