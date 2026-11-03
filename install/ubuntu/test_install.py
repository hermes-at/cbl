"""Installer regression tests: isolated HOME and fake desktop/user bus."""
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
        extension = self.pkg / 'desktop/gnome-extension/cbl@codex-limits'
        extension.mkdir(parents=True)
        (extension / 'metadata.json').write_text('{"uuid":"cbl@codex-limits"}')
        (extension / 'extension.js').write_text('// popup fixture')
        (self.pkg / 'desktop/app').mkdir(parents=True)
        (self.pkg / 'desktop/app/cbl-app').write_text('#!/usr/bin/python3\n')
        (self.pkg / 'desktop/app/cbl.desktop').write_text('[Desktop Entry]\nExec=cbl-app\n')
        (self.pkg / 'desktop/indicator').mkdir(parents=True)
        (self.pkg / 'desktop/indicator/cbl-indicator.desktop').write_text('[Desktop Entry]\nType=Application\nExec=cbl-tray\n')
        for name in ['cbl', 'cbl-tray']:
            p = self.pkg / name
            p.write_text('#!/bin/sh\nexit 0\n')
            p.chmod(0o755)
        self.fake = self.base / 'fake'
        self.fake.mkdir()
        self.log = self.base / 'commands.log'
        commands = {
            'systemctl': 'printf "%s\\n" "systemctl $*" >> "$TEST_LOG"\ncase "$*" in *restart*) exit "${SYSTEMCTL_FAIL:-0}";; esac\nexit 0',
            'gnome-extensions': 'printf "%s\\n" "gnome-extensions $*" >> "$TEST_LOG"\ncase "$1" in enable) exit "${EXTENSION_FAIL:-0}";; esac\nexit 0',
            'pgrep': 'exit 1',
            'pkill': 'printf "%s\\n" "pkill $*" >> "$TEST_LOG"\nexit 0',
        }
        for name, body in commands.items():
            p = self.fake / name
            p.write_text('#!/bin/sh\n' + body + '\n'); p.chmod(0o755)
        self.env = dict(os.environ, HOME=str(self.home), PATH=str(self.fake)+':'+os.environ['PATH'], DISPLAY='', WAYLAND_DISPLAY='', TEST_LOG=str(self.log))

    def install(self, *args):
        return subprocess.run(['bash', str(self.pkg / 'install/ubuntu/install.sh'), *args], env=self.env, capture_output=True, text=True)

    def test_gnome_upgrade_restores_popup_preserves_accounts_removes_window(self):
        auth = self.home / '.config/cbl/accounts/kept.json'
        auth.parent.mkdir(parents=True); auth.write_text('keep-account')
        config = auth.parent.parent / 'config.json'; config.write_text('{"proxy_enabled":true,"proxy":"socks5h://127.0.0.1:2080"}')
        for rel in ['.local/bin/cbl-app','.local/share/applications/cbl.desktop','.config/autostart/cbl-indicator.desktop']:
            p = self.home / rel; p.parent.mkdir(parents=True, exist_ok=True); p.write_text('standalone artifact')
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        extension = self.home / '.local/share/gnome-shell/extensions/cbl@codex-limits'
        self.assertTrue((extension / 'extension.js').is_file())
        self.assertFalse((self.home / '.local/bin/cbl-app').exists())
        self.assertFalse((self.home / '.local/share/applications/cbl.desktop').exists())
        self.assertFalse((self.home / '.config/autostart/cbl-indicator.desktop').exists())
        self.assertEqual(auth.read_text(), 'keep-account')
        self.assertIn(':2080', config.read_text())
        self.assertIn('systemctl --user restart cbl.service', self.log.read_text())
        self.assertIn('gnome-extensions enable cbl@codex-limits', self.log.read_text())

    def test_service_failure_is_not_success(self):
        self.env['SYSTEMCTL_FAIL'] = '1'
        result = self.install()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('Done.', result.stdout)

    def test_explicit_extension_installs_popup(self):
        result = self.install('--systemd', '--extension')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.home / '.local/share/gnome-shell/extensions/cbl@codex-limits/metadata.json').is_file())
        self.assertFalse((self.home / '.local/bin/cbl-app').exists())

    def test_enable_failure_requests_session_restart_without_losing_install(self):
        self.env['EXTENSION_FAIL'] = '1'
        result = self.install('--extension')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('log out', result.stdout.lower())
        self.assertTrue((self.home / '.local/share/gnome-shell/extensions/cbl@codex-limits/extension.js').exists())

    def test_indicator_mode_does_not_install_window_or_extension(self):
        result = self.install('--indicator')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.home / '.config/autostart/cbl-indicator.desktop').is_file())
        self.assertFalse((self.home / '.local/bin/cbl-app').exists())
        self.assertFalse((self.home / '.local/share/gnome-shell/extensions/cbl@codex-limits').exists())

if __name__ == '__main__':
    unittest.main()
