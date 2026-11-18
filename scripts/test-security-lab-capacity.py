#!/usr/bin/env python3
"""Regression for admission blindness, dropped arrivals and response classification."""
import runpy
import json
import subprocess
import tempfile
import ssl
import unittest
from unittest.mock import patch
from pathlib import Path

module = runpy.run_path(str(Path(__file__).resolve().parents[1] / 'tools/security-lab-capacity'))
summarize = module['summarize']


class AdmissionSLITest(unittest.TestCase):
    def test_rejected_valid_work_is_not_success(self):
        result = summarize([(200, .1, None), (429, .01, '1')], 2, 0)
        self.assertEqual(result['server_error_availability'], 1)
        self.assertEqual(result['successful_valid_work_fraction'], .5)
        self.assertEqual(result['successful_within_250ms_fraction'], .5)
        self.assertEqual(result['rejections_with_retry_after'], 1)

    def test_client_drops_stay_in_user_denominator(self):
        result = summarize([(200, .3, None)], 2, 1)
        self.assertEqual(result['successful_valid_work_fraction'], .5)
        self.assertEqual(result['successful_within_250ms_fraction'], 0)
        self.assertEqual(result['client_dropped_requests'], 1)

    def test_transport_and_server_errors_are_bad(self):
        result = summarize([(0, 10, None), (503, .1, None), (401, .1, None)], 3, 0)
        self.assertAlmostEqual(result['server_error_availability'], 1 / 3)
        self.assertEqual(result['successful_valid_work_fraction'], 0)

    def test_tls_connect_keeps_verified_name_and_closes_failed_socket(self):
        connection_type = module['LoopbackHTTPS']
        context = unittest.mock.Mock()
        context.verify_mode = 2
        context.check_hostname = True
        context.wrap_socket.side_effect = ssl.SSLError('invalid certificate')
        raw = unittest.mock.Mock()
        with patch('socket.create_connection', return_value=raw) as dial:
            connection = connection_type('work-items.localhost', 9443, context=context, timeout=10)
            with self.assertRaises(ssl.SSLError):
                connection.connect()
        dial.assert_called_once_with(('127.0.0.1', 9443), 10)
        context.wrap_socket.assert_called_once_with(raw, server_hostname='work-items.localhost')
        raw.close.assert_called_once()

    def test_existing_capacity_reports_expose_shed_work(self):
        # Old stage files have no new ratio fields: derive them from raw counts.
        stage = {
            'stage': 'limited', 'resources': [],
            'jobs': {'accepted': 1, 'succeeded': 1, 'within_30_seconds': 1, 'latency_p99': .1},
            'including_tail_s': 1, 'drain_seconds': 0,
            'requests_per_second_including_tail': 4, 'request_p99_s': .1,
            'availability_fraction': 1, 'within_250ms_fraction': 1,
            'client_dropped_iterations': 1, 'status_counts': {'200': 1, '429': 3},
            'requests': 4, 'scheduled_iterations': 2, 'invariants': {},
        }
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, 'limited.json').write_text(json.dumps(stage))
            script = Path(__file__).resolve().parents[1] / 'tools/capacity-lab-report'
            report = json.loads(subprocess.check_output(['python3', str(script), directory], text=True))[0]
        self.assertEqual(report['availability'], 1)
        self.assertEqual(report['successful_request_fraction'], .25)
        self.assertEqual(report['accepted_iteration_fraction'], .5)

    def test_no_traffic_is_not_a_pass(self):
        result = summarize([], 0, 0)
        self.assertIsNone(result['server_error_availability'])
        self.assertIsNone(result['successful_valid_work_fraction'])
        self.assertIsNone(result['request_p99_s'])


if __name__ == '__main__':
    unittest.main()
