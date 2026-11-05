// Only fixed, actionable messages reach the popup, never upstream bodies or URLs.
export function loginErrorPresentation(error) {
    const message = String(typeof error === 'string' ? error : (error?.message || error?.code || '')).slice(0, 4096);
    if (/unsupported_country_region_territory/i.test(message))
        return {title: 'Вход недоступен', detail: 'OpenAI ограничивает доступ из текущего региона. Включите прокси в настройках CBL и повторите вход.'};
    if (/timeout|timed out|deadline exceeded/i.test(message))
        return {title: 'Сервер не ответил', detail: 'Проверьте сеть и прокси в настройках CBL, затем повторите попытку.'};
    if (/connect|network|resolve|dns|proxy|tls|socket|unreachable/i.test(message))
        return {title: 'Нет соединения с OpenAI', detail: 'Проверьте сеть и прокси в настройках CBL, затем повторите попытку.'};
    if (/429|rate.limit|too many requests/i.test(message))
        return {title: 'Слишком много запросов', detail: 'Подождите немного и повторите попытку входа.'};
    return {title: 'Не удалось войти', detail: 'Повторите попытку. Если ошибка сохраняется, проверьте подключение и прокси в настройках CBL.'};
}

export function accountErrorMessage(error) {
    if (!error) return '';
    const code = typeof error === 'string' ? error : (error.code || error.error || error.message || '');
    if (/\binvalid_(jwt|refresh_token)\b/.test(String(code)))
        return 'Session expired — add this account again';
    return 'Account unavailable';
}

// Structured states avoid exposing raw HTTP responses or token errors in the UI.
export function accountPresentation(account, {unavailable = false} = {}) {
    if (unavailable)
        return {label: 'Сервис недоступен', tone: 'error'};

    const status = account?.status;
    if (status === 'auth_required' || (!status && /invalid_(jwt|refresh_token)/.test(String(account?.error))))
        return {label: 'Нужен вход', tone: 'warning', detail: 'Добавьте этот аккаунт заново через Add Account…'};
    if (status === 'connection_error')
        return {label: 'Ошибка соединения', tone: 'error', detail: 'Проверьте сеть и настройки прокси. Повторите обновление.'};
    if (status === 'rate_limited')
        return {label: 'Слишком много запросов', tone: 'warning', detail: 'Подождите и повторите обновление.'};
    if (account?.error || (status && status !== 'ready'))
        return {label: 'Ошибка получения данных', tone: 'error', detail: 'Повторите обновление позже.'};
    const fetchedAt = Date.parse(account?.fetched_at);
    if (!Number.isFinite(fetchedAt))
        return {label: 'Нет данных', tone: 'warning'};
    return {label: '', tone: 'ready'};
}

// No request is emitted until a separate, explicit confirmation click.
export class AccountRemoval {
    constructor() {
        this.pending = null;
        this.busy = false;
    }

    request(key, label) {
        if (this.busy || !/^[a-f0-9]{64}$/.test(key || ''))
            return false;
        this.pending = {key, label};
        return true;
    }

    cancel() {
        if (this.busy)
            return false;
        this.pending = null;
        return true;
    }

    confirm() {
        if (!this.pending || this.busy)
            return null;
        this.busy = true;
        return this.pending.key;
    }

    finish(success) {
        this.busy = false;
        if (success)
            this.pending = null;
    }
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
