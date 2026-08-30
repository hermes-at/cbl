import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';
import Soup from 'gi://Soup';
import Clutter from 'gi://Clutter';
import ExtensionUtils from 'resource:///org/gnome/shell/extensions/extension.js';
import {PanelMenu} from 'resource:///org/gnome/shell/ui/panelMenu.js';
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

class ProgressCard extends St.BoxLayout {
    constructor(title) {
        super({vertical: true, style_class: 'cbl-card'});
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
}

class CblIndicator extends PanelMenu.Button {
    _init() {
        super._init(0.0, 'cbl');
        this._session = Soup.Session.new();
        this._extension = ExtensionUtils.getCurrentExtension();
        this._loginID = '';

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

        this._sessionCard = new ProgressCard('Лимит использования 5 часов');
        this._weeklyCard = new ProgressCard('Недельный лимит использования');
        this._creditsCard = new ProgressCard('Осталось кредитов');
        this._content.add_child(this._sessionCard);
        this._content.add_child(this._weeklyCard);
        this._content.add_child(this._creditsCard);

        this._loginBox = new St.BoxLayout({vertical: true, style_class: 'cbl-login-box'});
        this._loginText = new St.Label({text: 'Вход прямо из top bar: Add Account → подтвердить в браузере → I confirmed login.', style_class: 'cbl-card-meta'});
        this._loginText.clutter_text.line_wrap = true;
        this._loginBox.add_child(this._loginText);
        this._content.add_child(this._loginBox);

        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        this._addAccountItem = new PopupMenu.PopupMenuItem('🔑 Add Account…');
        this._addAccountItem.connect('activate', () => this._startLogin());
        this.menu.addMenuItem(this._addAccountItem);

        this._codeItem = new PopupMenu.PopupMenuItem('Code: —');
        this._codeItem.setSensitive(false);
        this.menu.addMenuItem(this._codeItem);

        this._completeLoginItem = new PopupMenu.PopupMenuItem('✓ I confirmed login');
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
        const plan = payload.plan || 'unknown';
        const account = shortAccount(payload.account_id);
        this._label.text = payload.windows?.[0]?.remaining ? `${payload.windows[0].remaining}%` : 'cbl';
        this._icon.gicon = this._stateIcon(payload.class || 'good');
        this._heading.text = 'Codex';
        this._subheading.text = `plan: ${plan} · account: ${account}`;
        this._badge.text = (payload.class || 'good').toUpperCase();
        this._sessionCard.setData(payload.windows?.[0]);
        this._weeklyCard.setData(payload.windows?.[1]);
        const creditsText = payload.credits?.text || '—';
        this._creditsCard._value.text = creditsText;
        this._creditsCard._fill.set_width(Math.max(3, Math.round(260 * (100 - clamp(payload.credits?.used, 0, 100)) / 100)));
        this._creditsCard._meta.text = 'monthly credits';
        this._statusItem.label.text = 'Status: OK';
    }

    _applyError(err) {
        const message = err?.message ?? String(err);
        this._label.text = '!';
        this._icon.gicon = this._stateIcon('error');
        this._subheading.text = 'service unavailable';
        this._badge.text = 'ERROR';
        this._sessionCard.setEmpty();
        this._weeklyCard.setEmpty();
        this._creditsCard.setEmpty();
        this._statusItem.label.text = `Status: ${message}`;
    }

    _startLogin() {
        this._codeItem.label.text = 'Requesting login code…';
        this._getJSON(LOGIN_START_URL, 'POST', (payload, err) => {
            if (err || !payload?.ok) {
                this._codeItem.label.text = `Login failed: ${err?.message || payload?.error || 'unknown error'}`;
                this._completeLoginItem.setSensitive(false);
                return;
            }
            this._loginID = payload.id;
            this._codeItem.label.text = `Code: ${payload.user_code}`;
            this._loginText.text = `Открыл страницу OpenAI. Введи код: ${payload.user_code}. Потом нажми I confirmed login.`;
            this._completeLoginItem.setSensitive(true);
            openURL(payload.verification_url);
        });
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
            this._codeItem.label.text = 'Login saved';
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
}

let indicator = null;

export default class CblExtension {
    enable() {
        indicator = new CblIndicator();
        Main.panel.addToStatusArea('cbl', indicator, 0, 'right');
    }

    disable() {
        if (indicator) {
            indicator.destroy();
            indicator = null;
        }
    }
}
