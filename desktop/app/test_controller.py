#!/usr/bin/python3
import importlib.util
import importlib.machinery
import pathlib
import unittest
import urllib.error
from unittest.mock import patch, Mock

APP = pathlib.Path(__file__).with_name('cbl-app')
spec = importlib.util.spec_from_file_location('cbl_app', str(APP), loader=importlib.machinery.SourceFileLoader('cbl_app', str(APP)))
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)

class ControllerTests(unittest.TestCase):
    def test_parses_all_accounts_and_windows(self):
        data = {'ok': True, 'accounts': [{'account_id':'a1','account_label':'one','plan':'pro','windows':[{'title':'5h','used':22,'remaining':78},{'title':'week','used':44,'remaining':56}],'credits':{'text':'9'}, 'error':''}, {'account_id':'a2','account_label':'two','plan':'plus','windows':[], 'credits':{'text':'—'}, 'error':'expired'}]}
        accounts, error = app.parse_status(data)
        self.assertIsNone(error); self.assertEqual(len(accounts), 2)
        self.assertEqual(accounts[0]['windows'][0]['remaining'], 78); self.assertEqual(accounts[1]['error'], 'expired')

    def test_top_level_error_is_reported(self):
        self.assertEqual(app.parse_status({'ok':False,'error':'offline'}), ([], 'offline'))

    def test_settings_payload_normalizes_fields(self):
        self.assertEqual(app.settings_payload({'proxy_enabled':True,'proxy':'http://localhost:1'}), {'proxy_enabled':True,'proxy':'http://localhost:1'})

    def test_action_routes_and_methods(self):
        expected={'refresh':('POST','/api/refresh'),'settings_get':('GET','/api/settings'),'settings_post':('POST','/api/settings'),'login_start':('POST','/api/login/start'),'login_complete':('POST','/api/login/complete')}
        for action, route in expected.items(): self.assertEqual(app.ACTIONS[action],route)

    def test_local_http_does_not_use_environment_proxy(self):
        with patch.object(app.urllib.request,'build_opener', wraps=app.urllib.request.build_opener) as build:
            opener=app.urllib.request.build_opener(app.urllib.request.ProxyHandler({}))
            self.assertTrue(opener)
            self.assertIsInstance(build.call_args.args[0], app.urllib.request.ProxyHandler)
            self.assertEqual(build.call_args.args[0].proxies,{})

    def test_structured_http_error_message(self):
        exc = urllib.error.HTTPError('http://localhost', 403, 'Forbidden', {}, None)
        exc.read = lambda: b'{"error":"account already active"}'
        self.assertEqual(app.error_message(exc), 'account already active')

    def test_unstructured_http_error_falls_back_to_status(self):
        exc = urllib.error.HTTPError('http://localhost', 403, 'Forbidden', {}, None)
        exc.read = lambda: b'not json'
        self.assertIn('403', app.error_message(exc))

if __name__ == '__main__': unittest.main()
