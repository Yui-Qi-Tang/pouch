@AGENTS.md

# Claude Code entry point

Use `/pouch` for Pouch planning, saved-result inspection and model-package verification. The project skill is `.claude/skills/pouch/SKILL.md`; its portable CLI guide is in `references/workflow.md` beside it. Load the guide when performing that workflow, rather than importing it into every unrelated coding task.

Use the shared project instructions above for implementation work. Run the skill in the main conversation; it does not delegate the work, install binaries, grant tool permissions or enable autonomous evidence admission. No local machine paths or global Claude settings are required by these files.

## Generate and deliver reports

Use the [CLI workflow](.claude/skills/pouch/references/workflow.md) for complete input requirements and commands. Use an approved `pouch` executable, or build this checkout and use `bin/pouch`.

- **New authorized search:** run `pouch solve` with the caller-held authority, source files and pinned solver/checker tools, using a new output directory. When the run reaches report generation, it writes `index.html` for human replay and `bundle.json` for AI inspection. An early failure may produce neither; report the error and preserve available output.
- **Existing result:** render the saved bundle without rerunning search:

  ```sh
  pouch render --bundle new-result/bundle.json --out replay.html
  ```

  The output HTML file must be new. Rendering displays saved claims; it does not revalidate them or execute the planned actions. Keep the original result directory and its referenced evidence available.
- **Delivery:** link the generated HTML and JSON, plus relevant checked packages and receipts. State the request ID, authority digest, actual run or inspection mode, forward-path completeness, separate return/no-return result and cost, assumptions and any failures. Describe costs as model-operation counts unless execution evidence supports another meaning.
- **Display validation:** distinguish HTML generation, automated content checks and actual browser interaction. Do not claim that path switching, forward/return replay controls or layout passed browser acceptance unless those interactions were performed and checked.

Keep outputs in ignored run directories, preserve prior reports and manifests, and remove credentials and private host paths from material shared externally. Report generation does not authorize AHE publication, commit or push.

## Candidate workflow

Follow the shared candidate rules in [AGENTS.md](AGENTS.md#candidate-workflow)
and the [CLI workflow](.claude/skills/pouch/references/workflow.md#declared-repair-candidates).
Use `candidate-prepare` → ordinary `solve` → `candidate-materialize`; a separately
authorized external runner supplies observations for `candidate-bind-runtime`.
The ordinary HTML shows model selection/recovery. Deliver the materialization
and runtime binding separately when explaining actual repair evidence.
