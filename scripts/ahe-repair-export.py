#!/usr/bin/env python3
"""Audit/export the fixed Django repair publication fixture; never rerun MCP."""
import argparse
import base64
import gzip
import hashlib
import json
from pathlib import Path
import subprocess

p = argparse.ArgumentParser()
p.add_argument('--work', required=True, type=Path)
p.add_argument('--project', required=True, type=Path)
p.add_argument('--ahe-repo', required=True, type=Path)
p.add_argument('--out', required=True, type=Path)
a = p.parse_args()
w, project, out = a.work.resolve(), a.project.resolve(), a.out.resolve()
load = lambda path: json.loads(path.read_bytes())
sha = lambda raw: hashlib.sha256(raw).hexdigest()
summary = load(w/'repair/summary.json')
cfg = load(w/'native-config.json')
plan = load(w/'frozen/plan.json')
if summary['status'] != 'PASS' or cfg['schema_version'] != 'pouch-disposable-native/v1':
    raise SystemExit('completed isolated fixture required')
if (w/'pgdata/postmaster.pid').exists():
    raise SystemExit('stop owned isolated cluster before export')
original = project/plan['input_run'] if plan['input_run'].startswith('artifacts/') else project/'artifacts'/plan['input_run']
checks = []

def check(name, ok):
    if not ok:
        raise RuntimeError(name)
    checks.append(name)

check('original manifest remains pinned', sha((original/'manifest.json').read_bytes()) == plan['manifest_sha256'])
for row in load(original/'manifest.json')['files']:
    raw = (original/row['id']).read_bytes()
    check('original/' + row['id'], len(raw) == row['bytes'] and sha(raw) == row['sha256'])
check('complete original inventory', len(list(p for p in original.rglob('*') if p.is_file())) == 206)
build = load(w/'build-identity.json')
ahe_patch = subprocess.check_output(['git', 'diff', '--binary', 'HEAD'], cwd=a.ahe_repo)
check('native checkout unchanged', sha(ahe_patch) == build['checkout']['patch_sha256'])
check('native HEAD unchanged', subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=a.ahe_repo, text=True).strip() == build['ahe_head'])
for name, expected in build['binaries'].items():
    check('native binary ' + name, sha((w/'bin'/name).read_bytes()) == expected)
freeze = load(w/'frozen/manifest.json')
check('integration test binary', sha((w/'bin/pouch-ahe.test').read_bytes()) == freeze['test_binary_sha256'])
for name, row in freeze['source_files'].items():
    raw = (w/'frozen'/name).read_bytes()
    check('frozen/' + name, sha(raw) == row['sha256'] and len(raw) == row['bytes'])
source_map = load(w/'repair/source-map.json')
source_manifest = load(original/'model/sources.json')
original_sources = {x['id']: (original/'model'/x['file']).read_bytes() for x in source_manifest['sources']}
source_records = sorted((w/'repair').glob('*-source.json'))
check('nine complete admitted source records', len(source_records) == 9)
execution_source = None
for path in source_records:
    r = load(path)
    record = r['readback']
    statement = record['statement'].encode()
    check(path.name + '/statement', sha(statement) == r['envelope_statement_sha256'] == record['statement_sha256'])
    env = json.loads(statement)
    raw = gzip.decompress(base64.b64decode(''.join(env['chunks']), validate=True))
    expected = original_sources.get(env['origin_id'])
    if expected is None:
        check('exact execution origin ID', env['origin_id'] == 'artifacts/go-repair-20260929-01/replay/result.json')
        expected = (original/'replay/result.json').read_bytes()
        execution_source = record['canonical_id']
    check(path.name + '/raw', raw == expected and sha(raw) == env['raw_sha256'] == r['raw_sha256'] and len(raw) == env['raw_bytes'])
    check(path.name + '/native ID', r['admission']['canonical_ref'] == record['canonical_id'] and record['raw']['statement_text'] == record['statement'])
