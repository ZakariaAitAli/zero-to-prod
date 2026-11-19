#!/usr/bin/env python3
"""Execute the control's real shell functions with a recording Docker boundary."""
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SOURCE = (ROOT / 'tools/security-lab-local').read_text()


def function(name, next_name):
    start = SOURCE.index('\n' + name + '() {')
    end = SOURCE.index('\n' + next_name + '() {', start)
    return SOURCE[start:end]


FUNCTIONS = ''.join([
    function('tool_network_create', 'check_configuration'),
    function('start_baseline_storage', 'start_baseline_api'),
    function('start_baseline_api', 'stop_baseline_api'),
    function('stop_baseline_api', 'restore_baseline_api'),
    function('restore_baseline_api', 'build_baseline_api_image'),
])


class AuthorizationIsolationTest(unittest.TestCase):
    def exercise(self, fail_probe=False):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'bin').mkdir()
            (work / 'auth').mkdir()
            docker = work / 'bin/docker'
            docker.write_text('''#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
with open(os.environ['ZTP_TEST_DOCKER_LOG'], 'a') as log:
    log.write(json.dumps(args) + '\\n')
if '-i' in args and 'psql' in args:
    sys.stdin.read()
if os.environ.get('ZTP_TEST_FAIL_PROBE') == '1' and args[:1] == ['run'] and 'probe-db' in args:
    sys.exit(1)
''')
            docker.chmod(0o755)
            variables = {
                'ROOT_DIR': str(ROOT), 'AUTH_WORK': str(work / 'auth'),
                'PROJECT': 'zero-to-prod-security-lab-test', 'SECRETS_DIR': str(work / 'secrets'),
                'PKI_DIR': str(work / 'pki'), 'INTERNAL_TLS_DIR': str(work / 'internal-tls'),
                'APP_TLS_DIR': str(work / 'app-tls'), 'INTERNAL_CA_MOUNT': '/internal-ca/ca.crt',
                'BASELINE_API': 'probe-api', 'BASELINE_SHIM': 'probe-shim',
                'BASELINE_POSTGRES': 'probe-db', 'BASELINE_DATA': 'probe-data',
                'POSTGRES_IMAGE': 'pinned-postgres', 'INGRESS_IMAGE': 'pinned-ingress',
                'ISSUER': 'https://auth.work-items.localhost:9443/realms/work-items',
                'API_AUDIENCE': 'work-items-api', 'API_TLS_PORT': '8443',
            }
            preamble = 'set -euo pipefail\n' + '\n'.join(k + '=' + shlex.quote(v) for k, v in variables.items())
            preamble += '''
BASELINE_API_SWAPPED=false
compose() { docker compose "$@"; }
die() { echo "$*" >&2; exit 1; }
sleep() { :; }
api_version() { echo baseline-test; }
'''
            script = preamble + FUNCTIONS + '''
trap 'restore_baseline_api' EXIT
start_baseline_api baseline-image
'''
            env = dict(os.environ, PATH=str(work / 'bin') + os.pathsep + os.environ['PATH'],
                       ZTP_TEST_DOCKER_LOG=str(work / 'calls.jsonl'), ZTP_TEST_FAIL_PROBE='1' if fail_probe else '0')
            result = subprocess.run(['bash', '-c', script], env=env, capture_output=True, text=True)
            calls = [json.loads(line) for line in (work / 'calls.jsonl').read_text().splitlines()]
            broker = work / 'auth/baseline-broker-url'
            return result, calls, broker.read_text() if broker.exists() else None

    def test_control_cannot_use_current_database_or_broker(self):
        result, calls, broker = self.exercise()
        self.assertEqual(result.returncode, 0, result.stderr)
        api = next(c for c in calls if c[:1] == ['create'] and 'probe-api' in c)
        self.assertEqual(api[api.index('--network') + 1], 'probe-data')
        self.assertNotIn('zero-to-prod-security-lab-test_data', api)
        mounts = [api[i + 1] for i, arg in enumerate(api) if arg == '-v']
        self.assertTrue(any('/auth/baseline-broker-url:/run/secrets/api-rabbitmq-url:ro' in m for m in mounts))
        self.assertFalse(any(m.split(':', 1)[0].endswith('/secrets/api-rabbitmq-url') for m in mounts))
        self.assertIn('@127.0.0.1:1/', broker)
        database = next(c for c in calls if c[:1] == ['run'] and 'probe-db' in c)
        self.assertEqual(database[database.index('--network') + 1], 'probe-data')
        self.assertIn('/var/lib/postgresql:rw,nosuid,nodev,size=256m', database)
        self.assertNotIn('-p', database)
        migrations = next(c for c in calls if c[:2] == ['exec', '-i'] and 'psql' in c)
        self.assertEqual(migrations[2], 'probe-db')
        self.assertTrue(any(c[:2] == ['network', 'rm'] and 'probe-data' in c for c in calls))
        self.assertTrue(any(c[:3] == ['rm', '-fv', 'probe-shim'] and 'probe-db' in c for c in calls))
        self.assertTrue(any(c[:3] == ['compose', 'up', '-d'] and c[-1] == 'api' for c in calls))

    def test_storage_start_failure_restores_current_api(self):
        result, calls, _ = self.exercise(fail_probe=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c[:1] == ['create'] for c in calls))
        self.assertTrue(any(c[:2] == ['network', 'rm'] and 'probe-data' in c for c in calls))
        self.assertTrue(any(c[:3] == ['compose', 'up', '-d'] and c[-1] == 'api' for c in calls))
        self.assertFalse(any('zero-to-prod-security-lab-test-postgres-1' in c for c in calls))


if __name__ == '__main__':
    unittest.main()
