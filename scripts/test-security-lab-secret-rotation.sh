#!/usr/bin/env bash
# Mutates only the explicitly selected disposable security lab. It rotates
# the API database password, injects a recreation failure, and resumes it.
set -euo pipefail
: "${ZTP_SECURITY_LAB_PROJECT:?select a disposable security lab explicitly}"
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
state="$("$root/tools/security-lab-local" state-dir)"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
umask 077
export ROTATION_STATE="$state" ROTATION_ROOT="$root" ROTATION_FIXTURE="$fixture"
export ROTATION_DOCKER="$(command -v docker)"
python3 - <<'PY'
import datetime,json,os,pathlib,subprocess,sys
s=pathlib.Path(os.environ['ROTATION_STATE']);root=pathlib.Path(os.environ['ROTATION_ROOT']);f=pathlib.Path(os.environ['ROTATION_FIXTURE'])
project=os.environ['ZTP_SECURITY_LAB_PROJECT'];docker=os.environ['ROTATION_DOCKER']
image='postgres:18.6-alpine3.24@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
started=datetime.datetime.now(datetime.timezone.utc).isoformat()
old=(s/'secrets/postgres-app-password').read_text().strip()
def pgpass(name,password):
 p=f/name;p.write_text(f'postgres:5432:zero_to_prod:zero_to_prod_app:{password}\n');p.chmod(0o600)
pgpass('old.pgpass',old)
def probe(name):
 return [docker,'run','--rm','-i','--network',f'{project}_data','--user',f'{os.getuid()}:{os.getgid()}',
 '--read-only','--cap-drop','ALL','--security-opt','no-new-privileges:true',
 '-v',f'{f}:/probe:ro','-v',f'{s}/pki/internal-ca.crt:/ca.crt:ro',
 '-e',f'PGPASSFILE=/probe/{name}','-e','PGCONNECT_TIMEOUT=5','-e','PGSSLMODE=verify-full','-e','PGSSLROOTCERT=/ca.crt',
 '--entrypoint','psql',image,'-X','-q','-A','-t','-h','postgres','-U','zero_to_prod_app','-d','zero_to_prod','-v','ON_ERROR_STOP=1']
# Hold a connection made with the old credential. Its backend must be retired,
# not merely left working after the password changes.
with (f/'old-session-error').open('wb') as errfile:
 live=subprocess.Popen(probe('old.pgpass'),stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=errfile,text=True)
 try:
  live.stdin.write('SELECT pg_backend_pid();\n');live.stdin.flush()
  pid=int(live.stdout.readline().strip())
  # Failure after role/files/session revocation but before consumer recreation.
  # No test-only failure flag is added to the production command.
  script='''docker() {
 if [[ "$*" == *"up -d --wait --force-recreate --no-deps api"* ]]; then return 42; fi
 "$ROTATION_DOCKER" "$@"
}
export -f docker
exec "$ROTATION_ROOT/tools/security-lab-local" rotate-app-db-password
'''
  first=subprocess.run(['bash','-c',script],capture_output=True,text=True)
  assert first.returncode==42,('unexpected injected failure status',first.returncode)
  pending=s/'secrets/.app-db-rotation/new-password'
  planned=pending.read_text().strip();new=(s/'secrets/postgres-app-password').read_text().strip()
  assert new==planned and new!=old
  count=subprocess.check_output([docker,'exec',f'{project}-postgres-1','psql','-U','zero_to_prod_admin','-d','zero_to_prod','-Atc',f'SELECT count(*) FROM pg_stat_activity WHERE pid={pid}'],text=True).strip()
  assert count=='0','old authenticated session was not revoked'
  pgpass('new.pgpass',new)
  refused=subprocess.run(probe('old.pgpass')+['-c','SELECT 1'],capture_output=True,text=True)
  assert refused.returncode!=0 and 'password authentication failed' in refused.stderr
  accepted=subprocess.run(probe('new.pgpass')+['-c','SELECT 1'],capture_output=True,text=True)
  assert accepted.returncode==0 and accepted.stdout.strip()=='1'
  second=subprocess.run([str(root/'tools/security-lab-local'),'rotate-app-db-password'],capture_output=True,text=True)
  assert second.returncode==0,('resume failed',second.stderr[-400:])
  assert not pending.parent.exists()
  assert (s/'secrets/postgres-app-password').read_text().strip()==planned
  assert subprocess.run(probe('new.pgpass')+['-c','SELECT 1'],capture_output=True).returncode==0
  output=first.stdout+first.stderr+second.stdout+second.stderr
  # Scan every service log since this rotation, plus command output, for all
  # configured source secrets and this experiment's retired credential.
  names=['postgres-admin-password','postgres-migrator-password','postgres-app-password','postgres-worker-password','rabbitmq-publisher-password','rabbitmq-worker-password','keycloak-db-password','gateway-client-secret','automation-client-secret','lab-user-password','lab-viewer-password','lab-outsider-password','lab-admin-client-secret','valkey-password','oauth2-proxy-cookie-secret']
  values=[old,new]+[(s/'secrets'/name).read_text().strip() for name in names]
  logs=[]
  for service in ['postgres','api','worker','ingress','keycloak','keycloak-postgres','oauth2-proxy','valkey','rabbitmq']:
   logs.append(subprocess.check_output([docker,'logs','--since',started,f'{project}-{service}-1'],stderr=subprocess.STDOUT,text=True))
  def matches(text):return sum(bool(v) and v in text for v in values)
  assert matches(output+'\n'.join(logs))==0,'a credential appeared in command output or service logs'
  assert matches('\n'.join(values))==len(values),'scanner positive control failed'
  report={'scope':'API database password rotation in explicitly selected local security lab','project':project,'started_at':started,'checks':[
   'old credential connected before rotation','failure injected after role/files update','pending journal preserves planned password','old authenticated backend terminated',
   'old credential refused on a new verified-TLS connection','new credential accepted on a new verified-TLS connection','resume reuses planned value and recreates API','API readiness recovered before success',
   'pending journal removed after success','new credential still accepted after resume','source values and retired/new credential absent from command output and all nine service logs','scanner positive control'], 'passed':True}
  evidence=os.environ.get('ZTP_ROTATION_TEST_EVIDENCE')
  if evidence:
   p=pathlib.Path(evidence);p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(report,indent=2)+'\n')
  print(json.dumps(report,indent=2))
 finally:
  try:live.stdin.close()
  except BrokenPipeError:pass
  try:live.wait(timeout=10)
  except subprocess.TimeoutExpired:live.kill();live.wait()
PY
