# Evidence graph projection

`internal/projection` accepts the explicit `canonical-evidence-graph/v1` JSON
shape used by the Lab H2 fixture. It depends only on Go's standard library.

A caller supplies `Decode(raw, expectedSHA256)` with the SHA-256 of the exact
original bytes from outside the untrusted graph. The decoded snapshot owns its
bytes and data; its accessors return copies. Reserializing a graph does not
reproduce its original whitespace or digest.

Required root fields are `schema_version`, nonempty `snapshot_id`, `nodes` and
`edges`. Each node has a unique nonempty `id`. Each edge has a unique nonempty
`id`, `from`, `to` and `relation`; both endpoints must exist. Empty graphs,
isolated nodes, self-loops, parallel edges and unknown relation labels are
supported. Duplicate JSON keys and case aliases of structural fields are
rejected. Other fields remain raw JSON sidecars, including payloads, provenance,
AND parent manifests, source bindings, scope, truncation and unknown values.

This is topology validation, not native AHE schema validation or proof that a
parent manifest is complete, a source is true, or a graph covers a requirement.
Opaque provider values may contain null; typed topology fields may not.

`Project(snapshot)` sorts node and edge IDs. It builds two separate binary
incidence matrices, each with node rows and edge columns, plus one binary
relation mask per distinct relation. Every node has presence 1. Node records and
edge attributes retain all non-topology fields. `Reconstruct(matrix)` requires
one source, one target and one relation per edge and derives them from the cells.
It rejects copied topology fields hidden in edge sidecars.

The matrix `source_sha256` identifies the original snapshot. It does not
authenticate a subsequently modified matrix or reconstructed graph. Consumers
must compare reconstruction against the caller-bound original when asserting
round-trip equality. No evidence edge becomes an action guard.

Limits are 4 MiB input, depth 32, 4,096 nodes, 8,192 edges and 1,048,576 total dense
matrix cells. Projection checks the cell budget before matrix allocation. These
are implementation bounds, not claims of general graph scalability.

Tests include an unchanged copy of the Lab H2 synthetic fixture at
`internal/projection/testdata/h2-artifact.json`, SHA-256
`e4b92c002656d881868ada4eab6f773a1ee549a973701f721467b0913c987f9b`.
Its synthetic provenance is retained; it is not observed or admitted evidence.

## Binding a graph to a solve request

Projection itself does not interpret the opaque payload records. The optional
`solve --graph` input additionally uses the [run source-binding contract](../internal/run/README.md#optional-pinned-graph-input)
to match every authority-selected source. It supports inline content digests and
`source_claim` nodes referencing `payloads[].claim`. Supplied content is hashed;
a declared digest cannot override conflicting content. This preflight occurs
before SAT work. The saved graph and matrix's source hash continue to identify
the original bytes, not a decorated or reserialized native artifact.
