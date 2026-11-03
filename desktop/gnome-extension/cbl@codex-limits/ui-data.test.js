import assert from 'node:assert/strict';
import {accountErrorMessage, accountWorstRemaining, overallUsageState} from './ui-data.js';
assert.equal(accountErrorMessage('codex usage request failed: 401 Unauthorized: {"error":{"code":"invalid_jwt"}}; token refresh also failed: invalid_refresh_token'), 'Session expired — add this account again');
assert.equal(accountErrorMessage({code: 'invalid_jwt'}), 'Session expired — add this account again');
assert.equal(accountErrorMessage({code: 'invalid_refresh_token'}), 'Session expired — add this account again');
assert.equal(accountErrorMessage({message: '<html>' + 'x'.repeat(5000) + '</html>'}), 'Account unavailable');
assert.equal(accountWorstRemaining({error: 'invalid_jwt', windows: []}), null);
assert.equal(overallUsageState([{windows: [{remaining: 100}]}, {error: 'failed'}]), 'error');
assert.equal(overallUsageState([{windows: [{remaining: 95}]}]), 'good');
