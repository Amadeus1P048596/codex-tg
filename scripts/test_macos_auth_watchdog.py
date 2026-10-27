import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("watchdog", Path(__file__).with_name("configure-macos-auth-watchdog.py"))
watchdog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(watchdog)


class ConfigureTests(unittest.TestCase):
    def test_private_idempotent_config_without_secret_copy(self):
        with tempfile.TemporaryDirectory() as root:
            config, source = Path(root)/"config.env", Path(root)/"auth.json"
            config.write_text('# retained\nCTR_GO_CODEX_HOME="/private/runtime"\n')
            original = json.dumps({"tokens": {"account_id": "test-account", "access_token": "never-copy-access", "refresh_token": "never-copy-refresh"}})
            source.write_text(original)
            watchdog.configure(config, source)
            first = config.read_text()
            watchdog.configure(config, source)
            self.assertEqual(first, config.read_text())
            self.assertEqual(original, source.read_text())
            self.assertNotIn("never-copy", first)
            self.assertIn("# retained", first)
            self.assertEqual(0o600, config.stat().st_mode & 0o777)

    def test_bad_source_preserves_config(self):
        with tempfile.TemporaryDirectory() as root:
            config, source = Path(root)/"config.env", Path(root)/"auth.json"
            config.write_text("original\n")
            source.write_text("{}")
            with self.assertRaises(ValueError):
                watchdog.configure(config, source)
            self.assertEqual("original\n", config.read_text())


if __name__ == "__main__":
    unittest.main()
