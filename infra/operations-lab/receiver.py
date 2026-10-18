"""Local alert evidence receiver and operator view; no external messaging."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import html,json,time
from pathlib import Path
DATA=Path('/data/receipts.jsonl')
def history():
    return DATA.read_bytes() if DATA.exists() else b''
def dashboard():
    latest={}
    for line in history().decode().splitlines():
        event=json.loads(line)
        for alert in event['payload'].get('alerts',[]):
            key=alert.get('fingerprint') or json.dumps(alert['labels'],sort_keys=True)
            latest[key]=(event['received_unix'],alert)
    active=[v for v in latest.values() if v[1]['status']=='firing']
    rows=''
    for stamp,alert in sorted(active,key=lambda row:row[0],reverse=True):
        name=html.escape(alert['labels']['alertname'])
        target=html.escape(alert['labels'].get('job','asynchronous processing'))
        section=alert.get('annotations',{}).get('runbook','').split('#')[-1]
        section={'api-unavailable':'api','backlog-unobservable':'database','async-stalled':'backlog'}.get(section,section)
        section=section if section in ('api','database','broker','backlog') else ''
        rows+=f'<tr><td>{name}</td><td>{target}</td><td>{time.strftime("%H:%M:%S UTC",time.gmtime(stamp))}</td><td><a href="/runbook#{section}">Runbook</a></td></tr>'
    if not rows:rows='<tr><td colspan="4">No active alerts in the received history. This does not prove the monitoring pipeline is healthy.</td></tr>'
    return f'''<!doctype html><html lang="en"><meta charset="utf-8"><meta http-equiv="refresh" content="5"><title>Local lab alerts</title>
<style>body{{font:18px system-ui;max-width:1100px;margin:40px auto;padding:20px}}table{{border-collapse:collapse;width:100%}}td,th{{text-align:left;padding:14px;border-bottom:1px solid #ccc}}a{{color:#1558b0}}</style>
<h1>Local lab alerts</h1><p>Active received alerts: {len(active)}. Refreshes every five seconds. Local supervised lab; no 24/7 coverage.</p>
<table><tr><th>Alert</th><th>Component</th><th>Received</th><th>Next step</th></tr>{rows}</table>
<p><a href="http://localhost:19094/alerts">Live Prometheus alerts</a> · <a href="http://localhost:19095">Alertmanager</a> · <a href="/runbook">Incident runbook</a> · <a href="/events">Raw notification history</a></p>
<p>Resolved incidents stay in the history. These receipts confirm delivery to this host; they do not establish that someone acknowledged an incident.</p></html>'''.encode()
def runbook():
    content=Path('/app/runbook.md').read_text();blocks=[]
    for line in content.splitlines():
        if line.startswith('## '):
            title=line[3:];anchor=title.lower().replace(' ','-')
            blocks.append(f'<h2 id="{html.escape(anchor)}">{html.escape(title)}</h2>')
        else:blocks.append(html.escape(line)+'\n')
    return ('<!doctype html><html lang="en"><meta charset="utf-8"><title>Local incident runbook</title><style>body{font:17px system-ui;max-width:1000px;margin:30px auto;padding:20px;white-space:pre-wrap}</style><a href="/">Alerts</a>\n'+''.join(blocks)).encode()
class Handler(BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_GET(self):
        if self.path.split('?')[0]=='/runbook':raw=runbook();kind='text/html; charset=utf-8'
        elif self.path.split('?')[0]=='/events':raw=history();kind='application/x-ndjson'
        elif self.path.split('?')[0]=='/' and 'text/html' in self.headers.get('Accept',''):raw=dashboard();kind='text/html; charset=utf-8'
        elif self.path.split('?')[0]=='/':raw=history();kind='application/x-ndjson'
        else:self.send_error(404);return
        self.send_response(200);self.send_header('Content-Type',kind);self.end_headers();self.wfile.write(raw)
    def do_POST(self):
        if self.path!='/alerts':self.send_error(404);return
        try:size=int(self.headers.get('Content-Length','0'))
        except ValueError:self.send_error(400);return
        if size>1048576:self.send_error(413);return
        try:payload=json.loads(self.rfile.read(size))
        except ValueError:self.send_error(400);return
        if not isinstance(payload,dict) or not isinstance(payload.get('alerts'),list):self.send_error(400);return
        if any(not isinstance(a,dict) or not isinstance(a.get('labels'),dict) or a.get('status') not in ('firing','resolved') for a in payload['alerts']):self.send_error(400);return
        with DATA.open('a') as f:f.write(json.dumps({'received_unix':time.time(),'payload':payload})+'\n')
        self.send_response(200);self.end_headers()
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
