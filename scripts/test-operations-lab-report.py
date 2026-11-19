#!/usr/bin/env python3
"""Regression cases for missing evidence and operations SLO denominators."""
import json
import subprocess
import sys
import tempfile
import runpy
import unittest
from pathlib import Path

summarize = runpy.run_path(str(Path(__file__).resolve().parents[1] / 'tools/operations-lab-report'))['summarize']


class ReportTests(unittest.TestCase):
    def setUp(self):
        self.rows = [{'start': 100, 'status': 200, 'seconds': .01},
                     {'start': 101, 'status': 429, 'seconds': .02},
                     {'start': 102, 'status': 0, 'seconds': 3}]
        self.incidents = [{'service': 'api', 'fault_unix': 1, 'notification_unix': 2,
                           'script_ack_unix': 3, 'business_recovered_unix': 4,
                           'resolved_notification_unix': 5}]
        self.incidents = [{**self.incidents[0], 'service': name} for name in ('api', 'postgres', 'rabbitmq', 'worker')]
        self.samples = [{'unix': t, 'containers': [{'CPUPerc': '50%', 'MemUsage': '1GiB / 2GiB'}]} for t in (1, 11)]
        self.db = {'jobs': 4, 'succeeded': 2, 'fresh': 1, 'accepted': 2}

    def report(self):
        return summarize(self.rows, self.incidents, self.samples, self.db)

    def test_separates_availability_from_success(self):
        result = self.report()
        self.assertEqual(result['observed_request_availability'], 2/3)
        self.assertEqual(result['successful_response_ratio'], 1/3)
        self.assertFalse(result['run_slo_comparison']['request_availability']['meets_target_in_run'])

    def test_freshness_distinguishes_completions_and_unfinished_work(self):
        result = self.report()
        self.assertEqual(result['observed_completed_within_30s_ratio'], .5)
        self.assertEqual(result['accepted_work_completed_within_30s_ratio'], .25)

    def test_empty_requests_and_jobs_are_unknown(self):
        self.rows = []
        self.db = {'jobs': 0, 'succeeded': 0, 'fresh': 0}
        result = self.report()
        self.assertIsNone(result['observed_request_availability'])
        self.assertIsNone(result['resource_estimates']['cpu_seconds_per_1000_succeeded'])
        self.assertFalse(result['evidence_complete'])

    def test_incomplete_incident_is_retained(self):
        del self.incidents[0]['business_recovered_unix']
        result = self.report()
        self.assertFalse(result['evidence_complete'])
        self.assertEqual(len(result['incomplete_incidents']), 1)

    def test_missing_drill_is_incomplete(self):
        self.incidents.pop()
        self.assertFalse(self.report()['evidence_complete'])

    def test_no_incidents_is_incomplete(self):
        self.incidents = []
        self.assertFalse(self.report()['evidence_complete'])

    def test_out_of_order_timing_is_not_reported_as_recovery(self):
        self.incidents[0]['notification_unix'] = 20
        self.assertEqual(len(self.report()['incidents']), 3)

    def test_missing_resource_samples_are_unknown(self):
        self.samples[0]['containers'] = []
        result = self.report()
        self.assertIsNone(result['resource_estimates']['cpu_core_seconds'])
        self.assertFalse(result['evidence_complete'])

    def test_resource_gaps_are_excluded(self):
        self.samples[1]['unix'] = 100
        self.assertIsNone(self.report()['resource_estimates']['mean_cpu_cores'])

    def test_sample_integral(self):
        result = self.report()['resource_estimates']
        self.assertEqual(result['cpu_core_seconds'], 5)
        self.assertEqual(result['mean_memory_gib'], 1)

    def test_rejects_inconsistent_job_counts(self):
        self.db['fresh'] = 5
        with self.assertRaises(ValueError):
            self.report()

    def test_rejects_nonfinite_resource_values(self):
        self.samples[0]['containers'][0]['CPUPerc'] = 'NaN%'
        with self.assertRaises(ValueError):
            self.report()

    def test_cli_cost_records_supplied_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, value in [('requests', self.rows), ('incidents', self.incidents),
                                ('resources', self.samples), ('database', self.db),
                                ('summary', {'collection_errors': []})]:
                (root / (name + '.json')).write_text(json.dumps(value))
            command = [sys.executable, str(Path(__file__).resolve().parents[1] / 'tools/operations-lab-report'), directory]
            result = subprocess.run(command + ['--cpu-core-hour-price', '1', '--gib-hour-price', '2'], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            report = json.loads(result.stdout)
            self.assertAlmostEqual(report['modeled_usage_cost']['amount'], 25/3600)
            result = subprocess.run(command + ['--cpu-core-hour-price', '1'], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            result = subprocess.run(command + ['--cpu-core-hour-price', 'NaN', '--gib-hour-price', '1'], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)

    def test_cli_excludes_warmup_from_the_measured_window(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, value in [('requests', self.rows), ('incidents', self.incidents),
                                ('resources', self.samples), ('database', self.db),
                                ('summary', {'collection_errors': [], 'traffic_start_unix': 101})]:
                (root / (name + '.json')).write_text(json.dumps(value))
            result = subprocess.run([sys.executable, str(Path(__file__).resolve().parents[1] / 'tools/operations-lab-report'), directory], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            report = json.loads(result.stdout)
            self.assertEqual(report['warmup_requests_excluded'], 1)
            self.assertEqual(report['request_count'], 2)
            self.assertEqual(report['observed_request_availability'], .5)

    def test_cli_fails_on_collection_errors_and_keeps_analysis(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, value in [('requests', self.rows), ('incidents', self.incidents),
                                ('resources', self.samples), ('database', self.db),
                                ('summary', {'collection_errors': ['sample command failed']})]:
                (root / (name + '.json')).write_text(json.dumps(value))
            result = subprocess.run([sys.executable, str(Path(__file__).resolve().parents[1] / 'tools/operations-lab-report'), directory], capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            self.assertIn('sample command failed', json.loads((root / 'analysis.json').read_text())['evidence_errors'])


if __name__ == '__main__':
    unittest.main()
