#!/usr/bin/env python3
"""Ensure running a recovery rehearsal never defaults to volume destruction."""
import copy
import runpy
import unittest
from pathlib import Path

module = runpy.run_path(str(Path(__file__).resolve().parents[1] / 'tools/availability-lab-experiments'))
parse_args = module['parse_args']
validate = module['validate_primary_storage']
PROJECT = module['PROJECT']


class SafetyTests(unittest.TestCase):
    def test_default_preserves_primary(self):
        self.assertFalse(parse_args(['new-report']).destroy_primary_volume)

    def test_destruction_requires_explicit_option(self):
        self.assertTrue(parse_args(['new-report', '--destroy-primary-volume']).destroy_primary_volume)

    def test_primary_storage_fencing_and_ownership(self):
        original = {'State': {'Running': False}, 'Config': {'Labels': {'com.docker.compose.project': PROJECT}},
                    'Mounts': [{'Type': 'volume', 'Destination': '/var/lib/postgresql', 'Name': PROJECT + '_postgres_data'}]}
        volume = {'Name': PROJECT + '_postgres_data', 'Labels': {'com.docker.compose.project': PROJECT}}
        self.assertEqual(validate(original, volume), volume['Name'])
        cases = []
        running = copy.deepcopy(original); running['State']['Running'] = True
        cases.append((running, volume))
        foreign = copy.deepcopy(original); foreign['Config']['Labels']['com.docker.compose.project'] = 'another-project'
        cases.append((foreign, volume))
        bind = copy.deepcopy(original); bind['Mounts'][0]['Type'] = 'bind'
        cases.append((bind, volume))
        ambiguous = copy.deepcopy(original); ambiguous['Mounts'] *= 2
        cases.append((ambiguous, volume))
        cases.append((original, {**volume, 'Name': 'unrelated-data'}))
        cases.append((original, {**volume, 'Labels': {}}))
        for container, storage in cases:
            with self.subTest(container=container, storage=storage):
                with self.assertRaises(RuntimeError):
                    validate(container, storage)


if __name__ == '__main__':
    unittest.main()
