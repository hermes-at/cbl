import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import GObject from 'gi://GObject';
import St from 'gi://St';
import Soup from 'gi://Soup?version=3.0';
import Clutter from 'gi://Clutter';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

const STATUS_URL = 'http://127.0.0.1:18088/api/status';
const LOGIN_START_URL = 'http://127.0.0.1:18088/api/login/start';
const LOGIN_COMPLETE_URL = 'http://127.0.0.1:18088/api/login/complete';
const REFRESH_SECONDS = 300;

function clamp(value, min, max) {
    return Math.max(min, Math.min(max, Number(value) || 0));
}

function shortAccount(accountID) {
    if (!accountID)
        return '—';
    return accountID.length > 8 ? `${accountID.slice(0, 8)}…` : accountID;
}

function accountTitle(account) {
    const label = account?.account_label || account?.account_email || account?.account_name;
    if (label)
        return label.length > 28 ? `${label.slice(0, 27)}…` : label;
    return shortAccount(account?.account_id);
}

function openURL(url) {
    try {
        Gio.AppInfo.launch_default_for_uri(url, null);
    } catch (err) {
        logError(err);
    }
}

function decodeBytes(bytes) {
    return new TextDecoder().decode(bytes.get_data());
}

function copyText(text) {
    try {
        const clipboard = St.Clipboard.get_default();
        const type = St.ClipboardType ? (St.ClipboardType.CLIPBOARD ?? 1) : 1;
        clipboard.set_text(type, text);
        return true;
    } catch (err) {
        logError(err);
        return false;
    }
}

const ProgressCard = GObject.registerClass(
class ProgressCard extends St.BoxLayout {
    _init(title) {
        super._init({vertical: true, style_class: 'cbl-card'});
        this._title = new St.Label({text: title, style_class: 'cbl-card-title'});
        this._value = new St.Label({text: '—', style_class: 'cbl-card-value'});
        this._track = new St.Widget({style_class: 'cbl-progress-track', x_expand: true});
        this._fill = new St.Widget({style_class: 'cbl-progress-fill'});
        this._track.add_child(this._fill);
        this._meta = new St.Label({text: '—', style_class: 'cbl-card-meta'});
        this.add_child(this._title);
        this.add_child(this._value);
        this.add_child(this._track);
        this.add_child(this._meta);
    }

    setData(card) {
        const remaining = clamp(card?.remaining, 0, 100);
        const used = clamp(card?.used, 0, 100);
        this._value.text = `${remaining}% осталось`;
        this._fill.set_width(Math.max(3, Math.round(260 * remaining / 100)));
        this._fill.remove_style_pseudo_class('warning');
        this._fill.remove_style_pseudo_class('critical');
        if (remaining <= 10)
            this._fill.add_style_pseudo_class('critical');
        else if (remaining <= 30)
            this._fill.add_style_pseudo_class('warning');
        this._meta.text = card?.reset ? `${used}% used · reset ${card.reset}` : `${used}% used`;
    }

    setEmpty() {
        this._value.text = '—';
        this._fill.set_width(3);
        this._meta.text = 'нет данных';
    }
});

function accountWorstRemaining(account) {
    const windows = account?.windows || [];
    let worst = 100;
    for (const win of windows) {
        worst = Math.min(worst, clamp(win?.remaining, 0, 100));
    }
    if (!windows.length)
        return 0;
    return worst;
}

function styleFill(fill, remaining) {
    fill.remove_style_pseudo_class('warning');
    fill.remove_style_pseudo_class('critical');
    if (remaining <= 10)
        fill.add_style_pseudo_class('critical');
    else if (remaining <= 30)
        fill.add_style_pseudo_class('warning');
}