parents = sorted(x['canonical_id'] for x in source_map)
check('eight distinct model parents', len(parents) == len(set(parents)) == 8)
check('source-map hash', sha((w/'repair/source-map.json').read_bytes()) == load(w/'repair/input-binding.json')['source_map_sha256'])
report = load(original/'replay/result.json')
for row, path in zip(summary['paths'], report['paths'], strict=True):
    pid = row['path_id']
    check(pid + '/original path', pid == path['id'] and row['original_package_sha256'] == path['package_sha256'])
    check(pid + '/original package', sha((original/'model/packages'/f'{pid}.json').read_bytes()) == path['package_sha256'])
    check(pid + '/native package', sha((w/'repair'/f'{pid}-package.json').read_bytes()) == row['native_package_sha256'])
    model = load(w/'repair'/f'{pid}-model-publication.json')['publication']
    binding = load(w/'repair'/f'{pid}-execution-publication.json')
    model_back = load(w/'repair'/f'{pid}-model-final-readback.json')
    execution_back = load(w/'repair'/f'{pid}-execution-final-readback.json')
    for name, receipt, back, expected_parents in [
        ('model', model['admission'], model_back, parents),
        ('execution', binding['admission'], execution_back, sorted([model['canonical_id'], execution_source])),
    ]:
        check(pid + '/' + name + '/native receipt and parents', back['raw']['endpoint_admission'] == receipt and receipt['admission']['ParentNodeIDs'] == expected_parents and receipt['admission']['CanonicalRef'] == back['canonical_id'] and receipt['admission']['AdmissionOutcome'] == 'admitted')
        check(pid + '/' + name + '/kind', back['raw']['canonical']['node_kind'] == 'derived_claim')
        check(pid + '/' + name + '/statement', back['raw']['statement_text'] == back['statement'] and sha(back['statement'].encode()) == row[name + '_statement_sha256'])
    claim = json.loads(execution_back['statement'])
    check(pid + '/execution claim bindings', claim['execution_report_sha256'] == plan['execution_report_sha256'] and claim['original_package_sha256'] == path['package_sha256'] and claim['native_package_sha256'] == row['native_package_sha256'] and claim['source_map_sha256'] == sha((w/'repair/source-map.json').read_bytes()) and claim['original_authority_sha256'] == plan['authority_sha256'] and claim['initial'] == path['initial'] and claim['patched'] == path['patched'] and claim['final'] == path['final'])
    check(pid + '/claim distinction', not claim['observed_fact'] and not claim['new_execution'] and claim['conclusion_kind'] == 'recorded_file_replay_binding' and not model['validation']['observed_fact'])
check('six rows', len(summary['paths']) == 6)
check('zero new execution/search/tests', summary['new_sat_queries'] == summary['new_patch_operations'] == summary['new_official_tests'] == 0)

out.mkdir(parents=True, exist_ok=False)
def emit(name, raw):
    # Generic forbidden prefixes only; never put a real workstation path here.
    scan = raw
    if str(name) == 'frozen/pouch/scripts/ahe-integration-bootstrap.py':
        # Preserve the exact frozen source, including its generic DSN template.
        scan = scan.replace(b'postgres://{user}:{pwd}@127.0.0.1:{port}/pouch_native?sslmode=disable&connect_timeout=3', b'GENERIC_DSN_TEMPLATE')
    for marker in [b'/Users/', b'/home/', b'/private/tmp/', b'/tmp/', b'postgres://', b'postgresql://']:
        if marker in scan:
            raise RuntimeError('private path or DSN in selected export: ' + str(name))
    dest = out/name
    dest.parent.mkdir(parents=True, exist_ok=True)
    with dest.open('xb') as stream:
        stream.write(raw)
def emit_json(name, data):
    emit(Path(name), (json.dumps(data, ensure_ascii=False, indent=2) + '\n').encode())
for path in sorted((w/'repair').rglob('*')):
    if path.is_file(): emit(Path('native')/path.relative_to(w/'repair'), path.read_bytes())
