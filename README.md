# Pouch

**Explore possible paths. Check each step. Find a way back.**

Pouch is a standalone Go tool for planning changes from evidence. Give it a source snapshot, explicit operation rules, an initial state and a goal. It searches for legal paths, shows how the state changes, and separately searches for a return to your chosen baseline.

The return is a new planning problem: it can use different operations, take more steps, or be impossible under the declared rules. Pouch makes that distinction visible before you execute a change.

Version: **`0.1.0-dev`** — in development, unreleased.

## What you get

- **Possible forward paths:** enumerate simple paths that first reach the goal within your chosen bound; report incomplete searches explicitly.
- **A separate recovery result:** a shortest model return, a checked finite-model no-return conclusion, or an inconclusive result. Reaching the goal alone does not establish recoverability.
- **Inspectable reasoning:** source identities, assumptions, guards, effects, unchanged fields, per-step states and operation counts.
- **Two views of one result:** an offline interactive HTML report for people, and a versioned JSON bundle for AI and other tools.

For example, a change may reach its goal in two operations while returning to the baseline requires three. The report shows both routes. These costs count declared operations; real execution time and external side effects require an execution adapter and their own checks.

## How it works

```text
Evidence snapshot + declared model + initial state / goal / baseline
                              |
               Matrix representation -> CNF -> SAT search
                              |
           Original-rule replay and separate return checks
                              |
                    JSON bundle + HTML replay
```

Evidence relationships and operation rules are separate inputs. Pouch can preserve and display a supplied evidence graph through a matrix projection; graph edges do not automatically become legal operations. The caller-owned `authority.json` defines the model and query used to check candidate results.

The current model supports finite categorical states, AND guards, constant effects, explicit unchanged fields and unit operation costs. Results are conditional on those rules and assumptions. Pouch does not yet invent operations or patches from raw evidence, and a model return is not by itself proof of physical restoration.

Pouch works with local files and has no required evidence service, database or LLM. An optional [AHE adapter](docs/ahe-adapter.md) reads evidence and submits explicitly approved results for storage through public MCP tools.

## Build and use

Requires Go **1.27.1**. The Go module uses only the standard library. SAT search additionally requires caller-supplied CaDiCaL and DRAT-trim executables pinned by SHA-256; standalone model validation does not.

```sh
go build -o bin/pouch ./cmd/pouch
go test ./...

bin/pouch solve \
  --authority authority.json --authority-sha256 "$POUCH_AUTHORITY_SHA256" \
  --request-id "$POUCH_REQUEST_ID" --sources sources.json --out new-result \
  --solver "$POUCH_CADICAL" --solver-sha256 "$POUCH_CADICAL_SHA256" \
  --checker "$POUCH_DRAT_TRIM" --checker-sha256 "$POUCH_DRAT_TRIM_SHA256"
```

Set the tool paths, trusted hashes and request ID before running. The output directory must be new. Open its `index.html` to explore saved paths; read `bundle.json` for the same structured results. The report is a snapshot of that run: browsing it does not execute changes or rerun the solver.

`pouch verify` checks a candidate package against caller-owned authority. `pouch project` converts a supported evidence graph to a matrix representation. `pouch render` renders an existing bundle; rendering does not revalidate its claims. See the [input and bundle format](docs/bundle-format.md) and [search/tool contract](docs/search-format.md) for commands, schemas, limits and exit statuses.

## Using Pouch with AI

An AI agent can use the CLI to explore a declared problem and inspect the structured result:

1. **Fix the question:** identify the exact source snapshot, operations, start, goal, complete baseline, assumptions and search bounds. Draft missing rules as proposals; do not treat them as established evidence.
2. **Pin the inputs:** use caller-owned `authority.json`, its independently supplied SHA-256, the request ID and `sources.json`. Keep those trusted values separate from candidate packages.
3. **Search:** run `pouch solve` in a new output directory using approved, pinned solver/checker binaries. This writes planning results; it does not execute the planned actions.
4. **Inspect and verify:** read `bundle.json`, including `status`, `complete`, `set_check`, `diagnostics` and each path's `recovery_status`. Use `pouch verify` on a selected `package_file` against the original authority. One accepted package does not prove the whole set is complete.
5. **Explain the result:** show the forward path, separate return or no-return evidence, operation counts, assumptions and remaining limits. Link `index.html` for human replay. If the result is incomplete, explain why rather than silently loosening the model or calling it impossible.

The [AI workflow and CLI examples](.claude/skills/pouch/references/workflow.md) cover inputs, exact commands, status interpretation and evidence follow-up. Actual execution and external publication require their own authorized workflow.

For Claude Code, open this repository and invoke:

```text
/pouch Inspect this saved bundle and explain each forward path and its recovery status.
/pouch Use my pinned authority and sources to enumerate paths within the declared bounds. Do not execute them.
```

The project includes [AGENTS.md](AGENTS.md), [CLAUDE.md](CLAUDE.md), and the [Pouch skill](.claude/skills/pouch/SKILL.md). `CLAUDE.md` imports the shared agent instructions. The skill lives in Claude Code's project skill directory; it does not install Pouch or grant execution/publishing permissions. To use it in another repository, copy the entire `pouch` skill directory, including `references`, into that repository's `.claude/skills/`; supply a trusted Pouch executable separately. See the official [Claude Code skill conventions](https://code.claude.com/docs/en/skills) and [instruction imports](https://code.claude.com/docs/en/memory#import-additional-files).

## Documentation

- [Architecture and specification](docs/ARCH.md)
- [Theory: representation, search, replay and recovery](docs/THEORY.md)
- [Current capabilities and verification scope](docs/STATUS.md)
- [Report output and optional test archives](docs/artifacts.md)

## License

[MIT](LICENSE).
