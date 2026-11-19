#!/usr/bin/env python3
"""Regression for admission blindness, dropped arrivals and response classification."""
import runpy
import json
import subprocess
import tempfile
import ssl
import threading
import unittest
from unittest.mock import patch
from pathlib import Path

module = runpy.run_path(str(Path(__file__).resolve().parents[1] / 'tools/security-lab-capacity'))
summarize = module['summarize']


class AdmissionSLITest(unittest.TestCase):
    def test_state_marker_and_privacy_must_match_selected_lab(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / '.zero-to-prod-security-lab').write_text('project=selected\n')
            module['validate_state'](state, 'selected')
            with self.assertRaises(ValueError):
                module['validate_state'](state, 'another-project')
            state.chmod(0o755)
            with self.assertRaises(ValueError):
                module['validate_state'](state, 'selected')

    def test_authentication_cannot_leave_verified_lab_origins(self):
        validate = module['require_lab_url']
        for url in ['https://work-items.localhost:9443/a', 'https://auth.work-items.localhost:9443/b']:
            validate(url)
        for url in ['http://work-items.localhost:9443/a', 'https://example.com:9443/',
                    'https://work-items.localhost.evil:9443/', 'https://work-items.localhost:443/',
                    'https://user:password@work-items.localhost:9443/']:
            with self.assertRaises(ValueError):
                validate(url)

    def test_process_rejection_is_not_a_completed_workflow(self):
        iteration = module['workflow_iteration']
        responses = [(201, .01, None, b'{"id": 1}'), (429, .01, '1', b'{}')]
        with patch.dict(iteration.__globals__, safe_request=unittest.mock.Mock(side_effect=responses)):
            result = iteration(None, 'session', 'synthetic-', 1)
        self.assertFalse(result['accepted'])
        self.assertFalse(result['complete'])
        self.assertEqual([r[0] for r in result['records']], [201, 429])

    def test_read_failure_preserves_observed_acceptance(self):
        iteration = module['workflow_iteration']
        responses = [(201, .01, None, b'{"id": 1}'), (202, .01, None, b'{"id": 2}'), (0, 10, None, b'')]
        with patch.dict(iteration.__globals__, safe_request=unittest.mock.Mock(side_effect=responses)):
            result = iteration(None, 'session', 'synthetic-', 1)
        self.assertTrue(result['accepted'])
        self.assertFalse(result['complete'])
        self.assertEqual(len(result['records']), 3)

    def test_wrong_job_response_is_not_success(self):
        iteration = module['workflow_iteration']
        responses = [(201, .01, None, b'{"id": 1}'), (202, .01, None, b'{"id": 2}'), (200, .01, None, b'{"id": 3}')]
        with patch.dict(iteration.__globals__, safe_request=unittest.mock.Mock(side_effect=responses)):
            result = iteration(None, 'session', 'synthetic-', 1)
        self.assertTrue(result['accepted'])
        self.assertFalse(result['complete'])
        self.assertTrue(result['protocol_error'])

    def test_failed_login_does_not_expose_callback_url(self):
        login = module['browser_cookie']
        opener = unittest.mock.Mock()
        opener.open.side_effect = RuntimeError('callback?code=sensitive-code')
        with patch('urllib.request.build_opener', return_value=opener):
            with self.assertRaises(RuntimeError) as caught:
                login(None, Path('/unused'))
        self.assertNotIn('sensitive-code', str(caught.exception))
        self.assertTrue(caught.exception.__suppress_context__)

    def test_malformed_identifier_cannot_select_another_route(self):
        for body in [b'{"id":"../admin"}', b'{"id":true}', b'{"id":0}', b'[]', b'broken']:
            self.assertIsNone(module['response_id'](body))

    def test_worker_exception_cannot_disappear_from_report(self):
        def fail(_):
            raise RuntimeError('worker failed')
        with self.assertRaisesRegex(RuntimeError, 'worker failed'):
            module['bounded_arrivals'](fail, 1, 1)

    def test_empty_resource_response_is_not_a_zero_cost_sample(self):
        samples, errors = [], []
        with patch('subprocess.check_output', return_value=''):
            module['resource_samples'](['container'], threading.Event(), samples, errors)
        self.assertEqual(samples, [])
        self.assertEqual(errors, ['ValueError'])

    def test_security_workflow_report_keeps_failed_verification_visible(self):
        stage = {
            'stage': 'overload', 'resources': [], 'drain_s': .1,
            'jobs': {'accepted': 1, 'succeeded': 1, 'within_30_seconds': 1, 'latency_p99_s': .1,
                     'pending': 0, 'failed': 0, 'missing_outbox': 0, 'succeeded_without_result': 0, 'result_without_success': 0},
            'including_tail_s': 1, 'request_p99_s': .1, 'attempted_requests': 4,
            'server_error_availability': 1, 'successful_within_250ms_fraction': .25,
            'client_dropped_iterations': 0, 'status_counts': {'201': 1, '202': 1, '200': 1, '429': 1},
            'scheduled_iterations': 2,
        }
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, 'summary.json').write_text(json.dumps({
                'workload_kind': 'workflow', 'workload': 'browser create/process/read', 'passed': False, 'stages': [stage]}))
            script = Path(__file__).resolve().parents[1] / 'tools/capacity-lab-report'
            report = json.loads(subprocess.check_output(['python3', str(script), directory], text=True))[0]
        self.assertEqual(report['profile'], 'security-lab')
        self.assertFalse(report['verification_passed'])
        self.assertEqual(report['successful_request_fraction'], .75)
        self.assertEqual(report['accepted_iteration_fraction'], .5)

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

    def test_cost_model_uses_explicit_prices_and_resource_units(self):
        stage = {
            'stage': 'sustained', 'resources': [{'containers': [{'Name': 'stack-api-1', 'CPUPerc': '100%', 'MemUsage': '1GiB / 2GiB'}]}],
            'jobs': {'accepted': 1000, 'succeeded': 1000, 'within_30_seconds': 990, 'latency_p99': 30},
            'including_tail_s': 3590, 'drain_seconds': 10,
            'requests_per_second_including_tail': 1, 'request_p99_s': .1,
            'availability_fraction': 1, 'within_250ms_fraction': 1,
            'client_dropped_iterations': 0, 'status_counts': {'200': 3000},
            'requests': 3000, 'scheduled_iterations': 1000, 'invariants': {},
        }
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, 'sustained.json').write_text(json.dumps(stage))
            script = Path(__file__).resolve().parents[1] / 'tools/capacity-lab-report'
            report = json.loads(subprocess.check_output(['python3', str(script), directory,
                '--cpu-hour-price', '2', '--gib-hour-price', '3', '--currency', 'test-units'], text=True))[0]
            rejected = subprocess.run(['python3', str(script), directory, '--cpu-hour-price', 'nan',
                '--gib-hour-price', '3'], capture_output=True)
        self.assertEqual(report['cost_model']['sampled_resource_cost_for_stage'], 5)
        self.assertEqual(report['cost_model']['sampled_resource_cost_per_1000_successes'], 5)
        self.assertEqual(report['freshness_of_accepted_fraction'], .99)
        self.assertNotEqual(rejected.returncode, 0)

    def test_no_traffic_is_not_a_pass(self):
        result = summarize([], 0, 0)
        self.assertIsNone(result['server_error_availability'])
        self.assertIsNone(result['successful_valid_work_fraction'])
        self.assertIsNone(result['request_p99_s'])


if __name__ == '__main__':
    unittest.main()
