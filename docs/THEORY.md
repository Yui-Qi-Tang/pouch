# Pouch: Graphs, Matrices, SAT, and Verifiable Evolution

Document version: `theory.2`; 2026-10-04. This document applies to the finite, deterministic model with complete state described in [ARCH](ARCH.md).

This document defines the mathematical objects and validation obligations the product must follow. The Go product implements finite path/closure validation, graph projection, matrix/CNF encoding, SAT enumeration and HTML/JSON presentation. It also compiles caller-declared finite candidate spaces and binds materialized patches to external runtime records; it does not derive semantic constraints from raw evidence. The definitions, short derivations, and references below do not constitute a general correctness proof for the new encoder.

## 1. Three distinct graph concepts

1. **Evidence graph E**: nodes are records or statements, and edges are typed, source-attributed relations. Complete derivation parent sets are retained separately.
2. **State graph T**: nodes are complete simulated states, and edges labeled with action IDs are legal one-step operations.
3. **Witness path π**: a concrete sequence of states and actions in the state graph.

Supports/derived relations in E do not automatically become guards/effects in T. An explicitly declared model and source-to-rule mapping connect the two; matrix entries cannot invent undeclared actions. Pouch searches for paths in T under that declared basis. The current v1 contract binds the complete selected source set and assumptions to the authority; it does not encode per-action or per-guard source-to-rule mappings. Such granular attribution must not be inferred from evidence edges.

## 2. Evidence graph → matrices

For a fixed snapshot with n nodes and m edges, establish a stable, reversible ID-to-index mapping and define source/target incidence:

```text
S[i,e] = 1  iff node i is the source of edge e
T[i,e] = 1  iff node i is the target of edge e
A_r = S · diag(mask_r) · transpose(T)    (Boolean OR/AND)
A_r[i,j] = 1  iff the snapshot contains at least one r edge: i → j
```

Every existing edge has exactly one source and one target, and both endpoints must exist. Separate source/target incidence preserves self-loops; a single signed incidence matrix may cancel them out. Boolean adjacency collapses parallel edges with the same endpoints, so A_r alone cannot reconstruct the original graph.

The complete projection bundle also retains node presence, edge IDs, records/payloads, provenance, temporal/integrity data, relations, derivations, scope, coverage, direction semantics, and the original snapshot bytes/digest. Reconstruction must derive endpoints and relations from the matrices and cross-check them against the records. Returning an unchanged sidecar alone does not establish a matrix round trip.

For a specified relation and direction, Boolean `A_r^k[i,j]` indicates whether a **walk** of exactly k edges exists. It does not count simple paths or establish the truth of a derived statement. Replacing entries with edge multiplicities and using integer addition/multiplication counts walks under that representation, not independent pieces of evidence.

