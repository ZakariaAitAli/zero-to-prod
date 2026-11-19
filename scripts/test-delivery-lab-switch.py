#!/usr/bin/env python3
"""Exercise traffic-switch rollback when gateway validation or reload fails."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

DRIVER = Path(__file__).resolve().parents[1] / 'tools/delivery-lab-local'


class SwitchTests(unittest.TestCase):
    def run_switch(self, fail):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / 'docker'
            binary.write_text('''#!/usr/bin/env python3
import os,sys
args=sys.argv[1:]
failure=os.environ.get('FAKE_FAILURE')
if failure=='readiness' and 'wget' in args:sys.exit(1)
if failure=='validation' and args[-2:]==['nginx','-t']:sys.exit(1)
if failure=='reload' and args[-3:]==['nginx','-s','reload']:sys.exit(1)
''')
            binary.chmod(0o755)
            state = root / 'state'
            state.mkdir()
            config = state / 'nginx.conf'
            config.write_text('original active blue configuration\n')
            result = subprocess.run(['bash', str(DRIVER), 'switch', 'green'],
                env={**os.environ, 'PATH': str(root) + ':' + os.environ['PATH'],
                     'DELIVERY_STATE': str(state), 'FAKE_FAILURE': fail}, capture_output=True, text=True)
            return result.returncode, config.read_text()

    def test_failed_readiness_preserves_previous_config(self):
        code, config = self.run_switch('readiness')
        self.assertNotEqual(code, 0)
        self.assertEqual(config, 'original active blue configuration\n')

    def test_failed_validation_restores_previous_config(self):
        code, config = self.run_switch('validation')
        self.assertNotEqual(code, 0)
        self.assertEqual(config, 'original active blue configuration\n')

    def test_failed_reload_restores_previous_config(self):
        code, config = self.run_switch('reload')
        self.assertNotEqual(code, 0)
        self.assertEqual(config, 'original active blue configuration\n')

    def test_success_keeps_candidate_config(self):
        code, config = self.run_switch('')
        self.assertEqual(code, 0)
        self.assertIn('server green:8080;', config)


if __name__ == '__main__':
    unittest.main()
