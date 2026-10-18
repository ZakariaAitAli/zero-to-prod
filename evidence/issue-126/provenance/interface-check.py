import json,time,subprocess,urllib.request
from pathlib import Path
out=Path('evidence/issue-126/final-interface-check');out.mkdir(exist_ok=False)
def command(*args):
 p=subprocess.run(['tools/operations-lab-local',*args],capture_output=True,text=True)
 with (out/'commands.log').open('a') as f:f.write(' '.join(args)+'\n'+p.stdout+p.stderr+'\n')
 if p.returncode:raise RuntimeError('command failed')
 return p.stdout

def get(path,accept=None,base='http://127.0.0.1:18086'):
 req=urllib.request.Request(base+path,headers={} if accept is None else {'Accept':accept})
 with urllib.request.urlopen(req,timeout=3) as r:return r.read().decode()
def wait(fn):
 for _ in range(150):
  try:
   v=fn()
   if v:return v
  except Exception:pass
  time.sleep(1)
 raise RuntimeError('condition timed out')
try:
 page=get('/','text/html');assert 'Local lab alerts' in page
 assert 'id="api"' in get('/runbook')
 start=time.time();command('compose','stop','api')
 page=wait(lambda: (lambda x:x if 'LocalDrillAPIUnavailable' in x else None)(get('/','text/html')))
 (out/'active.html').write_text(page)
 events=[json.loads(x) for x in get('/events').splitlines()]
 matching=[r for r in events if r['received_unix']>=start and any(a['labels']['alertname']=='LocalDrillAPIUnavailable' and a['status']=='firing' for a in r['payload'].get('alerts',[]))]
 assert matching
 alert=matching[-1];assert alert['payload']['externalURL']=='http://localhost:19095'
 assert any(a['generatorURL'].startswith('http://localhost:19094/') for a in alert['payload']['alerts'])
 (out/'firing.json').write_text(json.dumps(alert,indent=2)+'\n')
 command('compose','up','-d','--wait','api')
 wait(lambda:get('/ready',base='http://127.0.0.1:18085'))
 page=wait(lambda:(lambda x:x if 'LocalDrillAPIUnavailable' not in x else None)(get('/','text/html')))
 (out/'resolved.html').write_text(page)
 invariant="""SELECT json_build_object(
 'done_without_result',(SELECT count(*) FROM work_items i WHERE i.status='done' AND NOT EXISTS(SELECT 1 FROM work_item_results r WHERE r.work_item_id=i.id)),
 'results_without_success',(SELECT count(*) FROM work_item_results r JOIN processing_jobs j ON j.id=r.processing_job_id WHERE j.state<>'succeeded'),
 'multiple_active_jobs',(SELECT count(*) FROM (SELECT work_item_id FROM processing_jobs WHERE state='accepted' GROUP BY 1 HAVING count(*)>1) s),
 'missing_outbox',(SELECT count(*) FROM processing_jobs j WHERE NOT EXISTS(SELECT 1 FROM outbox_messages o WHERE o.processing_job_id=j.id)))"""
 state=json.loads(command('compose','exec','-T','postgres','psql','-U','zero_to_prod_admin','-d','zero_to_prod','-Atc',invariant))
 assert all(x==0 for x in state.values()),state
 (out/'invariants.json').write_text(json.dumps(state,indent=2)+'\n')
 (out/'summary.json').write_text(json.dumps({'active_and_resolved_view':True,'runbook_anchor':True,'local_generator_and_alertmanager_links':True,'invariants':state,'note':'Separate post-run interface/pinning check, not the UI used during the owner response or the timed run.'},indent=2)+'\n')
finally:command('stop')
