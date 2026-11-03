"""Opt-in real GTK acceptance; all HTTP responses are local test fixtures.
Run: CBL_GUI_TEST=1 DISPLAY=:... dbus-run-session -- /usr/bin/python3 -m unittest discover -s desktop/app -p test_gui.py -v
"""
import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch


@unittest.skipUnless(os.environ.get('CBL_GUI_TEST') == '1', 'requires opt-in virtual display and private D-Bus session')
class NativeGuiTests(unittest.TestCase):
    def test_real_window_actions_and_single_instance(self):
        import gi
        gi.require_version('Gtk', '3.0')
        from gi.repository import Gtk, GLib, Gio, Gdk
        launcher = Path(__file__).with_name('cbl-app')
        loader = importlib.machinery.SourceFileLoader('cbl_gui_acceptance', str(launcher))
        spec = importlib.util.spec_from_loader(loader.name, loader)
        module = importlib.util.module_from_spec(spec)
        loader.exec_module(module)
        calls = []
        settings = {'ok': True, 'proxy_enabled': False, 'proxy': ''}
        failures = [True]
        status = {'ok': True, 'accounts': [
            {'account_id': 'test-good', 'account_label': 'TEST — healthy account', 'plan': 'plus', 'windows': [
                {'title': '5h', 'used': 25, 'remaining': 75, 'reset': 'TEST reset'},
                {'title': 'week', 'used': 40, 'remaining': 60}], 'credits': {'text': '10.00'}},
            {'account_id': 'test-broken', 'account_label': 'TEST — unavailable account', 'error': 'TEST upstream unavailable', 'windows': [], 'credits': {'text': '—'}},
        ]}
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_GET(self): self.respond()
            def do_POST(self): self.respond()
            def respond(self):
                body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                calls.append((self.command, self.path, body))
                code = 200
                if self.path == '/api/settings':
                    if body: settings.update(json.loads(body))
                    data = settings
                elif self.path == '/api/login/start':
                    data = {'ok': True, 'id': 'test-session', 'user_code': 'TEST-CODE', 'verification_url': 'https://example.invalid/test-only'}
                elif self.path.startswith('/api/login/complete'):
                    if failures:
                        failures.pop(); code = 502; data = {'ok': False, 'error': 'TEST transient login failure'}
                    else: data = {'ok': True}
                else: data = status
                self.send_response(code); self.end_headers(); self.wfile.write(json.dumps(data).encode())
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close); self.addCleanup(server.shutdown)
        module.API = f'http://127.0.0.1:{server.server_port}'
        app = module.create_application()
        self.assertTrue(app.register(None))
        self.assertFalse(app.get_is_remote())
        app.activate()
        window = app.props.active_window
        self.addCleanup(window.destroy)
        def wait(predicate):
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                while GLib.MainContext.default().pending(): GLib.MainContext.default().iteration(False)
                if predicate(): return
                time.sleep(.01)
            self.fail('GTK action timed out')
        def children(widget):
            yield widget
            if isinstance(widget, Gtk.Container):
                for child in widget.get_children(): yield from children(child)
        def labels(): return [w.get_text() for w in children(window) if isinstance(w, Gtk.Label)]
        wait(lambda: window.settings_loaded and len(window.cards.get_children()) == 2)
        self.assertTrue(any('TEST upstream unavailable' in text for text in labels()))
        self.assertEqual(len([w for w in children(window) if isinstance(w, Gtk.ProgressBar)]), 2)
        window.refresh_button.clicked()
        wait(lambda: any(method == 'POST' and path == '/api/refresh' for method, path, _ in calls) and not window.inflight)
        window.proxy_enabled.set_active(True); window.proxy_entry.set_text('socks5h://127.0.0.1:1080')
        window.save_button.clicked()
        wait(lambda: settings['proxy_enabled'] and not window.inflight)
        self.assertEqual(settings['proxy'], 'socks5h://127.0.0.1:1080')
        with patch.object(Gio.AppInfo, 'launch_default_for_uri', return_value=True):
            # A second real process activates the primary instance and requests login.
            secondary = subprocess.Popen(['/usr/bin/python3', str(launcher), '--login'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            wait(lambda: secondary.poll() is not None and window.login_id == 'test-session' and not window.inflight)
            out, err = secondary.communicate(timeout=2)
            self.assertEqual(secondary.returncode, 0, err.decode())
            self.assertEqual(len(app.get_windows()), 1)
            self.assertTrue(any(isinstance(w, Gtk.Entry) and w.get_text() == 'TEST-CODE' for w in children(window)))
            confirm = next(w for w in children(window) if isinstance(w, Gtk.Button) and w.get_label() == 'I confirmed login')
            confirm.clicked()
            wait(lambda: any('TEST transient login failure' in text for text in labels()) and not window.inflight)
            self.assertEqual(window.login_id, 'test-session')
            retry = next(w for w in children(window) if isinstance(w, Gtk.Button) and w.get_label() == 'Retry confirmation')
            retry.clicked()
            wait(lambda: window.login_id is None and not window.inflight)
        self.assertEqual(sum(path.startswith('/api/login/complete') for _, path, _ in calls), 2)
        screenshot = os.environ.get('CBL_GUI_SCREENSHOT')
        if screenshot:
            root = Gdk.get_default_root_window()
            image = Gdk.pixbuf_get_from_window(root, 0, 0, root.get_width(), root.get_height())
            image.savev(screenshot, 'png', [], [])
        window.destroy()
        self.assertFalse(window.alive)
        self.assertIsNone(window.refresh_source)
        print('GTK acceptance: 2 accounts, error/progress bars, refresh, settings save, single-instance --login, retry, teardown verified')


if __name__ == '__main__': unittest.main()