const AccountCard = GObject.registerClass(
class AccountCard extends St.BoxLayout {
    _init(account) {
        super._init({vertical: true, style_class: 'cbl-account-card'});

        const header = new St.BoxLayout({vertical: false, style_class: 'cbl-account-header'});
        const title = new St.Label({
            text: accountTitle(account),
            style_class: 'cbl-account-title',
            x_expand: true,
        });
        const status = new St.Label({
            text: account?.plan || 'unknown',
            style_class: 'cbl-plan-pill',
        });
        header.add_child(title);
        header.add_child(status);
        this.add_child(header);

        const windows = account?.windows || [];
        this._addMeter('5h', windows[0]);
        this._addMeter('week', windows[1]);
        const credits = account?.credits?.text ?? '—';
        const creditsUsed = clamp(account?.credits?.used, 0, 100);
        this._addMeter('credits', {remaining: credits === '0.00' ? 0 : 100 - creditsUsed, label: credits});
    }

    _addMeter(name, data) {
        const remaining = clamp(data?.remaining, 0, 100);
        const row = new St.BoxLayout({vertical: false, style_class: 'cbl-meter-row'});
        const label = new St.Label({text: name, style_class: 'cbl-meter-label'});
        const track = new St.Widget({style_class: 'cbl-mini-track'});
        const fill = new St.Widget({style_class: 'cbl-mini-fill', y_align: Clutter.ActorAlign.CENTER});
        fill.set_width(Math.max(2, Math.round(160 * remaining / 100)));
        styleFill(fill, remaining);
        track.add_child(fill);
        const value = new St.Label({
            text: data?.label ? String(data.label) : `${remaining}%`,
            style_class: 'cbl-meter-value',
        });
        row.add_child(label);
        row.add_child(track);
        row.add_child(value);
        this.add_child(row);
    }
});