for path in sorted((w/'frozen').rglob('*')):
    if path.is_file(): emit(Path('frozen')/path.relative_to(w/'frozen'), path.read_bytes())
for name in ['build-identity.json', 'repair-native.log']:
    emit(Path(name), (w/name).read_bytes())
emit_json('schema-pins.json', cfg['pins'])
for kind in cfg['pins']:
    emit(Path(kind+'-discovery.json'), (w/(kind+'-discovery.json')).read_bytes())
selected = ['model/authority.json','model/bundle.json','model/sources.json','replay/result.json','replay/manifest.json','manifest.json','frozen/plan.json','frozen/fixture/provenance.json','frozen/fixture/history/restore/summary.json']
selected += [str(p.relative_to(original)) for p in sorted((original/'model/packages').glob('*.json'))]
selected += ['model/' + x['file'] for x in source_manifest['sources']]
for name in selected:
    emit(Path('original')/name, (original/name).read_bytes())
emit_json('audit.json', {'status':'PASS','mode':'read-only artifact audit; no MCP, solver or patch execution','checks':checks,'count':len(checks),'cluster_stopped':True,'ahe_checkout_unchanged':True,'limits':'Original inventory is verified in the repository; only selected original files are copied here. Native application payloads and public receipts are retained; private launchers, credentials, DB files and executables are omitted. This is not a second physical execution.'})
rows = '\n'.join(f"| {r['path_id']} | `{r['model_canonical_id']}` | `{r['execution_binding_canonical_id']}` | 6 |" for r in summary['paths'])
report_md = f'''# Django 13344: recorded repair connected to AHE

Run `{cfg['run_id']}` completed **PASS** on 2026-09-29 (Asia/Taipei).
The latest three-file execution record from `go-repair-20260929-01` is now
bound to original sources, model, packages and per-step file hashes/modes, saved
through public AHE MCP, and freshly read back. The isolated cluster is stopped.

## Completed scope

| Item | Count |
|---|---:|
| Original source artifacts admitted and losslessly read back | 8 |
| Complete existing execution report admitted and losslessly read back | 1 |
| Original / identity-rebound packages revalidated | 6 / 6 |
| Model conclusions saved as native derived claims | 6 |
| Recorded execution bindings saved as native derived claims | 6 |
| Final fresh result readbacks | 12 |
| Existing file events matched to original rules and checkpoints | 36 |
| New SAT queries / patch operations / official tests | 0 / 0 / 0 |

This is one Django problem, six orderings of one known three-file repair.
Every path retains three apply and three separately planned reverse operations.
The new counts concern evidence binding and persistence; they do not add new
repair executions to the prior 18 apply and 18 reverse operations.

## Evidence chain and hashes

```text
8 exact original byte artifacts -> 8 admitted lossless source envelopes
  -> identity-rebound authority/packages -> independent model validation
  -> 6 model derived claims (all 8 source envelopes are AND parents)

complete original execution report -> admitted lossless report envelope
  + each matching model derived claim -> 6 recorded execution binding claims
  -> final fresh Query of all 12 claims, exact statement/receipt/parent checks
```

Original authority SHA-256: `{plan['authority_sha256']}`.
Original bundle SHA-256: `{plan['bundle_sha256']}`.
Original execution report SHA-256: `{plan['execution_report_sha256']}`.

The report preserves the actual original request/package identities. New native
IDs and wrapper statement hashes have an explicit bijective source map. Only
request/authority/source identity fields and corresponding basis IDs are rebound;
undoing that rebind restores the original contract. Actions, guards, effects,
frames, full baseline, Goal, bounds and every trace are unchanged. There is no
claim that the original physical execution used these newly created native IDs.

[`native/input-binding.json`](native/input-binding.json),
[`native/source-map.json`](native/source-map.json), and each path's two publication
records link original hashes, native package hashes and canonical IDs. The full
execution report includes each file's initial/patched/final hashes, mode and all
36 event snapshots; its exact bytes are both copied under `original/replay/` and
recoverable from the native report envelope.

## Why lossless envelopes

AHE's default line citations and ancestor loader have bounded capacities. The
original pretty-printed model/checkpoint JSON and source files are preserved as
`pouch-lossless-artifact/v1`, gzip + base64 in bounded chunks. No source bytes or
parents are dropped. Decode length and SHA-256 are checked before admission and
after fresh readback. **Raw file hash and native envelope statement hash are
different identities**, both recorded. AHE stores/reviews the declared envelope;
the fixed Pouch integration harness performs decoding and raw-byte validation.
Compression does not prove source meaning or change the finite model.

## Per-path native records

| Path | Model result | Execution binding | Recorded events checked |
|---|---|---|---:|
{rows}

Each model validation receipt remains `model_conditional_conclusion` with
`observed_fact=false`. The separate execution statement is
`recorded_file_replay_binding`, also with `observed_fact=false` and
`new_execution=false`. It describes a checked existing record, not a freshly
observed production state. Native admission receipts are a third evidence layer.

## What the receiver actually checked

The fixed-example harness pins the committed original manifest, report, authority
and bundle, verifies all 205 original inventory entries, and revalidates all six
packages. For each event it checks action, direction, target, complete before/after
snapshots, changed-file hash/mode, unchanged-file frames and correspondence with
model states. The final three-file snapshot equals the original checkpoint.
Engineering negative checks reject changed target hashes, mode, frame, action,
missing return, wrong source identity/digest and wrong request. These are focused
binding checks, not a general reliability theorem or new repair trial.

The native integration uses existing Query/intake/source-review/endpoint-review
public stdio MCP profiles and pinned tool schemas. Every approval is visibly
**TEST APPROVAL STUB**, limited to synthetic/public fixtures in a fresh isolated
DB; not human approval or semantic certification. Exact native review, statement,
all direct parents, native receipt and final readback are checked. No automatic
retry was used. AHE product code and its pre-existing local edits were unchanged.

## Artifacts and limits

- [`native/summary.json`](native/summary.json): actual counts and native IDs.
- `native/*-source.json`: complete source review/admission/readback payloads.
- `native/*-model-publication.json`: original and rebound validation, native receipt.
- `native/*-execution-publication.json`: exact derived review, both AND parents,
  approval, native receipt and publication-time readback.
- `native/*-final-readback.json`: separate final Query responses.
- `frozen/`: pre-run plan and Go integration source; native build/schema identities
  are also supplied. Local paths, credentials, runtime launchers and DB files are
  excluded. Source archives must be decoded for semantic inspection.
- [`audit.json`](audit.json): {len(checks)} read-only artifact checks; not a second
  independent physical experiment. `manifest.json` hashes this portable export.

This acceptance is implemented by an opt-in fixed-example Go integration harness
around the existing adapter, not a new general-purpose physical-repair validator.
There are no new official Django/SWE test results. Historical
`h13-restore-20260928-02` still has **FAIL / CONTROL_REPRODUCTION_MISMATCH**.
This records restoration of three disposable file copies; HTTP/database/external
side effects, production rollback and general technical-debt cost are outside scope.
The original run's statement that it performed no AHE publication describes its
execution time; this is a separate, later persistence run.
'''
emit(Path('REPORT.md'), report_md.encode())
files = {str(p.relative_to(out)): {'sha256':sha(p.read_bytes()),'bytes':p.stat().st_size} for p in sorted(out.rglob('*')) if p.is_file()}
emit_json('manifest.json', {'schema_version':'pouch-repair-native-export/v1','run_id':cfg['run_id'],'files':files})
print(json.dumps({'status':'PASS','run_id':cfg['run_id'],'files':len(files)+1,'audit_checks':len(checks),'model_admissions':6,'execution_bindings':6,'final_readbacks':12,'private_path_scan':'PASS','cluster_stopped':True}))
