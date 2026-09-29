#!/usr/bin/env python3
"""Export selected native fixture evidence without launcher secrets/local paths."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--work', type=Path, required=True)
parser.add_argument('--ahe-repo', type=Path, required=True)
parser.add_argument('--out', type=Path, required=True)
parser.add_argument('--supplemental-overwrote-original', action='store_true')
args = parser.parse_args()
work = args.work.resolve()
out = args.out.resolve()
config = json.loads((work / 'native-config.json').read_text())
summary = json.loads((work / 'publication-summary.json').read_text())
if config['schema_version'] != 'pouch-disposable-native/v1' or summary['status'] != 'PASS':
    raise SystemExit('completed disposable native fixture required')
if (work / 'pgdata' / 'postmaster.pid').exists():
    raise SystemExit('stop the disposable cluster before final export')
out.mkdir(parents=True, exist_ok=False)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def emit(relative, raw):
    for marker in [b'/Users/', b'/private/tmp/', b'/tmp/', b'postgres://', b'postgresql://']:
        if marker in raw:
            raise RuntimeError('private path or DSN detected in selected export: ' + str(relative))
    target = out / relative
    target.parent.mkdir(parents=True, exist_ok=True)
    with target.open('xb') as stream:
        stream.write(raw)


def emit_json(relative, obj):
    emit(relative, (json.dumps(obj, ensure_ascii=False, indent=2) + '\n').encode())


def copy(relative):
    emit(relative, (work / relative).read_bytes())


build = json.loads((work / 'build-identity.json').read_text())
patch = subprocess.check_output(['git', 'diff', '--binary', 'HEAD'], cwd=args.ahe_repo)
status = subprocess.check_output(['git', 'status', '--porcelain=v1'], cwd=args.ahe_repo, text=True)
head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=args.ahe_repo, text=True).strip()
build['checkout_after_acceptance'] = {
    'head': head, 'patch_sha256': sha(patch), 'patch_bytes': len(patch),
    'status': status.splitlines(),
    'capture_phase': 'after_native_acceptance',
    'limit': 'This post-run checkout identity is not a pre-build freeze. Exact saved binary hashes identify the executed native programs.'}
for name, expected in build['binaries'].items():
    if sha((work / 'bin' / name).read_bytes()) != expected:
        raise RuntimeError('native binary changed after fixture build')
emit_json('build-identity.json', build)
emit_json('schema-pins.json', config['pins'])
for role in config['pins']:
    copy(Path(role + '-discovery.json'))
copy(Path('sources-summary.json'))
copy(Path('publication-summary.json'))

case_rows = []
for name in ['synthetic-key-rotation', 'django-13344']:
    base = work / name
    for f in ['authority.json', 'sources.json', 'input-binding.json', 'source-native-ledger.json', 'source-readbacks.json']:
        copy(Path(name) / f)
    source_manifest = json.loads((base / 'sources.json').read_text())
    for source in source_manifest['sources']:
        content = (base / source['file']).read_bytes()
        if sha(content) != source['sha256']:
            raise RuntimeError('source digest mismatch')
        copy(Path(name) / source['file'])
    artifact_name = 'artifact-readback.json' if (base / 'artifact-readback.json').exists() else 'artifact.json'
    snapshot_name = 'snapshot-readback.json' if (base / 'snapshot-readback.json').exists() else 'snapshot.json'
    snapshot = json.loads((base / snapshot_name).read_text())
    artifact = (base / artifact_name).read_bytes()
    if sha(artifact) != snapshot['artifact_sha256']:
        raise RuntimeError('materialized artifact hash mismatch')
    emit(Path(name) / 'artifact.json', artifact)
    emit_json(Path(name) / 'snapshot-evidence.json', {
        'selection': 'complete native materialized artifact separately delivered; descriptor and byte hashes selected here; full query response not delivered',
        'schema_version': snapshot['schema_version'], 'view': snapshot['view'],
        'artifact_sha256': snapshot['artifact_sha256'], 'response_sha256': snapshot['response_sha256'],
        'original_source_phase_read_view_retained': not args.supplemental_overwrote_original,
        'snapshot_role': 'supplementary native query; projection.Decode accepted exact artifact bytes'})
    if (base / 'graph-matrix.json').exists():
        copy(Path(name) / 'graph-matrix.json')
    copy(Path(name) / 'result' / 'bundle.json')
    paths = sorted((base / 'result' / 'packages').glob('p*.json'))
    for p in paths:
        copy(p.relative_to(work))
    publications = sorted((base / 'publication').glob('p*.json'))
    for p in publications:
        value = json.loads(p.read_text())
        emit_json(Path(name) / 'publication' / p.name, {
            'selection': 'validation, exact approval, intake receipts, complete admission and fresh Query readback; native review display and repeated source records omitted',
            'original_fixture_file_sha256': sha(p.read_bytes()),
            'validation': value['validation'], 'approval': value['approval'],
            'pending': value['pending'], 'publication': value['publication'],
            'review_binding': {key: value['review'][key] for key in ['subject', 'display_sha256', 'package_sha256']}})
    bundle = json.loads((base / 'result' / 'bundle.json').read_text())
    kinds = [json.loads(p.read_text())['return_kind'] for p in paths]
    case_rows.append({'case': name, 'sources': len(source_manifest['sources']), 'paths': len(paths),
                      'return': kinds.count('return'), 'no_return': kinds.count('no_return'),
                      'native_admissions': len(publications), 'fresh_readbacks': len(publications),
                      'complete': bundle['complete'], 'snapshot_nodes': snapshot['view']['node_count'],
                      'snapshot_edges': snapshot['view']['edge_count'], 'snapshot_truncated': snapshot['view']['truncated']})

notes = {
    'schema_version': 'pouch-native-acceptance/v1', 'fixture_id': config['run_id'],
    'status': 'PASS', 'public_mcp_only_for_evidence': True, 'cases': case_rows,
    'cluster_stopped': True, 'no_operational_database_accessed': True,
    'approval': 'TEST APPROVAL STUB; synthetic/public fixture only; not human or semantic certification',
    'source_binding': 'exact canonical ID and SHA256(statement_text UTF-8); no lifecycle/global-currentness claim',
    'snapshot_retention': ('Original source-phase snapshot.json was overwritten by the supplementary read before the harness was corrected. This export retains the supplementary exact artifact and descriptor only. Source statement files, source native ledger and source readbacks remain retained; authority/search bind statements, not the overwritten read-view bytes.' if args.supplemental_overwrote_original else 'Source-phase and supplementary read-view files are distinct in the private fixture.'),
    'engineering_history': [
        'Fixture 01: full_document plus nonempty limitations rejected during source intake; adapter envelope fixed; no model search or result admission.',
        'Fixture 02: native raw_content_hash has sha256: prefix; adapter initially expected bare hex and rejected its receipt; comparison fixed; no model search or result admission.',
        'Fixture 03: fresh complete source phase, new native-bound Pouch solve, independent validation, exact native review/admission/readback all completed; earlier denominators not combined.'],
    'export_limits': ['Selected original files and explicitly selected receipt projections only.', 'Full native review displays and full Query response framing are not delivered.', 'Solver CNF/proof files are not copied here; bundle and packages describe the bounded results.', 'No official SWE runtime test, source-to-model semantic completeness, global currentness, or physical restore claim.']}
emit_json('acceptance.json', notes)
rows = '\n'.join(f"| {r['case']} | {r['sources']} | {r['paths']} | {r['return']} | {r['no_return']} | {r['fresh_readbacks']} |" for r in case_rows)
graph_rows = '\n'.join(f"| {r['case']} | {r['snapshot_nodes']} | {r['snapshot_edges']} | {r['snapshot_truncated']} |" for r in case_rows)
report = f'''# Pouch / AHE public-MCP acceptance

Fixture `{config['run_id']}` completed **PASS** on 2026-09-29. Nine fresh sources
passed public intake, proposal, exact source review, test approval, native admission
and fresh Query. Pouch then searched both newly bound authorities. All eight
packages passed independent validation, exact public endpoint review, test approval,
native derived admission and fresh result Query.

| Fixed case | Sources | Paths | Return | No return | Exact result readbacks |
|---|---:|---:|---:|---:|---:|
{rows}

The isolated PostgreSQL cluster is stopped. Operational user databases were not
accessed. Every fixture approval is explicitly **TEST APPROVAL STUB**; it is not
human approval or a semantic certification.

## What was checked

- Both unchanged native canonical graph artifacts are accepted by `projection.Decode`;
  no adapter field remapping was required. See each `artifact.json` and selected
  `snapshot-evidence.json` scope/hash descriptor.
- Exact source statement bytes and new canonical IDs bind the caller-owned v1
  authority. The model remains unchanged; identities are rebound to this fresh run.
- `Prepare` independently checks authority/package/request and fresh source bindings.
- Native review binds the pending statement, all source parents and package hash.
- Admission/readback checks exact statement equality, `derived_claim`, request,
  subject, reviewer, package-bound approval and parent set. A successful status alone
  is insufficient. All four no-return conclusions receive the same checks.
- The implementation uses generic public tools only, with separate schema-pinned
  Query, intake, source-review and endpoint-review profiles. AHE product code was
  not changed by this adapter work; existing local Pouch-specific removals remain.

## Bounded graph materialization

| Fixed case | Nodes | Edges | Truncated |
|---|---:|---:|---|
{graph_rows}

The Django materialized graph hits the 128-node view cap. Its projection is valid
for the delivered bounded artifact, but this is **not complete AHE graph coverage**.
Pouch's `complete` search status concerns its separately declared finite authority
model; it does not make a truncated evidence view complete. Both views forbid
global absence inference. The supplied `graph-matrix.json` files, when present,
are the CLI projections of those exact artifacts.

## Executed build and evidence

`build-identity.json` records AHE base HEAD, every executed native binary hash,
and the post-run checkout patch hash/status. HEAD alone does not identify the dirty
checkout. The checkout hash was captured after acceptance, not before build;
the saved binary hashes identify the exact programs used. Future bootstrap runs
also freeze the checkout patch before building.

`acceptance.json`, source ledgers/readbacks, exact authorities/source files,
new Pouch bundles/packages, and selected publication records are supplied here.
Each publication selection retains the full native admission and fresh Query
readback, while omitting the large native review display and repeated source records.
`manifest.json` hashes all supplied files except itself. Omitted CNF/proof files,
full review displays and full Query framing are not implicitly delivered.

## Retention correction and engineering history

{notes['snapshot_retention']}
The harness now creates distinct supplementary filenames exclusively and refuses
to overwrite them. This fixture was not rerun to conceal the retention mistake.

Earlier setup fixtures are separate engineering history: fixture 01 exposed an
invalid full-document envelope limitation; fixture 02 exposed a missing `sha256:`
prefix in the adapter receipt comparison. Both were stopped and retained privately.
This report describes fixture 03 only and does not combine their denominators.

## Interpretation limits

The public v1 source binding checks **exact statement_text plus canonical ID**.
It does not establish all lifecycle metadata, invalidation state, or a global
currentness cut. Statements remain `model_conditional_conclusion`; validation does
not prove source-to-model interpretation, official SWE repair execution, physical
restore, or general reliability. Missing public tools are capability failures;
ambiguous writes remain unknown and are never automatically retried.
'''
emit('REPORT.md', report.encode())
manifest = {'schema_version': 'pouch-native-export-manifest/v1', 'fixture_id': config['run_id'],
            'files': {str(p.relative_to(out)): {'sha256': sha(p.read_bytes()), 'bytes': p.stat().st_size}
                      for p in sorted(out.rglob('*')) if p.is_file()}}
emit_json('manifest.json', manifest)
print(json.dumps({'status': 'EXPORTED', 'fixture_id': config['run_id'], 'files': len(manifest['files']) + 1,
                  'native_admissions': 8, 'fresh_readbacks': 8, 'cluster_stopped': True}))
