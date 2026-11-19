#!/usr/bin/env python3
"""Exercise the actual webhook HTTP handler and persisted receipt contract."""
import importlib.util
import json
from pathlib import Path
import tempfile
import sys
import threading
import unittest
import urllib.error
import urllib.request
from http.server import HTTPServer

path = Path(__file__).resolve().parents[1] / 'infra/operations-lab/receiver.py'
spec = importlib.util.spec_from_file_location('receiver', path)
receiver = importlib.util.module_from_spec(spec)
sys.dont_write_bytecode = True
spec.loader.exec_module(receiver)


class ReceiverTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        receiver.DATA = Path(self.temp.name) / 'receipts.jsonl'
        self.server = HTTPServer(('127.0.0.1', 0), receiver.Handler)
        self.thread = threading.Thread(target=self.server.serve_forever)
        self.thread.start()
        self.base = 'http://127.0.0.1:' + str(self.server.server_port)
        self.payload = {'alerts': [{'status': 'firing', 'labels': {'alertname': 'LocalDrillAPIUnavailable'}}]}

    def tearDown(self):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()
        self.temp.cleanup()

    def post(self, payload):
        request = urllib.request.Request(self.base + '/alerts', data=json.dumps(payload).encode(), headers={'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(request, timeout=2) as response:
                return response.status
        except urllib.error.HTTPError as error:
            code = error.code
            error.close()
            return code

    def test_receipt_and_resolution_are_saved_and_visible(self):
        self.assertEqual(self.post(self.payload), 200)
        self.payload['alerts'][0]['status'] = 'resolved'
        self.assertEqual(self.post(self.payload), 200)
        with urllib.request.urlopen(self.base + '/events', timeout=2) as response:
            receipts = [json.loads(line) for line in response.read().splitlines()]
        self.assertEqual([r['payload']['alerts'][0]['status'] for r in receipts], ['firing', 'resolved'])
        request = urllib.request.Request(self.base, headers={'Accept': 'text/html'})
        with urllib.request.urlopen(request, timeout=2) as response:
            self.assertIn(b'Active received alerts: 0', response.read())

    def test_missing_alert_name_is_rejected_before_persistence(self):
        del self.payload['alerts'][0]['labels']['alertname']
        self.assertEqual(self.post(self.payload), 400)
        self.assertFalse(receiver.DATA.exists())

    def test_storage_failure_is_retryable_and_not_acknowledged(self):
        receiver.DATA = Path(self.temp.name) / 'missing' / 'receipts.jsonl'
        self.assertEqual(self.post(self.payload), 503)

    def test_invalid_status_is_rejected(self):
        self.payload['alerts'][0]['status'] = 'unknown'
        self.assertEqual(self.post(self.payload), 400)


if __name__ == '__main__':
    unittest.main()