GraphBLAS provides background on expressing graph algorithms through explicitly defined algebraic operations. Pouch may use the same idea, but currently has no GraphBLAS library dependency or validated performance benefit. See the [official GraphBLAS specification, Introduction](https://graphblas.org/graphblas-api-cpp/).

### Complete AND-parent basis

Let H[i,d]=1 mean that node i is a required parent of derivation d, and let k_d be its complete parent count. If x gives known 0/1 parent availability:

```text
parents_complete(d) iff sum_i H[i,d] * x[i] = k_d
```

This checks only that the parent set is complete. It does not establish the target's truth or turn an evidence parent into an action prerequisite. A missing parent manifest or unknown availability must not be treated as zero or used to drop a parent condition; unknown status must be retained separately.

### Comparing snapshots

Align matrices using the union of stable IDs and retain presence separately. Additions/deletions can be determined only when scope and coverage are comparable. An edge absent from a read must not simply be filled with zero and treated as evidence of deletion. Reordering indexes must not change conclusions. `truncated=false` describes only that bounded read, not global completeness.

## 3. Finite action model

Let the fields be f_1…f_n, each with a finite domain D_i. A complete state s assigns a precisely typed value to every field. The legal state set is:

```text
S_model = {s in D_1 × … × D_n | Domain(s)}
action a = (guard_a, effect_a, frame_a, cost_a)
T(s,a,s') iff s,s' are legal, guard_a(s) holds,
              s' matches effect_a exactly and preserves every field in frame_a
```

Actions are deterministic in this phase. Each field belongs to exactly one of effect or frame, with no omissions or overlaps. Current v1 supports only string enumerations, exact conjunctive guards, constant effects, and unit costs. Numbers, Booleans, and strings must not be interchanged because they look alike; null, unknown, and omitted values are also distinct. Unsupported input must be rejected rather than silently coerced.

The model must contain enough information to determine subsequent behavior. If history, resource identity, or consumed permissions affect a guard, they belong in the state. States must not be merged merely because their visible presentations match.

## 4. Action matrices and evidence adjacency

Encode each `(field,value)` pair as a one-hot coordinate X[f,v], with exactly one value per field. The representation may include:

- G[a,(f,v)]: whether action a permits this field value; unconstrained fields allow all values, with guard presence recorded separately.
- E[a,(f,v)]: whether action a sets f to v.
- F[a,f]: whether action a preserves f.
- Action IDs/targets, field/value indexes, types, and source mappings.

Given X, an action is enabled only when the permitted-value condition for every relevant field holds. AND must not be replaced with "at least one condition holds." For multivalued fields, define membership within each field first, then take the conjunction across fields.

Data round trips and behavioral equivalence are separate obligations. Original text and IDs may survive while computation replaces AND with OR. A finite model allows complete comparison of every `(s,a,s')` to detect extra illegal edges or missing legal edges. If both approaches share the same faulty conversion function, agreement can still reflect the same error; the direct evaluator therefore does not read the matrix encoder's answers.

## 5. Matrices/model → CNF → SAT

For exact length h, introduce state variables X[t,f,v] and action-selection variables U[t,a]. The conceptual formula is:

```text
Φ_h = Init(X_0)
      ∧ Domain(X_0)…Domain(X_h)
      ∧ ∧_{t=0}^{h-1} Transition(X_t,U_t,X_{t+1})
      ∧ Goal(X_h)
      ∧ Policy(X_0…X_h,U_0…U_{h-1})
```

The CNF must encode at least exactly one value per field, exactly one action per step, guard implications, effects, frame equality in both directions, forbidden patterns, and the original init and Goal. One-hot exactly-one constraints may use one at-least-one clause plus pairwise at-most-one clauses. Other equivalent encodings must retain variable mappings and undergo validation.

For example, if feed requires permit and water_ok, the SAT literal u selecting feed implies at least `¬u ∨ permit` and `¬u ∨ water_ok`. The effect `fed'=true` and frames for other fields require separate clauses. Evidence-graph edges alone do not establish these implications.

For background on bounded unrolling and SAT, see [Biere et al., *Bounded Model Checking*, author-hosted full text](https://cca.informatik.uni-freiburg.de/papers/BiereCimattiClarkeStrichmanZhu-Advances-58-2003-preprint.pdf). UNSAT at a single h excludes only that encoded scope. Claiming no path across 0…H requires completion at every length or a separately validated at-most-H encoding. Without idle actions, UNSAT at a longer exact length cannot exclude shorter solutions.

The encoder's obligations are that every path satisfying the contract can be encoded as a solution (no missing paths), and every solution decodes to a legal path (no extra paths). These are specification requirements, not a claim that the new Go encoder already has a general proof.

## 6. Multiple paths, identity, and completeness

With the deterministic model and init fixed, this phase identifies a path by its **complete sequence of action IDs**. Different targets are different action instances. Equal endpoints, lengths, or costs do not make two paths identical.

After obtaining an h-step witness `(a_0,…,a_{h-1})`, add a blocking clause:

```text
¬U[0,a_0] ∨ … ∨ ¬U[h-1,a_{h-1}]
```

This excludes only that action sequence, not the entire endpoint. After the unique empty sequence at h=0 is processed, add an empty clause or explicitly mark that cell complete. Differences in auxiliary SAT variables must not count as new paths. Introducing nondeterminism in the future requires revisiting path identity rather than applying this blocking rule unchanged.

The default research policy is simple first-hit: no repeated complete state, and termination on the first Goal visit. If init already satisfies Goal, the empty path is valid; leaving Goal to manufacture another path is not allowed. This policy does not enumerate all walks containing loops.

For each tested length, retain every verified solution and UNSAT evidence for the final blocked formula. Stopping at a solution-count or time limit means the result is partial. The final UNSAT must apply to the original problem plus clauses blocking exactly the paths already listed. Overblocking cannot establish completeness even when its proof is valid.

Direct DFS set comparisons on small models can detect overblocking, missing paths, extra paths, and duplicates. Per-path receipts validate members, not set completeness.

If the complete model has exactly N legal states and contains all information affecting future behavior, every reachable goal has a simple path of at most N−1 steps: a cycle between repeated states can be removed while leaving the suffix executable. This bound can support reachability decisions for that model. It cannot be assumed when H is below N−1, and N must not be replaced with the node count of a bounded evidence view. Enumerating every simple path still does not enumerate every walk that allows repetition.

### Finite candidate combinations

The optional candidate compiler treats a candidate as a tuple choosing one
option from each declared group. It selects groups in a fixed order, then uses
`submit_candidate` to reach the goal. A declared forbidden partial assignment
prevents submission whenever the complete tuple matches it. Completed search
and set checks cover the legal tuples of this supplied space under the query
bounds; they do not cover edits absent from its options. The fixed selection
order deliberately excludes permutations of the same choice tuple.

Literal templates map a verified tuple to file contents. The complete space
digest binds choices, constraints, source references and rendering text into
the authority. This closes a byte-identity boundary, not a semantic proof that
the tuple repairs the original program. An external runner must test the patch.
The generated return withdraws selections in reverse group order; it describes
selection-state recovery, not the execution of a reverse patch.

## 7. Step-by-step replay and negative evidence

The receiver obtains the caller-confirmed sources, model, and query outside the candidate package, then checks:

1. The request, source-byte digests, model, assumptions, and search policy match.
2. Every state is complete and legal, and the starting state is the original init.
3. Guards, effects, and frames are evaluated under the original rules; the complete successor must match.
4. The endpoint satisfies Goal, and length, cost, simple-path, and first-hit requirements satisfy the contract.
5. A shortest-path claim has an independent cost check, rather than relying on the solver's chosen path.

Clause-by-clause SAT assignment checking establishes that the assignment satisfies this CNF. Replay under original rules establishes that the path answers the current query. An UNSAT proof establishes that this CNF has no solution; faithful encoding of the original problem still requires a separate check. DRAT-trim takes both a formula and a proof as inputs, making this distinction essential when using it. See [Wetzler, Heule, and Hunt, *DRAT-trim*, §§3–4](https://www.cs.cmu.edu/~mheule/publications/drat-trim.pdf).

Tool identity, version, binary digest, exit code, exact success marker, and timeout are part of the acceptance contract. A checker crash or a mismatch between the success string and exit code means validation is incomplete, not PASS. A repaired execution uses a new run while preserving history; it does not rewrite the old run.

## 8. A return path is more than a reversed action list

The legality of forward path `π: s_init → s_end` and the existence of return path `ρ: s_end → baseline` are different propositions. Every reverse operation must be explicitly declared in the original model and enabled at the state where it is used. A file checkpoint restore that is merely a model assumption must not be reported as physical restoration already performed.

A return starts from the complete s_end and checks every baseline field. Restoring only the fields selected by projection P must be described as `P(s)=P(baseline)`, not full-state restoration. Forward and return paths each use first-hit semantics. Their concatenation may revisit earlier states, so the entire round trip cannot also be required to remain simple.

For a complete, finite, fixed T, repeatedly add every enabled successor from s_end until reaching a fixed point:

```text
R_0 = {s_end}
R_{k+1} = R_k ∪ {s' | exists s in R_k, a: T(s,a,s')}
R = fixed point
```

A no-return claim within the model requires the baseline to be absent from R. Every legal path from s_end starts in R, and R is closed under legal successors, so every state along such a path remains in R. The current validator additionally requires the candidate's closed_states to equal the complete reachable set independently reconstructed by BFS; omissions, duplicates, and extraneous states are rejected.

Failure to find a return within the return horizon does not prove no return: a path may exist at h+1. Even with a valid bounded UNSAT proof, the negative claim must be rejected if the complete reachable set contains the baseline. This check is already implemented in the v1 validator.

## 9. Distance, cost, and evolution views

For structural and field differences, first align IDs, types, and scope, then separately calculate changes in node presence, edge identity, relation adjacency, payloads, and state fields. A one-hot value change flips two bits but changes only one field; bit counts must not be reported as action counts.

Return cost is the minimum cost over legal return paths:

```text
D_return(s,b) = min_{legal ρ: s→b} sum cost(a)
```

Infinity is justified only after complete unreachability has been verified; incomplete search requires unknown. The cost may be asymmetric and is not generally a metric. Current unit-cost validation uses BFS shortest lengths. Future nonnegative-cost shortest paths or min-plus methods require a separate language definition and acceptance testing.

If the complete state-transition graph is known, let W[i,j] be the lowest one-step cost, with infinity for absent edges. Min-plus closure with the zero-step identity describes shortest costs. It does not eliminate state explosion or replace an action witness.

HTML evolution views are generated from saved states/actions, displaying guards, changed fields, frames, baseline differences, and source justifications at each step. Evidence support, model declarations, and state transitions require distinct edge types. Visual proximity is not a native evidence relation.

## 10. Uses and limits of the linear state equation

Only when each operation can be faithfully expressed as a fixed increment `x'=x+b_a` may the columns of B be defined as b_a, yielding the necessary condition `x_target=x_init+B n` for a legal sequence, where n contains nonnegative integer action counts. This equation does not preserve guards or ordering.

For example, consider a finite integer submodel x∈{0,1,2} with a single action a, guard x≥1, effect x'=x+1, and a requirement that the successor remain in the domain. For x=0 to x=1, choosing n_a=1 satisfies 1=0+1. However, a is disabled at the start, so no such legal path exists. This numeric language is illustrative and is not a type supported by the current v1 validator.

An invariant `qᵀB=0` that holds for every legal step can provide a conservation law; a goal that violates it can be excluded. The converse does not generally hold. The Petri-net state equation is usually only a necessary condition for reachability; equivalence in particular subclasses requires additional assumptions. See [Hujsa et al., *Checking marking reachability with the state equation in Petri net subclasses*, arXiv:2006.05600v1, §1](https://arxiv.org/html/2006.05600v1).

This project has not established a Petri-net subclass mapping or ILP cost equivalence. These are not blockers for the current product, and reset operations absent from the original problem must not be added merely to apply a theorem.

## 11. Supported claims

| Check | Supported claim | Unsupported extension |
| --- | --- | --- |
| Record round trip | Selected records and identities are preserved | The original text was interpreted correctly |
| Complete one-step relation comparison | The tested transformation agrees for this finite model | Every possible encoder input is handled correctly |
| Assignment/proof | The specified formula's conclusion has corresponding evidence | The formula necessarily expresses the original problem |
| Replay under original rules | The path satisfies the current model | The result is necessarily recoverable or the original problem is fully covered |
| Return path/complete closure | The result is recoverable or non-recoverable within the model | Real resources have been restored |
| Complete-set comparison/enumeration exhaustion evidence | The set is complete within the specified policy and scope | All unknown actions or arbitrary program solutions are covered |
| Materialization | The checked tuple rendered deterministically against pinned baseline files | The patch executed or repairs the original problem |
| Runtime-record binding | Supplied artifacts and observations meet the declared hash/test/file checks | Logs are authentic, the parser is correct, or undeclared state was restored |
| External admission/readback | The external system persisted the specified content | The external system necessarily ran the Pouch validator |

Matrix precision cannot supply omitted original requirements. Source-to-rule and Goal coverage must separately identify supported items, assumptions, and omissions. PASS within a model does not establish that the entire original problem has been solved.

## 12. A small example for manual verification

Take three fields `(permit, water_ok, fed)`, each with enumerated string values `0`/`1`. Init and baseline are `110`. The feed guard is `permit=1 ∧ water_ok=1 ∧ fed=0`, its effect is `fed=1`, and its frame preserves the other two fields. Goal is `fed=1`.

The legal forward path is `110 --feed--> 111`. If AND is encoded as OR and a feed witness is found from `100`, replay under the original guard must reject it. `100` belongs to a different query; the starting state must not be swapped without changing query identity.

If the model has no action that clears fed, the complete closure from `111` is `{111}`. The baseline is absent, supporting no return within the model. A model return `111→110` exists only after a declared `reset_fed` action is separately approved and added; wanting to return does not authorize inventing that action. This example is explanatory, not a new experimental result.

## 13. Method references

The following sources provide methodological background and limits of applicability. They were consulted on 2026-09-29 and do not certify Pouch's implementation.

- GraphBLAS, *C++ API Specification v1.0*, official specification, Introduction. [Original specification](https://graphblas.org/graphblas-api-cpp/). This project draws only on its approach to explicitly defined matrix algebra.
- Armin Biere, Alessandro Cimatti, Edmund M. Clarke, Ofer Strichman, and Yunshan Zhu, *Bounded Model Checking*, 2003. [Author-hosted full text](https://cca.informatik.uni-freiburg.de/papers/BiereCimattiClarkeStrichmanZhu-Advances-58-2003-preprint.pdf). Background on finite-transition unrolling and SAT.
- Nathan Wetzler, Marijn J. H. Heule, and Warren A. Hunt, Jr., *DRAT-trim: Efficient Checking and Trimming Using Expressive Clausal Proofs*, SAT 2014. [Author-hosted full text](https://www.cs.cmu.edu/~mheule/publications/drat-trim.pdf). Proof checking for UNSAT claims about a specified CNF.
- Thomas Hujsa, Bernard Berthomieu, Silvano Dal Zilio, and Didier Le Botlan, *Checking marking reachability with the state equation in Petri net subclasses*, 2020, arXiv:2006.05600v1. [Pinned-version full text](https://arxiv.org/html/2006.05600v1). The distinction between state equations and legal reachability.

Provider boundaries, identity bindings, data packages, and separate acceptance checks in this document are Pouch design requirements, not conclusions proved for this product by the cited literature.
