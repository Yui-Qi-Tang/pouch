# Generated output and optional test archives

Pouch writes HTML reports, JSON bundles, formulas, proofs, receipts and manifests.
These are run outputs, not product source. The whole `artifacts/` directory and
`docs/acceptance/` are ignored. Other `docs/` files are ignored unless explicitly
listed in `.gitignore` as maintained user, AI or developer documentation.

The curated [experimental evaluation](EVALUATION.md) is maintained product
documentation: it keeps result tables, scope and archive identities in Git while
the underlying generated runs remain outside this repository.

Ignoring an already tracked file is insufficient: it must also be removed from
the Git index. Generated evidence is untracked, and the repository history was
subsequently rewritten to remove generated output and detailed non-product
documents. Local files were preserved. An external Git backup retains the old
history for recovery; it is not part of this repository. New clones do not
receive these outputs or their old commit history.

## Create and share a report

Use a fresh output directory for every authorized solve. `pouch solve` writes
`index.html` and `bundle.json` when it reaches report generation; early failures
may produce neither. `pouch render` can display a saved bundle without rerunning
or validating it. See the [AI workflow](../.claude/skills/pouch/references/workflow.md).

Preserve the complete output and manifest when archiving a run. Keep authority
and source inputs under appropriate version control separately from generated
results. Remove private launcher configuration, credentials and host paths from
material shared externally. Reports are snapshots, not live results.

## Retrieve historical output when needed

Earlier output can be recovered from a separately retained evidence archive or
external Git backup. The old checkpoint
`470360b828d69cafe523b8fb6d6b960fdb1223d5` is no longer present in the cleaned
repository. If you hold a backup containing that checkpoint, set
`POUCH_HISTORY_BACKUP_GIT` to its Git directory and export into a new directory:

```sh
mkdir pouch-evidence-20260929
git --git-dir="$POUCH_HISTORY_BACKUP_GIT" archive \
  470360b828d69cafe523b8fb6d6b960fdb1223d5 artifacts \
  | tar -x -C pouch-evidence-20260929
```

The backup is optional and is not distributed with the product. Outputs created
after that checkpoint require their separately retained local archive. Do not
overwrite an existing run when restoring evidence.

## Test dependencies

Ordinary `go test ./...` uses versioned small fixtures in `testdata/` and does not
need historical run directories. Keep those fixtures tracked. SAT search requires
caller-configured, hash-pinned external tools; ignored historical binaries are
not an installation mechanism.

The optional `TestNativeRepairPublication` specifically requires the full
`artifacts/go-repair-20260929-01` historical inventory. On a checkout where that
run is absent, restore the exported directory without overwriting another run:

```sh
test ! -e artifacts/go-repair-20260929-01 && \
  cp -R pouch-evidence-20260929/artifacts/go-repair-20260929-01 artifacts/
```

The smaller `testdata/recorded-repair/` subset is enough for its default regression
test, but cannot stand in for the complete inventory required by the native
fixture. See [AHE test configuration](ahe-adapter.md) for the explicit isolated
DB and approval setup. Removing files from tracking does not remove these test
requirements or turn unavailable evidence into a successful test.
