export function accountErrorMessage(error) {
    if (!error) return '';
    const code = typeof error === 'string' ? error : (error.code || error.error || error.message || '');
    if (/\binvalid_(jwt|refresh_token)\b/.test(String(code)))
        return 'Session expired — add this account again';
    return 'Account unavailable';
}

export function accountWorstRemaining(account) {
    if (account?.error) return null;
    const windows = account?.windows || [];
    if (!windows.length) return 0;
    return Math.min(...windows.map(win => Math.max(0, Math.min(100, Number(win?.remaining) || 0))));
}

export function overallUsageState(accounts) {
    if (accounts.some(account => account?.error)) return 'error';
    const worst = Math.min(...accounts.map(accountWorstRemaining));
    return worst <= 10 ? 'critical' : worst <= 30 ? 'warning' : 'good';
}
