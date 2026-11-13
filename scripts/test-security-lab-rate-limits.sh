#!/usr/bin/env bash
# Exercise the production nginx admission rules with controlled TLS upstreams.
# This tests rate limiting, not authentication or the real Web UI.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
container="ztp-rate-test-$$"
network="${container}-net"
image='nginx:1.30.5-alpine@sha256:0985e772fb9f729e6fa0980da05fca5d9c468e870eed43071545afa9d2e27d94'
cleanup() {
 local status=$?
 if [[ "$status" != 0 ]]; then
  docker logs --tail 12 "${container}-upstream" >&2 2>/dev/null || true
  docker logs --tail 12 "$container" >&2 2>/dev/null || true
 fi
 docker rm -f "$container" "${container}-upstream" >/dev/null 2>&1 || true
 docker network rm "$network" >/dev/null 2>&1 || true
 rm -rf -- "$fixture"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
ZTP_SECURITY_LAB_PROJECT=zero-to-prod-security-lab-rate ZTP_SECURITY_LAB_STATE_DIR="${fixture}/state" "$root/tools/security-lab-local" init >/dev/null
subnet=()
[[ -z "${ZTP_RATE_TEST_SUBNET:-}" ]] || subnet=(--subnet "$ZTP_RATE_TEST_SUBNET")
docker network create "${subnet[@]}" --label zero-to-prod.rate-test=true "$network" >/dev/null
python3 - "$root" "$fixture" <<'PY'
import pathlib,sys
root,fixture=map(pathlib.Path,sys.argv[1:])
s=(root/'infra/security-lab/ingress/nginx.conf').read_text().rstrip()
assert s.endswith('}')
# Separate controlled upstreams keep the production configuration unchanged.
# Its verifier checks their service-specific names and CAs.
mock='''
 log_format test_upstream 'server=$server_name method=$request_method path=$uri';
 server {
  listen 8443 ssl; server_name api work-items.localhost;
  ssl_certificate /mock/api.pem; ssl_certificate_key /mock/api.pem;
  access_log /tmp/upstream.log test_upstream;
  location / { default_type application/json; return 200 '{"mock":"api"}'; }
 }
 server {
  listen 4443 ssl; server_name oauth2-proxy;
  ssl_certificate /mock/oauth2-proxy.pem; ssl_certificate_key /mock/oauth2-proxy.pem;
  access_log /tmp/upstream.log test_upstream;
  location = /oauth2/auth { add_header X-Auth-Request-Access-Token rate-test-placeholder; return 200; }
  location / { return 200 'mock gateway'; }
 }
 server {
  listen 8443 ssl; server_name keycloak auth.work-items.localhost;
  ssl_certificate /mock/keycloak.pem; ssl_certificate_key /mock/keycloak.pem;
  access_log /tmp/upstream.log test_upstream;
  location / { return 200 'mock identity provider'; }
 }
'''
(fixture/'nginx.conf').write_text(s+'\n')
(fixture/'mock-nginx.conf').write_text('pid /tmp/nginx.pid; events {} http { client_body_temp_path /tmp/client_body; proxy_temp_path /tmp/proxy; fastcgi_temp_path /tmp/fastcgi; uwsgi_temp_path /tmp/uwsgi; scgi_temp_path /tmp/scgi;\n'+mock+'\n}\n')
(fixture/'mock').mkdir()
import shutil
for service in ('api','oauth2-proxy'):
 shutil.copy(fixture/'state/app-tls'/service/'tls.pem',fixture/'mock'/f'{service}.pem')
shutil.copy(fixture/'state/idp-tls/tls.pem',fixture/'mock/keycloak.pem')
(fixture/'static').mkdir();(fixture/'static/index.html').write_text('rate-limit static control')
PY
chmod -R a+rX "${fixture}/mock" "${fixture}/static"
docker run -d --name "${container}-upstream" --network "$network" \
 --network-alias api --network-alias oauth2-proxy --network-alias keycloak \
 --label zero-to-prod.rate-test=true --cap-drop ALL --security-opt no-new-privileges:true \
 --read-only --user 101:101 --tmpfs /tmp --tmpfs /var/cache/nginx \
 --entrypoint nginx -v "${fixture}/mock-nginx.conf:/etc/nginx/nginx.conf:ro" \
 -v "${fixture}/mock:/mock:ro" "$image" -g 'daemon off;' >/dev/null
docker run -d --name "$container" --network "$network" \
 --label zero-to-prod.rate-test=true --cap-drop ALL --security-opt no-new-privileges:true \
 --read-only --user 101:101 --tmpfs /tmp --tmpfs /var/cache/nginx \
 -p 127.0.0.1::9443 --entrypoint nginx \
 -v "${fixture}/nginx.conf:/etc/nginx/nginx.conf:ro" \
 -v "$root/infra/security-lab/ingress/api-upstream.conf:/etc/nginx/api-upstream.conf:ro" \
 -v "$root/infra/security-lab/ingress/oauth2-proxy-upstream.conf:/etc/nginx/oauth2-proxy-upstream.conf:ro" \
 -v "$root/infra/security-lab/ingress/keycloak-upstream.conf:/etc/nginx/keycloak-upstream.conf:ro" \
 -v "${fixture}/state/ingress-tls:/run/ingress-tls:ro" \
 -v "${fixture}/state/pki/app-ca.crt:/run/app-ca/ca.crt:ro" \
 -v "${fixture}/state/pki/idp-ca.crt:/run/idp-ca/ca.crt:ro" \
 -v "${fixture}/static:/usr/share/nginx/html:ro" \
 "$image" -g 'daemon off;' >/dev/null
port="$(docker port "$container" 9443/tcp | cut -d: -f2)"
python3 - "$port" "${fixture}/state/pki/ca.crt" "$container" "${ZTP_RATE_TEST_EVIDENCE:-}" <<'PY'
import concurrent.futures, http.client, json, socket, ssl, subprocess, sys, time
from collections import Counter
port,ca,container,evidence=sys.argv[1:]
ctx=ssl.create_default_context(cafile=ca)
class Connection(http.client.HTTPSConnection):
 def connect(self):
  self.sock=ctx.wrap_socket(socket.create_connection(('127.0.0.1',int(port)),timeout=10),server_hostname=self.host)
all_api=[]
def request(path,host='work-items.localhost',method='GET',session=False,index=0):
 headers={'Host':f'{host}:9443','X-Forwarded-For':f'192.0.2.{index%250+1}','X-Real-IP':f'198.51.100.{index%250+1}','Forwarded':f'for=203.0.113.{index%250+1}'}
 headers.update({'Cookie':'__Host-wi-session=rate-test-placeholder'} if session else {'Authorization':'Bearer rate-test-placeholder'})
 conn=Connection(host,timeout=10)
 try:
  conn.request(method,path,headers=headers);response=conn.getresponse();body=response.read().decode();status=response.status
  if status==429:
   assert json.loads(body)=={'error':'rate_limited'},body
   assert response.getheader('Retry-After')=='1'
  if path.startswith('/api/') and status==200: all_api.append(status)
  return status
 finally: conn.close()
for _ in range(40):
 try:
  if request('/')==200:break
 except (OSError,ssl.SSLError):pass
 time.sleep(.25)
else:raise AssertionError('nginx did not become ready')
results=[]
def burst(label,path,host='work-items.localhost',method='GET',session=False,n=120):
 started=time.monotonic()
 with concurrent.futures.ThreadPoolExecutor(max_workers=80) as pool:
  statuses=list(pool.map(lambda i:request(path,host,method,session,i),range(n)))
 counts=Counter(statuses)
 assert counts[200]>0 and counts[429]>0,(label,counts)
 assert set(counts)<={200,429},(label,counts)
 results.append({'check':label,'statuses':dict(counts),'seconds':round(time.monotonic()-started,3),'passed':True})
burst('bearer path limited despite changing forwarding headers','/api/items')
time.sleep(3)
assert request('/api/items')==200
results.append({'check':'bearer recovers after refill','passed':True})
burst('session path uses the same API admission policy','/api/items',session=True)
time.sleep(3)
burst('identity-provider POSTs limited','/realms/work-items/protocol/openid-connect/token','auth.work-items.localhost','POST',n=45)
assert request('/api/items')==200
results.append({'check':'authentication saturation does not consume API bucket','passed':True})
time.sleep(3)
burst('gateway sign-in initiation limited','/oauth2/start',n=45)
assert request('/')==200
assert request('/realms/work-items/.well-known/openid-configuration','auth.work-items.localhost')==200
results.append({'check':'static UI and discovery stay available during auth saturation','passed':True})
upstream=subprocess.check_output(['docker','exec',container+'-upstream','cat','/tmp/upstream.log'],text=True)
api_calls=sum(line.startswith('server=api ') for line in upstream.splitlines())
assert api_calls==len(all_api),(api_calls,len(all_api))
results.append({'check':'rate-limited API requests never reach controlled API upstream','api_upstream_calls':api_calls,'admitted_api_requests':len(all_api),'passed':True})
logs=subprocess.check_output(['docker','logs',container],stderr=subprocess.STDOUT,text=True)
assert 'limit=REJECTED' in logs
assert 'rate-test-placeholder' not in logs
results.append({'check':'rejection observable without credential values','passed':True})
report={'scope':'production nginx rules with controlled TLS upstreams; authentication and real UI not tested','checks':results,'passed':True}
if evidence:
 import pathlib
 p=pathlib.Path(evidence);p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
PY
