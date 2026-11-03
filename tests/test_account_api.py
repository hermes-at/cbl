#!/usr/bin/env python3
"""Offline black-box acceptance: real binary, isolated HOME, fake usage API.

Does not access user credentials, systemd, a live GNOME desktop or OpenAI.
"""
import hashlib
import http.server
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


class Usage(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        account = self.headers.get('ChatGPT-Account-Id')
        payload = {'account_id': account, 'plan_type': 'plus', 'rate_limit': {
            'primary_window': {'used_percent': 15, 'limit_window_seconds': 18000},
            'secondary_window': {'used_percent': 25, 'limit_window_seconds': 604800}}}
        code = 200
        if account == 'other':
            code, payload = 503, {'error': {'code': 'temporarily_unavailable'}}
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        pass


def credentials(identity):
    return json.dumps({'tokens': {'account_id': identity, 'access_token': 'offline-access',
                                'refresh_token': 'offline-refresh'}}).encode()


def main():
    with tempfile.TemporaryDirectory(prefix='cbl-account-api-', dir=os.environ.get('TMPDIR', '/root/.hermes/cache/scratch')) as directory:
        work = Path(directory)
        binary = work / 'cbl'
        subprocess.run(['go', 'build', '-o', str(binary), './cmd/cbl'], cwd=ROOT, check=True)
        home = work / 'home'
        accounts = home / '.config/cbl/accounts'
        accounts.mkdir(parents=True)
        external = home / '.codex/auth.json'
        external.parent.mkdir(parents=True)
        a = accounts / 'first.json'
        duplicate = home / '.config/cbl/auth.json'
        other = accounts / 'other.json'
        bad = accounts / 'broken.json'
        missing = accounts / 'missing.json'
        for path in (a, duplicate, external):
            path.write_bytes(credentials('first'))
        other.write_bytes(credentials('other'))
        bad.write_bytes(b'{broken')
        missing.write_bytes(b'{"tokens":{"account_id":"expired"}}')
        preserved = {path: path.read_bytes() for path in (other, external, missing)}
        (home / '.config/cbl/config.json').write_text('{"proxy_enabled":false}')
        upstream = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Usage)
        thread = threading.Thread(target=upstream.serve_forever, daemon=True)
        thread.start()
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 0))
            port = probe.getsockname()[1]
        address = f'http://127.0.0.1:{port}'
        env = {key: value for key, value in os.environ.items()
               if not key.startswith('CBL_') and key not in ('CODEX_HOME', 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'http_proxy', 'https_proxy', 'all_proxy')}
        env['HOME'] = str(home)
        command = [str(binary), 'serve', '--addr', f'127.0.0.1:{port}', '--interval', '1h',
                   '--base-url', f'http://127.0.0.1:{upstream.server_port}']
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

        def request(route, method='GET'):
            try:
                with opener.open(urllib.request.Request(address + route, method=method), timeout=5) as response:
                    return response.status, json.load(response)
            except urllib.error.HTTPError as error:
                return error.code, json.load(error)

        def start():
            process = subprocess.Popen(command, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                for _ in range(100):
                    if process.poll() is not None:
                        raise RuntimeError('isolated CBL service exited early')
                    try:
                        code, result = request('/api/status')
                        assert code == 200 and result['ok']
                        return process, result
                    except urllib.error.URLError:
                        time.sleep(0.05)
                raise RuntimeError('isolated CBL service did not start')
            except BaseException:
                process.terminate()
                process.wait(timeout=5)
                raise

        def stop(process):
            process.terminate()
            process.wait(timeout=5)

        process = None
        try:
            process, status = start()
            cards = status['accounts']
            assert len(cards) == 4, cards
            ready = next(card for card in cards if card.get('account_id') == 'first')
            assert ready['status'] == 'ready' and ready['fetched_at']
            assert next(card for card in cards if card.get('account_id') == 'other')['status'] == 'error'
            assert any(card['status'] == 'auth_required' for card in cards)
            assert all(len(card['account_key']) == 64 for card in cards)
            before = {path: path.read_bytes() for path in accounts.glob('*.json')}
            assert request('/api/accounts?key=../../.codex/auth.json', 'DELETE')[0] == 400
            assert request('/api/accounts?key=' + 'f' * 64, 'DELETE')[0] == 404
            assert request('/api/accounts?key=' + ready['account_key'])[0] == 405
            assert {path: path.read_bytes() for path in accounts.glob('*.json')} == before
            code, deleted = request('/api/accounts?key=' + ready['account_key'], 'DELETE')
            assert code == 200 and deleted['ok'], (code, deleted)
            assert len(deleted['accounts']) == 3
            assert not a.exists() and not duplicate.exists()
            for path, original in preserved.items():
                assert path.read_bytes() == original, path
            tombstone = home / '.config/cbl/removed-accounts.json'
            assert tombstone.is_file() and tombstone.stat().st_mode & 0o777 == 0o600
            # Even a forced refresh must not re-import external CLI credentials.
            code, refreshed = request('/api/refresh', 'POST')
            assert code == 200 and refreshed['ok']
            assert ready['account_key'] not in [card['account_key'] for card in refreshed['accounts']]
            stop(process)
            process = None
            process, restarted = start()
            assert ready['account_key'] not in [card['account_key'] for card in restarted['accounts']]
            # Invalid JSON credential files remain removable without trusting a path from the client.
            broken = next(card for card in restarted['accounts'] if card.get('account_id') == 'broken.json')
            code, deleted_bad = request('/api/accounts?key=' + broken['account_key'], 'DELETE')
            assert code == 200 and deleted_bad['ok']
            assert not bad.exists()
            for path, original in preserved.items():
                assert hashlib.sha256(path.read_bytes()).digest() == hashlib.sha256(original).digest(), path
            print('PASS: real binary/API states, DELETE-only mutation, invalid/unknown key rejection, targeted and bad-file deletion, external/other-byte preservation, no re-import after refresh and restart, private tombstones')
        finally:
            if process is not None:
                stop(process)
            upstream.shutdown()
            upstream.server_close()


if __name__ == '__main__':
    main()
