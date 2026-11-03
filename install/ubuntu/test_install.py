"""Installer acceptance tests: fake user bus, isolated HOME, no host changes."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]

class InstallTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='cbl-install-', dir=os.environ.get('TMPDIR'))
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.home = self.base / 'home'
        self.home.mkdir()
        self.pkg = self.base / 'pkg'
        shutil.copytree(ROOT / 'install', self.pkg / 'install')
        old = self.pkg / 'desktop/gnome-extension/cbl@codex-limits'
        old.mkdir(parents=True)
        (old / 'metadata.json').write_text('{}')
        (self.pkg / 'desktop/app').mkdir(parents=True)
        (self.pkg / 'desktop/app/cbl-app').write_text('#!/usr/bin/python3\n')
        (self.pkg / 'desktop/app/cbl.desktop').write_text('[Desktop Entry]\nType=Application\nName=CBL\nExec=cbl-app\n')
        (self.pkg / 'desktop/indicator').mkdir(parents=True)
        (self.pkg / 'desktop/indicator/cbl-indicator.desktop').write_text('[Desktop Entry]\nType=Application\nExec=cbl-tray\n')
        for name in ['cbl', 'cbl-tray']:
            p = self.pkg / name
            p.write_text('#!/bin/sh\nexit 0\n')
            p.chmod(0o755)
        self.fake = self.base / 'fake'
        self.fake.mkdir()
        for name, body in {'systemctl': 'case "$*" in *enable*) exit "${SYSTEMCTL_FAIL:-0}";; esac\nexit 0', 'gnome-extensions': 'exit 0', 'pgrep': 'exit 1', 'pkill': 'exit 0'}.items():
            p = self.fake / name
            p.write_text('#!/bin/sh\n' + body + '\n')
            p.chmod(0o755)
        self.env = dict(os.environ, HOME=str(self.home), PATH=str(self.fake)+':'+os.environ['PATH'], DISPLAY='', WAYLAND_DISPLAY='')

    def install(self, *args):
        return subprocess.run(['bash', str(self.pkg / 'install/ubuntu/install.sh'), *args], env=self.env, capture_output=True, text=True)

    def test_default_installs_app_tray_and_preserves_accounts(self):
        auth = self.home / '.config/cbl/accounts/kept.json'
        auth.parent.mkdir(parents=True)
        auth.write_text('keep-account')
        legacy = self.home / '.local/share/gnome-shell/extensions/cbl@codex-limits'
        legacy.mkdir(parents=True)
        (legacy / 'extension.js').write_text('old')
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.home / '.local/bin/cbl-app').is_file())
        self.assertTrue((self.home / '.local/share/applications/cbl.desktop').is_file())
        self.assertTrue((self.home / '.config/autostart/cbl-indicator.desktop').is_file())
        self.assertFalse(legacy.exists())
        self.assertEqual(auth.read_text(), 'keep-account')

    def test_service_failure_is_not_success(self):
        self.env['SYSTEMCTL_FAIL'] = '1'
        result = self.install()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('Done.', result.stdout)

    def test_old_extension_flag_migrates_to_app(self):
        result = self.install('--systemd', '--extension')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.home / '.local/bin/cbl-app').is_file())
        self.assertTrue((self.home / '.config/autostart/cbl-indicator.desktop').is_file())

if __name__ == '__main__':
    unittest.main()
