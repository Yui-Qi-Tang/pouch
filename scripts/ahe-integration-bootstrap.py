#!/usr/bin/env python3
"""Fresh local PostgreSQL + public MCP acceptance fixture; never adopts a store.

Run: python3 scripts/ahe-integration-bootstrap.py --ahe-repo PATH --work NEW_PATH
Then solve the generated two authority/source manifests into CASE/result.
Run: python3 scripts/ahe-integration-bootstrap.py --work PATH --publish
Stop: python3 scripts/ahe-integration-bootstrap.py --work PATH --stop

Database SQL is used only for isolated cluster/schema/role provisioning. All
case sources, proposals, canonical writes and readbacks use real public MCP.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import sys
import urllib.parse

p = argparse.ArgumentParser()
p.add_argument('--ahe-repo', type=Path)
p.add_argument('--work', required=True, type=Path)
p.add_argument('--publish', action='store_true')
p.add_argument('--discovery-only', action='store_true', help='provision and pin public tools without loading the default examples')
p.add_argument('--stop', action='store_true')
a = p.parse_args()
root = Path(__file__).resolve().parents[1]
work = a.work.resolve()
config = work / 'native-config.json'
base_env = {k:v for k,v in os.environ.items() if not k.startswith('PG')}
base_env['GOCACHE'] = str(work / 'go-cache')

def run(argv, env=None, label=None, data=None, cwd=None):
    # Outputs are retained under private fixture ownership. Never print DSNs.
    out = work / ((label or Path(argv[0]).name) + '.log')
    with out.open('ab') as stream:
        r = subprocess.run([str(x) for x in argv], cwd=cwd, env=env or base_env,
                           input=data, stdout=stream, stderr=subprocess.STDOUT)
    if r.returncode:
        raise RuntimeError(f'{label or Path(argv[0]).name} failed; inspect private log {out.name}')

def require(command):
    found = shutil.which(command)
    if not found:
        raise RuntimeError(f'{command} is required')
    return str(Path(found).resolve())

def test(pattern, label):
    env = dict(base_env, POUCH_AHE_NATIVE_CONFIG=str(config),
               POUCH_AHE_TEST_APPROVAL='isolated-synthetic-public-fixtures-only')
    run([require('go'), 'test', '-count=1', '-timeout=5m', '-v', './internal/ahe', '-run', pattern],
        env=env, label=label, cwd=root)

def stop():
    marker = json.loads((work/'cluster-owner.json').read_text())
    if marker.get('contract') != 'pouch-disposable-native/v1' or marker.get('work') != str(work):
        raise RuntimeError('refusing to stop an unowned cluster')
    run([require('pg_ctl'), '-D',work/'pgdata','-m','fast','-w','stop'],label='stop')

try:
    if a.stop:
        stop();print('STOPPED generated disposable cluster');sys.exit(0)
    if a.publish:
        test('^TestNativePublication$', 'native-publication')
        print((work/'publication-summary.json').read_text());sys.exit(0)
    if not a.ahe_repo or not a.ahe_repo.is_dir():
        raise RuntimeError('--ahe-repo must identify the unchanged AHE checkout')
    work.mkdir(mode=0o700,parents=False,exist_ok=False)
    (work/'bin').mkdir(mode=0o700)
    (work/'socket').mkdir(mode=0o700)
    run_id = 'pouch-native-' + secrets.token_hex(6)
    password = secrets.token_hex(24)
    (work/'admin-password').write_text(password);(work/'admin-password').chmod(0o600)
    run([require('initdb'),'-D',work/'pgdata','-U','pouch_lab_admin','-A','scram-sha-256',
         '--pwfile',work/'admin-password','--no-locale','--encoding=UTF8'],label='initdb')
    with socket.socket() as sock:
        sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
    (work/'cluster-owner.json').write_text(json.dumps({'contract':'pouch-disposable-native/v1','work':str(work),'run_id':run_id,'port':port}))
    run([require('pg_ctl'),'-D',work/'pgdata','-l',work/'postgres.log','-o',
         f'-h 127.0.0.1 -p {port} -k {work / "socket"}', '-w','start'],label='start')
    pg_env=dict(base_env,PGHOST='127.0.0.1',PGPORT=str(port),PGUSER='pouch_lab_admin',PGPASSWORD=password,PGDATABASE='postgres')
    def sql(text, database='pouch_native'):
        run([require('psql'),'-X','-v','ON_ERROR_STOP=1'],env=dict(pg_env,PGDATABASE=database),data=text.encode(),label='provision-sql')
    sql('CREATE DATABASE pouch_native;','postgres')
    sql('REVOKE ALL ON DATABASE pouch_native FROM PUBLIC; CREATE SCHEMA pouch_native; REVOKE ALL ON SCHEMA public FROM PUBLIC;')
    # Freeze checkout identity before building; the HEAD alone may omit local removals.
    checkout_patch = subprocess.check_output(['git', 'diff', '--binary', 'HEAD'], cwd=a.ahe_repo)
    (work/'ahe-checkout.patch').write_bytes(checkout_patch)
    checkout_status = subprocess.check_output(['git', 'status', '--porcelain=v1'], cwd=a.ahe_repo, text=True)
    checkout_identity = {'capture_phase': 'before_build', 'patch_sha256': hashlib.sha256(checkout_patch).hexdigest(),
                         'patch_bytes': len(checkout_patch), 'status': checkout_status.splitlines()}
    binaries={}
    for name in ['ahe-migrate','ahe-runtime-admin','ahe-mcp-launch','ahe-query-mcp','ahe-ingest-mcp']:
        path=work/'bin'/name
        run([require('go'),'build','-mod=readonly','-o',path,'./cmd/'+name],label='build-'+name,cwd=a.ahe_repo)
        binaries[name]=path
    def url(user,pwd):
        return f'postgres://{user}:{pwd}@127.0.0.1:{port}/pouch_native?sslmode=disable&connect_timeout=3'
    admin_env=dict(base_env,DATABASE_DSN=url('pouch_lab_admin',password),AHE_DATABASE_NAME='pouch_native',AHE_DATABASE_SCHEMA='pouch_native')
    run([binaries['ahe-migrate']],env=admin_env,label='migrate')
    commands={}
    for profile in ['query','intake','source-claim-reviewer','endpoint-reviewer']:
        stem=profile.replace('-','_');login='pouch_'+stem+'_login';role='pouch_'+stem+'_group';pwd=secrets.token_hex(24)
        env=dict(admin_env,AHE_DATABASE_LOGIN=login,AHE_DATABASE_ROLE=role,AHE_RUNTIME_PROFILE=profile)
        run([binaries['ahe-runtime-admin'],'provision'],env=env,label='provision-'+profile)
        sql(f"ALTER ROLE {login} PASSWORD '{pwd}';")
        credential=work/(profile+'.dsn');credential.write_text(url(login,pwd));credential.chmod(0o600)
        binary=binaries['ahe-query-mcp' if profile=='query' else 'ahe-ingest-mcp']
        cfg={'schema_version':'ahe-mcp-launcher/v1','binary_path':str(binary),'database_dns_file':str(credential),
             'database':'pouch_native','session_user':login,'schema':'pouch_native','role':role,'profile':profile,'principal_id':'TEST APPROVAL STUB:'+profile}
        launcher=work/(profile+'-launcher.json');launcher.write_text(json.dumps(cfg));launcher.chmod(0o600)
        commands[profile]={'path':str(binaries['ahe-mcp-launch']),'args':['--config',str(launcher)]}
    config.write_text(json.dumps({'schema_version':'pouch-disposable-native/v1','run_id':run_id,'work_dir':str(work),'pouch_project':str(root),'commands':commands,'pins':{}},indent=2));config.chmod(0o600)
    head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=a.ahe_repo,text=True).strip()
    (work/'build-identity.json').write_text(json.dumps({'ahe_head':head,'checkout':checkout_identity,'binaries':{name:hashlib.sha256(path.read_bytes()).hexdigest() for name,path in binaries.items()},'fixture':'fresh PostgreSQL cluster; no operational database accessed'},indent=2))
    test('^TestNativeDiscovery$', 'native-discovery')
    if a.discovery_only:
        print(json.dumps({'status':'DISCOVERY_READY','run_id':run_id}))
        sys.exit(0)
    test('^TestNativeSources$', 'native-sources')
    print(json.dumps({'status':'SOURCES_READY','work_dir':str(work),'run_id':run_id,'cases':['synthetic-key-rotation','django-13344'],'next':'Run Pouch solve on each new authority.json/sources.json into CASE/result, then --publish, then --stop.'}))
except Exception as exc:
    print(str(exc),file=sys.stderr)
    print('No automatic retry. Any generated cluster remains identified by cluster-owner.json; use --stop after inspection.',file=sys.stderr)
    sys.exit(1)
