# Experimental results and what they establish

Documentation updated **2026-10-31**. Results below were recorded on
**2026-10-03–04** for Pouch `0.1.0-dev` (unreleased), unless a section gives
another date.

**The completed evaluations support finite combination search, checked forward
and return paths, and traceable repair/reversal records.** H30 tested selecting
combinations under supplied requirements; H31 tested the migrated Go workflow
with known repairs. They answer different questions and are reported separately.

This is a curated product-facing summary of recorded Lab evidence. It adds no
experiments and does not turn archive hashes into independent certification.
Detailed logs, formulas, proofs and generated reports remain outside this
product repository; [evidence identities](#evidence-identities) identify the
records used here.

## What Pouch contributes

| User need | Observed evidence | Practical value within the tested scope |
|---|---|---|
| Explore alternatives before running a patch | H30 selected 4 different configurations from 32; all 4 passed the specified runtime tests without test-feedback re-search | Produce a finite set of inspectable choices instead of only one proposal |
| Check the route and its return separately | H31 checked 29 forward paths and 29 separate model returns, supported by 214 solver queries | Show whether the declared model permits recovery; reaching the goal alone is insufficient |
| Connect reasoning to execution evidence | H31 materialized and bound 29 completed repair/reversal records | Trace the exact source, model, path, patch and recorded test/restore observations |
| Preserve a result's evidence context | H30 saved/read back 4 candidate claims through AHE; H31 separately saved/read back 8 fixed-model claims | Retrieve accepted claims with their source identities and declared dependencies |

The external runner, rather than the planner, applied and reversed the patches.
A model return withdraws modeled choices. Physical restoration below refers to
the separately observed tracked-file and baseline-test checks.

## H30: one search, four working combinations

**Current result: bounded repair evaluation and subsequent AHE storage/readback
completed.** Case: `pylint-dev__pylint-8898`, SWE-bench Verified.
Search: `h30-20261003-01`; publication:
`h30-publication-20261004-02`.

This experiment asked whether Pouch could select combinations **before receiving
this run's test scores**. It assumed evidence completeness only for **9 explicit
behavioral requirements**. The supplied grammar offered five Boolean choices:
quantifier, character class, group, escape and normalization handling
(`2^5 = 32` configurations).

An external, problem-specific symbolic interpreter translated those requirements
into three forbidden partial combinations. Pouch searched the declared action
model with matrices/CNF/SAT. Fixed option order prevented multiple edit
permutations from being counted as different repairs.

| Measurement | Recorded result |
|---|---:|
| Actual semantic searches / searches after runtime feedback | 1 / 0 |
| Solver queries, all checked | 39 = 8 SAT + 31 UNSAT |
| Distinct configurations / distinct patches selected | 4 / 4 |
| Forward paths / separately searched model returns | 4 / 4; 6 model operations each |
| Direct DFS versus SAT result sets | Equal within the declared search |
| Symbolic versus materialized-function comparisons | 288/288 agreed: 32 configurations × 9 requirements |
| Selected candidates satisfying all supplied requirements | 4/4 |
| Candidate runtime arms / official-patch control arms | 4 / 1 |
| Specified parameterized tests after patching | 20/20 per arm |
| Baseline tests before repair and after reversal | 18 passed, 2 failed per arm |
| Tracked file contents and modes restored | 3,741 per arm |
| Historically new combinations | 0 |

The search bound was exact length 0…6, without repeated states and stopping on
first reaching the goal. There were 22 declared actions and 1,701 Cartesian
states. The table describes this bounded search, not every possible source edit.

### Which combinations worked?

All four enable quantifier and normalization handling and disable group handling.
Character-class and escape handling remain independent choices.

| Candidate | Quantifier | Character class | Group | Escape | Normalize | Specified tests |
|---|---|---|---|---|---|---|
| `c10001` | on | off | off | off | on | 20/20 |
| `c11001` | on | on | off | off | on | 20/20 |
| `c10011` | on | off | off | on | on | 20/20 |
| `c11011` | on | on | off | on | on | 20/20 |

These are four different patches, with behavioral differences outside some
selected inputs, not four operation orderings. None is byte-identical to the
official patch; all passed the specified evaluation. The dataset lists 19
scoring IDs, while the recorded execution expands to 20 parameterized IDs.
Those denominators are not interchangeable.

**Assistance matters:** the coordinator had seen earlier results and the official
repair. The grammar/templates already contained useful repair logic, and an
official compatibility counterexample informed the supplied requirements. The
search did not read the official patch or earlier candidate scores, but this
was not blind repair synthesis. All four patches had already appeared in H29.
The additional evidence is successful requirement-driven selection without
feeding this run's runtime scores back into the search.

## H31: Go Product repair and reversal regression

**Current result: bounded migration regression completed and passed.**
Run: `h31-20261004-01`.

For each of 25 historical SWE-bench cases, the supplied choices were “leave
unchanged” and “apply the known official repair”; the goal excluded the unchanged
choice. A separate H30 context retained its 32 configurations and produced its
four existing candidates. Thus **26 contexts produced 29 arms**, not 29 unique
issues or 25 independently discovered repairs. An *arm* means one candidate's
baseline → patch → reverse evaluation.

| Measurement | Recorded result |
|---|---:|
| Unique SWE-bench cases with known repair fixtures | 25 |
| Additional H30 candidate arms on the same Pylint issue | 4 |
| Checked solver results | 214 = 58 SAT + 156 UNSAT |
| Forward paths / separate model returns | 29 / 29 |
| Completed arms passing required repair tests, tracked-file restoration and reproduced baseline statuses | 29/29 |
| Official scoring IDs summed across the 25 unique cases | 2,874 |
| Consistent runtime-record bindings | 29 `RECORDED_CHECKS_PASS` |
| Initially completed arms / setup or probe stops | 23 / 6 |
| Named continuations / total container attempts | 6 / 35 |
| Actual test-phase commands | 88 |

Each arm used its pinned base commit and container image, the official test
patch, and SWE-bench evaluation scripts/parser/grader. A custom isolated driver
added baseline, imported-source and reversal checks; this was not a direct run
of the stock `run_evaluation` CLI.

The pass criterion covered the recorded FAIL_TO_PASS/PASS_TO_PASS scoring sets.
It does not mean every repository test passed: broader logs retain unrelated
failures/skips. Repeated baseline, patched and restored executions do not
increase the number of independent cases. The Pylint official arm plus four H30
arms each additionally passed all 20 actual parameterized checks.

### Per-case results

Every row passed its specified repair and reversal checks. The final two columns
keep the external runner's complete tracked inventory separate from the Go
binder's portable regular-file subset; they are not the same verification scope.

| Case | Scoring IDs | Runner tracked files | Binder regular files |
|---|---:|---:|---:|
| astropy__astropy-12907 | 15 | 1,876 | 1,876 |
| astropy__astropy-14365 | 9 | 1,870 | 1,870 |
| django__django-10914 | 99 | 6,054 | 6,043 |
| django__django-11848 | 45 | 6,141 | 6,130 |
| django__django-13158 | 30 | 6,268 | 6,257 |
| django__django-13344 | 358 | 6,399 | 6,388 |
| django__django-14089 | 44 | 6,440 | 6,429 |
| django__django-15503 | 80 | 6,616 | 6,605 |
| django__django-15695 | 124 | 6,634 | 6,623 |
| matplotlib__matplotlib-23299 | 193 | 4,451 | 4,443 |
| matplotlib__matplotlib-23412 | 47 | 4,459 | 4,451 |
| psf__requests-2317 | 141 | 128 | 128 |
| pydata__xarray-6992 | 957 | 320 | 320 |
| pydata__xarray-7233 | 190 | 321 | 321 |
| pylint-dev__pylint-8898 | 19 | 3,741 | 3,729 |
| pytest-dev__pytest-10051 | 16 | 570 | 570 |
| pytest-dev__pytest-10356 | 80 | 578 | 578 |
| pytest-dev__pytest-5787 | 125 | 443 | 443 |
| pytest-dev__pytest-7571 | 15 | 490 | 490 |
| scikit-learn__scikit-learn-12973 | 33 | 1,224 | 1,224 |
| scikit-learn__scikit-learn-14087 | 175 | 1,192 | 1,192 |
| sphinx-doc__sphinx-10614 | 6 | 1,660 | 1,654 |
| sympy__sympy-13031 | 10 | 1,444 | 1,444 |
| sympy__sympy-17630 | 21 | 1,693 | 1,693 |
| sympy__sympy-20154 | 42 | 1,813 | 1,813 |

The runner compared tracked-file bytes and modes; the binder checked only safe
relative regular-file entries supported by its contract. Other tracked entries,
including symlinks or names outside that portable subset, remain in the runner's
inventory. This is not a claim that every untracked file, database, network
effect or external resource was restored.

Django13344's `test_coroutine` again showed FAILED → PASSED → FAILED. Earlier H13
already observed that transition. H13's different `test_deprecation` control
was PASSED → PASSED → PASSED and retains its historical control-mismatch result;
H31 does not rewrite it or claim to be the first successful coroutine reversal.

### Product build and evidence continuity

The 214 searches used the frozen initial H31 binary. Runtime-record validation
exposed case-sensitive/Unicode test-ID handling that was subsequently corrected.
The final binary regenerated **all 26 authorities and 29 materializations
byte-for-byte**, then revalidated all 29 runtime records. That compatibility
check added **zero SAT searches and zero repair executions**.

Final build, vet and uncached race checks passed. The limited parity check
connects these saved results to the final product; it is not a proof that the
two binaries behave identically for all inputs.

## How AHE-mcp helps

[AHE-mcp](https://github.com/Yui-Qi-Tang/ahe-mcp) is an optional evidence provider.
Its contribution in these evaluations was to preserve identifiable inputs and
accepted result claims through the public MCP workflow:

1. **Before planning:** save and freshly read back sources with canonical IDs and
   statement hashes. In H30, six admitted records covered a historical source
   root, issue, pinned code, requirements, grammar and interpreter. The modeling
   inputs remained explicitly labeled assumptions; the complete source tree was
   not ingested again.
2. **During verification:** Pouch checks against caller-owned authority and
   pinned source bindings. AHE graph relations preserve provenance; they do not
   automatically become operation rules or validate the formalization.
3. **After verification and approval:** submit a result claim, perform native
   review/admission, and read it back in a new process to check its identity and
   dependency set.

H30's completed publication added four record-source claims and four derived
claims, then freshly read back all four results. Each used seven declared AND
parents: the six original records plus that candidate's runtime record.
Original sources remained unchanged. This publication reused existing runtime
evidence and performed no new repair or SAT search.

H31 separately tested **two fixed models, nine sources, eight derived admissions
and eight fresh readbacks**, covering four model-return and four model-no-return
results. These eight claims are not the 29 SWE runtime arms: **H31 did not freshly
admit every SWE execution result**.

Both workflows used **TEST APPROVAL STUBS in isolated experiment databases**.
AHE stored traceable claims and hash references, not all logs or repository file
bytes. General AHE admission does not itself run Pouch proof validation,
authenticate runtime logs or establish that a source is true. See the
[AHE adapter contract](ahe-adapter.md) for responsibilities and authorization.

## Resolved engineering history

These are completed evaluations with recorded continuations, not current
blockers:

- **H30:** the first invocation rejected `edges:null` before any solver call.
  A documented Lab input normalization enabled the one actual search; the
  status remains `COMPLETED_WITH_INPUT_NORMALIZATION`. H31 later implemented
  and verified the scoped Product fix, preserving original AHE bytes.
- **H30 publication:** the first attempt omitted required title metadata and
  saved no derived result. The separately identified corrected attempt completed
  all four publications; the original partial records remain historical.
- **H31:** five Pylint arms stopped on digest parsing and one SymPy arm on
  import-probe JSON parsing. Startup warnings had contaminated the expected
  output. Six named continuations used corrected parsing with unchanged models,
  patches and test expectations. The status remains
  `COMPLETED_WITH_ENGINEERING_CONTINUATIONS`.
- **H31 race check:** one one-second shell fixture timed out. The unchanged
  suite subsequently passed; this does not establish a unique timeout cause or
  a flakiness fix.

## Composed finite models

Recorded **2026-10-31** on darwin/arm64 with Calx `91319bc` and the pinned CaDiCaL
and DRAT-trim. Each model ran twice; all sizes and counts matched, and times are
indicative. The three models are
private inputs and are not in this repository; these figures cannot be reproduced
from it. All three use forward and return limits of 8 and declare no forbidden
patterns.

Before this change, each model was refused for its declared size. With
`--timeout 9m` for model C and the default for the others, each run finished with
`FOUND` and `complete=true`:

| Model | Fields | Largest field | Domain product | Actions | Reached states | Forward paths | Solver queries | Run time |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| A | 8 | 11 | 105,600 | 91 | 173 | 969 | 1,923 | 59 s |
| B | 10 | 8 | 161,280 | 41 | 167 | 590 | 1,220 | 27 s |
| C | 3 | 115 | 4,140 | 243 | 225 | 414 | 1,215 | 210 s |

| Model | DRAT total | Largest DRAT | Largest CNF | Largest package | `bundle.json` | Run directory |
|---|---:|---:|---:|---:|---:|---:|
| A | 531 MiB | 1.1 MiB | 1.0 MiB | 4.4 KiB | 5.9 MiB | 2.4 GiB |
| B | 147 MiB | 432 KiB | 399 KiB | 6.7 KiB | 4.1 MiB | 761 MiB |
| C | 3.1 GiB | 7.3 MiB | 7.6 MiB | 3.5 KiB | 3.9 MiB | 12 GiB |

Reached states count the forward closure from the start, by a separate script
that shares no code with Pouch or Calx. That script's path counts equal the table,
and the original-rule set comparison in each run also matched. A sampled package
from each run passed `pouch verify`, and each bundle passed `pouch render`. The
reports for all three models loaded in a browser, switched paths and stepped through
replays without console errors or page overflow, including at phone width. Matrix
rows stay about 37 px tall with sticky action and heading cells, the active row
scrolls into view, and model C redraws a step in about 23 ms with one matrix open.
Every path ended as `NO_RETURN_IN_MODEL`. This is a statement about these models,
not about any real system, and more paths do not make a composed model more
plausible.

The reached states are about 0.1–5.4% of the declared products, so the former
domain limit measured the declaration, not the work.

Under the default `--timeout 2m`, model C does not finish. Search stops at 90% of
the timeout so validation can still run, and validation took about 4 s. The run
ended in 112 s with `FOUND`, `complete=false` and exit 2: all 414 forward paths were
verified, 50 had receipts, and the other 364 kept `UNKNOWN` recovery. Before search
stopped early, the same run listed no paths. At `--timeout 20s` it verified the 278
paths found so far. Allow more time for a complete result, and plan for the run
directory: every query keeps its formula and proof.

### Chosen limits

- **128 fields and 4,096 coordinates**; see [Wide models](#wide-models).
- **1,024 actions and 256 values per field** match the Calx encoder, so an
  unsupported model is refused before search. Model C needs 243 actions and a
  115-value field. The clause budget decides first at long horizons; Calx records
  those measurements.
- **65,536 reached states** per closure. One closure took 26–45 ms at 4,096 states
  and 1.1 s with 72 MiB allocated at 65,536 states and 510 actions. The three
  models reach at most 225 states, so this leaves room while keeping a closure
  near one second. It cannot be lower than 4,096, because existing packages must
  still verify.
- **4 MiB packages** did not change. At 101–290 bytes per closed state, a `no_return`
  package holds about 14,000–41,000 states, so that limit can stop first with
  `PACKAGE_SIZE_LIMIT`.

One certification run now shares each closure between packages. A synthetic model
of 16 binary fields reaches exactly 65,536 states:

| Paths | Closures shared | Timeout | Result | Time | Peak memory |
|---:|---|---:|---|---:|---:|
| 16 | No | 4 min | Complete | 58 s | 2.8 GiB |
| 16 | Yes | 4 min | Complete, same path results | 43 s | 359 MiB |
| 226 | No | 2 min | `INCONCLUSIVE`, 29 paths certified | 121 s | Not recorded |
| 226 | Yes | 2 min | `INCONCLUSIVE`, 47 paths certified | 122 s | 396 MiB |
| 226 | Yes | 9 min | Complete | 302 s | 493 MiB |

Model B also gave identical path results with and without sharing. The three
composed models gained little, because their closures are small: A and B changed
by a few seconds, and C still needs more than the default 2 minutes. Each
distinct endpoint still needs its own closures, so a model near the budget with
many endpoints can need a longer timeout.

### Limit checks

Run through `pouch solve` with the pinned tools on synthetic models:

| Case | Result |
|---|---|
| Closure over 65,536 states, with a path | `FOUND`, `complete=false`, `STATE_BUDGET_EXHAUSTED`, exit 2 |
| Closure over 65,536 states, no path within bound | `INCONCLUSIVE`, exit 2 |
| Closure of exactly 65,536 states | `FOUND`, `complete=true`, 3.3 MB package accepted by `verify` |
| Package over 4 MiB | `FOUND`, `complete=false`, `PACKAGE_SIZE_LIMIT`, closure kept |
| 1,024 actions at horizon 1 | `FOUND`, `complete=true` |
| 1,025 actions; 257 values | `MODEL_BOUND`; `FIELD_DOMAIN` |
| Clause budget reached at horizon 2 | Path from horizon 1 kept, `complete=false`, exit 2 |
| Formula over the solver input limit | Query `INCONCLUSIVE` with the limit category, exit 2 |

A seeded differential ran 34 random models with 65–107 actions and domain products
of 4,320–15,750,000 against a separate Python enumeration. All 3,228 forward paths,
bundle statuses and recovery results matched. Models with more than 400 paths were
skipped. On the two Django fixtures, the previous and current binaries wrote
identical input formulas, query statuses and receipts.

`pouch-finite-validation/v1` stays the contract version. Limits are not model
semantics: the recorded packages produce the same receipts, and the frozen
key-rotation formulas are byte-identical. Older binaries refuse wider models.

### Wide models

Recorded **2026-10-31** on darwin/arm64 with Calx `5649f75`, the pinned CaDiCaL and
DRAT-trim, `--timeout 9m` and `--query-timeout 60s`. All models are synthetic and
each ran once, so times are indicative. A 16-field limit refused models that use
one field per on/off option, whatever they cost to encode.

Runs above 128 fields used a test build with the field limit raised to 256.

Each model below has 10 four-valued base fields, 1 stage field, 6 on/off fields and
framed padding fields, with 103 actions, forward limit 8 and 320 forward paths:

| Fields | Result | Queries | Run time | Peak memory | DRAT total | Largest CNF | `bundle.json` | Run directory |
|---:|---|---:|---:|---:|---:|---:|---:|---:|
| 17 | `FOUND`, `complete=true` | 457 | 30 s | 74 MiB | 87 MB | 2.1 MB | 3.5 MB | 1.6 GB |
| 32 | `FOUND`, `complete=true` | 457 | 40 s | 107 MiB | 133 MB | 3.0 MB | 5.2 MB | 2.3 GB |
| 64 | `FOUND`, `complete=true` | 457 | 58 s | 191 MiB | 242 MB | 5.1 MB | 8.9 MB | 3.8 GB |
| 128 | `FOUND`, `complete=true` | 457 | 96 s | 387 MiB | 450 MB | 9.2 MB | 16 MB | 7.0 GB |
| 224, 232, 240, 256 | `INCONCLUSIVE`, `complete=false`, exit 2 | 8–9 | 3–4 s | 231–264 MiB | 13–14 MB | 13–14 MB | 2.2–2.5 MB | 111–137 MB |

The wider runs stopped at the solver's input limit, which is reported as a limit,
not as absence. Calx measured the same limit at 232–240 two-valued fields. It
depends on the model, and the 129–223 range was not measured.

A closure holds one complete state per reached state, so its memory grows with the
field count. These models reach exactly 65,536 states, with one path:

| Fields | Result | Run time | Peak memory |
|---:|---|---:|---:|
| 16 | `FOUND`, `complete=true` | 5 s | 353 MiB |
| 64 | `FOUND`, `complete=true` | 12 s | 1.3 GiB |
| 128 | `FOUND`, `complete=true` | 22 s | 2.5 GiB |
| 256 | `FOUND`, `complete=true` | 43 s | 4.8 GiB |

**128 fields** is the widest value at which the 103-action shape completed under
default limits, and it keeps the worst closure near 2.5 GiB. Calx allows 256, but
256 would nearly double that peak, and the shape above stops at the input limit
well before it. **4,096 coordinates** matches the Calx limit on the dense matrices
built before the encoding budget is checked.

Wider states also fill the 4 MiB package limit sooner. A `no_return` package held
8,192 closed states at 17 fields, 2,048 at 64 and 1,024 at 128. The next size
measured at each width, double the states, ended with `PACKAGE_SIZE_LIMIT` and
`complete=false`, except at 128 fields, where it was not run.

| Check | Result |
|---|---|
| 17 fields against an enumeration sharing no code with Pouch or Calx | Same 320 action sequences; also at 64 fields |
| 17 fields against 64 separate 11-field models, one per option combination | Each separate run `complete=true`; their union equals the same 320 sequences |
| 129 fields; 4,097 coordinates | `MODEL_BOUND` through `pouch solve`; `MODEL_BOUND` in the unit test |
| Sampled packages at 17, 64 and 128 fields | 20 of 320 at each width passed `pouch verify` |
| Key-rotation and Django fixtures, previous and current binary | Same formulas, packages, statuses and receipts; previous packages pass `verify` |
| 128-field report, in a browser | Paths switched and forward/return replay stepped, without console errors or page overflow, including at phone width |
| 17-field report, in a browser | Loaded and stepped once, without console errors or page overflow |

At 128 fields the guard and effect matrices have 283 columns inside a scrolling
box, with rows about 37 px tall and sticky headings. A step redrew in 276 ms with
all three matrices open.

The five private models that motivated this limit were not run. The separate-run
workaround remains valid for them, but the agreement above is shown only for a
synthetic model of the same shape.

## What remains unmeasured

These evaluations do not measure a blind SWE-bench solve rate, improvement over
Sol alone, token/time savings, arbitrary operation invention, complete
natural-language problem coverage or recovery of every external side effect.
There is no controlled performance comparison here.

The demonstrated product value is a reproducible connection from **explicit
evidence and choices → checked combinations and paths → materialized patches →
separately observed repair/reversal records**. Semantic completeness still
depends on the supplied requirements and models. The problem-specific H30
interpreter was not ported into the generic Go core.

## Evidence identities

Product-facing descriptions use study names instead of internal run labels.
The labels on this page and in frozen test evidence remain for migration
traceability. Evidence digests below are unchanged.

The Product history was cleaned up on 2026-10-11. Product commit IDs below
identify the original, pre-cleanup experiment snapshots retained in the
separate history backup and Lab source records; they may no longer resolve
in the rewritten Product branch. This does not relabel the binaries that ran
the experiments or claim that the current source has their exact hashes.

These are Lab archive identifiers, not files bundled with the Product or
download links. Digests identify the exact original bytes used for this summary;
they do not replace obtaining and checking the underlying artifacts. Shared
exports that redact private paths may have different digests.

The Lab report checkpoint is
`f07a7ced5299f9f4679d682c46cfd0e410a5dff1`. Lab repository-relative identifiers
below are under its `pouch/` subtree. The Product migration is included in
`d71d15c3a6de776fe4b8fddcdca10d6af6ac47f6`; the experiment itself used a worktree
based on `480aec39fbce2cca1ca4abbe3780f4fae3c00eca`, with frozen source identities
and the final binary SHA-256
`e80631b9e73eaae1731c8afaf65b6fe40ca382f7a9461010443d23ebbd511a48`.
These Product hashes are the identities recorded by the runs. A later author
rewrite of the published history changed them: the same commits are now `3237c49`
and `30c2967`, with the same content.

- **H30 search counts**: `experiments/h30/runs/h30-20261003-01/summary.json`
  SHA-256: `9a25addcbbc33a711b876d41851eedf6ca8868f7db1001509dd82650bd35cee4`.
- **H30 actual repair/reversal arms**: `experiments/h30/runs/h30-20261003-01/runtime/summary.json`
  SHA-256: `23f31b5b8bd372248e9ff50fa2ec5ad994e5c0629c55f6ab00b8b5a32913b1b7`.
- **H30 completed AHE publication**: `experiments/h30/publications/h30-publication-20261004-02/summary.json`
  SHA-256: `cfdfd7cbf0f93fd8a4986080e868ef641484b005c2cd5f30bcfcc64c745e6f8f`.
- **H31 totals and completion status**: `experiments/h31/runs/h31-20261004-01/summary.json`
  SHA-256: `f8a454af40786f8fdf4de0c1831ea5ffc620f7292d38c4eb37fa7c2ef0a3507c`.
- **H31 case roster, base commits, images and scoring IDs**: `experiments/h31/runs/h31-20261004-01/roster.json`
  SHA-256: `d1c253a9644233db47b783424c1a9dcb081e67a32b0112fd2bbe967b1b9c9c55`.
- **H31 per-arm runtime bindings and inventory counts**: `experiments/h31/runs/h31-20261004-01/runtime-binding-summary.json`
  SHA-256: `74998f3c56ae233807fedc0ec3675a677e2c8fc0d0290ede935c3f6c2d64a693`.
- **H31 final authority/materialization parity**: `experiments/h31/runs/h31-20261004-01/final-product-parity.json`
  SHA-256: `d300902a9420fd3eab61fd4bf9b5ca3bb614e5abe210d37e84a39af0d7cba31f`.
- **H31 final source and binary identity**: `experiments/h31/runs/h31-20261004-01/final-product-source-identity.json`
  SHA-256: `21582feb7bb780bd7d6b8870047c1d198ad49f549371625f6a325db32d9e1ce3`.
- **H31 separate native integration**: `experiments/h31/runs/h31-20261004-01/native/publication-summary.json`
  SHA-256: `e3709290e4b1c26876d91478b3977d5acf493d4f1b4a3a4d13fbb1de16ea617d`.

The curated tables were checked against these saved JSON records and the H30/H31
reports when preparing this page. No solver, checker, container or native
admission was rerun for this documentation update. For current feature
boundaries see the [architecture](ARCH.md); for optional evidence retrieval see
[archive handling](artifacts.md).