const CblIndicator = GObject.registerClass(
class CblIndicator extends PanelMenu.Button {
    _init(extension) {
        super._init(0.0, 'cbl');
        this._session = Soup.Session.new();
        this._extension = extension;
        this._loginID = '';
        this._latestUserCode = '';

        this._icon = new St.Icon({
            gicon: this._loadIcon('assets/cbl-symbolic.svg'),
            style_class: 'system-status-icon',
        });
        this._label = new St.Label({text: 'cbl', y_align: Clutter.ActorAlign.CENTER});
        this._box = new St.BoxLayout({style_class: 'panel-status-menu-box'});
        this._box.add_child(this._icon);
        this._box.add_child(this._label);
        this.add_child(this._box);

        this._root = new PopupMenu.PopupBaseMenuItem({reactive: false, can_focus: false});
        this._root.add_style_class_name('cbl-popup-item');
        this._content = new St.BoxLayout({vertical: true, style_class: 'cbl-popup'});
        this._root.add_child(this._content);
        this.menu.addMenuItem(this._root);

        const header = new St.BoxLayout({vertical: false, style_class: 'cbl-header'});
        const titleBox = new St.BoxLayout({vertical: true, x_expand: true});
        this._heading = new St.Label({text: 'Codex', style_class: 'cbl-heading'});
        this._subheading = new St.Label({text: 'загрузка…', style_class: 'cbl-subheading'});
        titleBox.add_child(this._heading);
        titleBox.add_child(this._subheading);
        this._badge = new St.Label({text: 'CBL', style_class: 'cbl-badge'});
        header.add_child(titleBox);
        header.add_child(this._badge);
        this._content.add_child(header);

        this._accountsBox = new St.BoxLayout({vertical: true, style_class: 'cbl-accounts-box'});
        this._content.add_child(this._accountsBox);

        this._loginBox = new St.BoxLayout({vertical: true, style_class: 'cbl-login-box'});
        this._loginText = new St.Label({text: 'Чтобы добавить аккаунт: нажми «Добавить аккаунт…», подтверди вход в браузере, потом нажми «Я подтвердил вход».', style_class: 'cbl-card-meta'});
        this._loginText.clutter_text.line_wrap = true;
        this._loginBox.add_child(this._loginText);
        this._content.add_child(this._loginBox);

        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        this._addAccountItem = new PopupMenu.PopupMenuItem('🔑 Добавить аккаунт…');
        this._addAccountItem.connect('activate', () => this._startLogin());
        this.menu.addMenuItem(this._addAccountItem);

        this._codeItem = new PopupMenu.PopupMenuItem('Code: —');
        this._codeItem.setSensitive(false);
        this._codeItem.connect('activate', () => this._copyLoginCode());
        this.menu.addMenuItem(this._codeItem);

        this._copyCodeItem = new PopupMenu.PopupMenuItem('📋 Скопировать код');
        this._copyCodeItem.setSensitive(false);
        this._copyCodeItem.connect('activate', () => this._copyLoginCode());
        this.menu.addMenuItem(this._copyCodeItem);

        this._completeLoginItem = new PopupMenu.PopupMenuItem('✓ Я подтвердил вход');
        this._completeLoginItem.setSensitive(false);
        this._completeLoginItem.connect('activate', () => this._completeLogin());
        this.menu.addMenuItem(this._completeLoginItem);

        this._refreshItem = new PopupMenu.PopupMenuItem('↻ Refresh');
        this._refreshItem.connect('activate', () => this.refresh());
        this.menu.addMenuItem(this._refreshItem);

        this._statusItem = new PopupMenu.PopupMenuItem('Status: local service');
        this._statusItem.setSensitive(false);
        this.menu.addMenuItem(this._statusItem);

        this._timerId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, REFRESH_SECONDS, () => {
            this.refresh();
            return GLib.SOURCE_CONTINUE;
        });
        this.refresh();
    }

    destroy() {
        if (this._timerId) {
            GLib.Source.remove(this._timerId);
            this._timerId = 0;
        }
        super.destroy();
    }

    _loadIcon(relPath) {
        const iconPath = this._extension.dir.get_child(relPath).get_path();
        return Gio.FileIcon.new(Gio.File.new_for_path(iconPath));
    }

    _getJSON(url, method, callback) {
        try {
            const message = Soup.Message.new(method, url);
            this._session.send_and_read_async(message, GLib.PRIORITY_DEFAULT, null, (session, res) => {
                try {
                    const bytes = session.send_and_read_finish(res);
                    const text = decodeBytes(bytes);
                    const payload = JSON.parse(text);
                    callback(payload, null);
                } catch (err) {
                    callback(null, err);
                }
            });
        } catch (err) {
            callback(null, err);
        }
    }

    refresh() {
        this._getJSON(STATUS_URL, 'GET', (payload, err) => {
            if (err) {
                this._applyError(err);
                return;
            }
            if (!payload.ok) {
                this._applyError(new Error(payload.error || 'CBL unavailable'));
                return;
            }
            this._applyStatus(payload);
        });
    }

    _applyStatus(payload) {
        const accounts = payload.accounts || [];
        if (!accounts.length) {
            this._label.text = 'cbl';
            this._icon.gicon = this._stateIcon('good');
            this._heading.text = 'Codex';
            this._subheading.text = 'лимиты Codex';
            this._badge.hide();
            this._statusItem.label.text = 'Status: waiting for first account';
            this._showNoAccounts();
            return;
        }
        let worst = 100;
        for (const account of accounts)
            worst = Math.min(worst, accountWorstRemaining(account));
        const stateClass = worst <= 10 ? 'critical' : worst <= 30 ? 'warning' : 'good';
        this._label.text = `${worst}%`;
        this._icon.gicon = this._stateIcon(stateClass);
        this._heading.text = 'Codex';
        this._subheading.text = `${accounts.length} ${accounts.length === 1 ? 'профиль' : 'профиля'} · минимум ${worst}%`;
        if (stateClass === 'good') {
            this._badge.hide();
        } else {
            this._badge.text = stateClass === 'critical' ? 'МАЛО' : 'НИЗКО';
            this._badge.show();
        }
        this._statusItem.label.text = 'Status: OK';
        this._applyAccounts(accounts);
    }

    _showNoAccounts() {
        this._accountsBox.destroy_all_children();
        const box = new St.BoxLayout({vertical: true, style_class: 'cbl-empty-card'});
        const title = new St.Label({text: 'Аккаунтов: 0', style_class: 'cbl-empty-title'});
        const text = new St.Label({text: 'Нажми «Добавить аккаунт…» ниже, подтверди вход в браузере, потом нажми «Я подтвердил вход».', style_class: 'cbl-empty-text'});
        text.clutter_text.line_wrap = true;
        box.add_child(title);
        box.add_child(text);
        this._accountsBox.add_child(box);
    }

    _applyAccounts(accounts) {
        this._accountsBox.destroy_all_children();
        if (!accounts || !accounts.length)
            return;
        for (const account of accounts) {
            this._accountsBox.add_child(new AccountCard(account));
        }
    }

    _applyError(err) {
        const message = err?.message ?? String(err);
        this._label.text = '!';
        this._icon.gicon = this._stateIcon('error');
        this._subheading.text = 'service unavailable';
        this._badge.text = 'ERROR';
        this._badge.show();
        this._accountsBox.destroy_all_children();
        this._statusItem.label.text = `Status: ${message}`;
    }

    _startLogin() {
        this._latestUserCode = '';
        this._codeItem.setSensitive(false);
        this._copyCodeItem.setSensitive(false);
        this._codeItem.label.text = 'Запрашиваю код…';
        this._getJSON(LOGIN_START_URL, 'POST', (payload, err) => {
            if (err || !payload?.ok) {
                this._codeItem.label.text = `Login failed: ${err?.message || payload?.error || 'unknown error'}`;
                this._completeLoginItem.setSensitive(false);
                return;
            }
            this._loginID = payload.id;
            this._latestUserCode = payload.user_code || '';
            this._codeItem.label.text = `Код: ${payload.user_code}`;
            this._codeItem.setSensitive(true);
            this._copyCodeItem.setSensitive(true);
            const copied = copyText(this._latestUserCode);
            this._loginText.text = copied
                ? `Открыл страницу OpenAI. Код ${payload.user_code} уже скопирован в буфер. Вставь его в браузере, потом нажми «Я подтвердил вход».`
                : `Открыл страницу OpenAI. Нажми «Скопировать код», вставь код ${payload.user_code} в браузере, потом нажми «Я подтвердил вход».`;
            this._completeLoginItem.setSensitive(true);
            openURL(payload.verification_url);
        });
    }

    _copyLoginCode() {
        if (!this._latestUserCode)
            return;
        if (copyText(this._latestUserCode))
            this._copyCodeItem.label.text = '📋 Код скопирован';
        else
            this._copyCodeItem.label.text = '📋 Не удалось скопировать';
    }

    _completeLogin() {
        if (!this._loginID)
            return;
        this._codeItem.label.text = 'Waiting for confirmation…';
        this._getJSON(`${LOGIN_COMPLETE_URL}?id=${encodeURIComponent(this._loginID)}`, 'POST', (payload, err) => {
            if (err || !payload?.ok) {
                this._codeItem.label.text = `Login failed: ${err?.message || payload?.error || 'unknown error'}`;
                return;
            }
            this._loginID = '';
            this._latestUserCode = '';
            this._codeItem.label.text = 'Login saved';
            this._codeItem.setSensitive(false);
            this._copyCodeItem.label.text = '📋 Скопировать код';
            this._copyCodeItem.setSensitive(false);
            this._loginText.text = 'Аккаунт добавлен. CBL обновляет лимиты.';
            this._completeLoginItem.setSensitive(false);
            this.refresh();
        });
    }

    _stateIcon(stateClass) {
        if (stateClass === 'critical' || stateClass === 'warning' || stateClass === 'error') {
            const iconName = stateClass === 'critical' ? 'dialog-error-symbolic' : 'dialog-warning-symbolic';
            return new Gio.ThemedIcon({name: iconName});
        }
        const iconPath = this._extension.dir.get_child('assets').get_child('cbl-symbolic.svg').get_path();
        return new Gio.FileIcon({file: Gio.File.new_for_path(iconPath)});
    }
});

let indicator = null;

export default class CblExtension extends Extension {
    enable() {
        indicator = new CblIndicator(this);
        Main.panel.addToStatusArea('cbl', indicator, 0, 'right');
    }

    disable() {
        if (indicator) {
            indicator.destroy();
            indicator = null;
        }
    }
}
