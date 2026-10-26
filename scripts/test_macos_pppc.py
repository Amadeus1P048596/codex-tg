import importlib.util
import plistlib
import unittest
from unittest import mock
import subprocess
from pathlib import Path

spec = importlib.util.spec_from_file_location('pppc', Path(__file__).with_name('generate-macos-pppc.py'))
pppc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pppc)

REQUIREMENT = 'identifier "tech.mideco.codex-tg.tray" and anchor apple generic and certificate leaf[subject.OU] = "EXAMPLETEAM"'

class ProfileTests(unittest.TestCase):
    def test_profile_binds_app_and_installed_daemon(self):
        profile = pppc.make_profile('tech.mideco.codex-tg.tray', REQUIREMENT, REQUIREMENT.replace('.tray', '.daemon'), '/Applications/codex-tg.app')
        decoded = plistlib.loads(plistlib.dumps(profile))
        policy = decoded['PayloadContent'][0]
        self.assertEqual(policy['PayloadType'], 'com.apple.TCC.configuration-profile-policy')
        entries = policy['Services']['SystemPolicyAllFiles']
        self.assertEqual(len(entries), 2)
        self.assertEqual(entries[0]['IdentifierType'], 'bundleID')
        self.assertEqual(entries[1]['Identifier'], '/Applications/codex-tg.app/Contents/MacOS/ctr-go')
        self.assertTrue(all(e['Allowed'] and e['CodeRequirement'] for e in entries))
        self.assertEqual(list(policy['Services']), ['SystemPolicyAllFiles'])
        self.assertNotIn('/tmp/', plistlib.dumps(profile).decode())

    def test_rejects_unsigned_or_unbound_identity(self):
        for requirement in ['', 'identifier "a.out"', 'identifier "app" and cdhash H"abcd"']:
            with self.assertRaises(ValueError):
                pppc.make_profile('app', requirement, REQUIREMENT, '/Applications/codex-tg.app')

    def test_signature_rejects_ad_hoc_build(self):
        results = [subprocess.CompletedProcess([], 0, '', ''),
                   subprocess.CompletedProcess([], 0, '', 'Signature=adhoc\nTeamIdentifier=not set\n')]
        with mock.patch.object(pppc.subprocess, 'run', side_effect=results):
            with self.assertRaises(ValueError):
                pppc.signature(Path('codex-tg.app'))

    def test_rejects_relative_deployment_path(self):
        with self.assertRaises(ValueError):
            pppc.make_profile('app', REQUIREMENT, REQUIREMENT, 'staging/codex-tg.app')

    def test_profile_ids_stay_stable_across_redeploys(self):
        one = pppc.make_profile('app', REQUIREMENT, REQUIREMENT, '/Applications/codex-tg.app')
        two = pppc.make_profile('app', REQUIREMENT, REQUIREMENT, '/Applications/codex-tg.app')
        self.assertEqual(one, two)

if __name__ == '__main__':
    unittest.main()
