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
import {accountPresentation, AccountRemoval, overallUsageState, loginErrorPresentation, resetCreditPresentation} from './ui-data.js';

const STATUS_URL = 'http://127.0.0.1:18088/api/status';
const REFRESH_URL = 'http://127.0.0.1:18088/api/refresh';
const LOGIN_START_URL = 'http://127.0.0.1:18088/api/login/start';
const LOGIN_COMPLETE_URL = 'http://127.0.0.1:18088/api/login/complete';
const ACCOUNTS_URL = 'http://127.0.0.1:18088/api/accounts';
const RESET_USE_URL = 'http://127.0.0.1:18088/api/reset-credits/use';
const REFRESH_SECONDS = 60;
const MANUAL_REFRESH_COOLDOWN_MS = 5000;

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
        this._value.text = `${remaining}% left`;
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
        this._meta.text = 'no data';
    }
});

function meterColor(remaining, light = false) {
    if (light) {
        if (remaining <= 10)
            return [0.706, 0.137, 0.184, 1.0];
        if (remaining <= 30)
            return [0.502, 0.329, 0, 1.0];
        return [0.078, 0.478, 0.239, 1.0];
    }
    if (remaining <= 10)
        return [1.0, 0.36, 0.36, 1.0];
    if (remaining <= 30)
        return [0.96, 0.77, 0.26, 1.0];
    return [0.18, 0.83, 0.44, 1.0];
}

function colorLuminance(color) {
    const channel = value => {
        const normalized = value / 255;
        return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
    };
    return 0.2126 * channel(color.red) + 0.7152 * channel(color.green) + 0.0722 * channel(color.blue);
}

function roundedRect(cr, x, y, width, height, radius) {
    const r = Math.min(radius, width / 2, height / 2);
    cr.newSubPath();
    cr.arc(x + width - r, y + r, r, -Math.PI / 2, 0);
    cr.arc(x + width - r, y + height - r, r, 0, Math.PI / 2);
    cr.arc(x + r, y + height - r, r, Math.PI / 2, Math.PI);
    cr.arc(x + r, y + r, r, Math.PI, 3 * Math.PI / 2);
    cr.closePath();
}

const MiniMeter = GObject.registerClass(
class MiniMeter extends St.DrawingArea {
    _init(remaining) {
        super._init({style_class: 'cbl-mini-meter'});
        this._remaining = clamp(remaining, 0, 100);
        this._light = false;
        this.set_size(160, 8);
        this.connect('repaint', () => this._repaint());
    }

    setLightTheme(light) {
        this._light = light;
        this.queue_repaint();
    }

    _repaint() {
        const cr = this.get_context();
        try {
            let [width, height] = this.get_surface_size();
            width = width || 160;
            height = height || 8;
            roundedRect(cr, 0, 0, width, height, height / 2);
            cr.setSourceRGBA(...(this._light ? [0.82, 0.83, 0.85, 1.0] : [0.23, 0.23, 0.25, 1.0]));
            cr.fill();

            const fillWidth = this._remaining <= 0
                ? 2
                : Math.max(2, Math.round(width * this._remaining / 100));
            roundedRect(cr, 0, 0, fillWidth, height, height / 2);
            cr.setSourceRGBA(...meterColor(this._remaining, this._light));
            cr.fill();
        } finally {
            cr.$dispose();
        }
    }
});

const AccountCard = GObject.registerClass(
class AccountCard extends St.BoxLayout {
    _init(account, onRemove, presentation = {}, onReset = () => {}) {
        super._init({vertical: true, style_class: 'cbl-account-card'});
        this._meters = [];

        const header = new St.BoxLayout({vertical: false, style_class: 'cbl-account-header'});
        const title = new St.Label({
            text: accountTitle(account),
            style_class: 'cbl-account-title',
            x_expand: true,
        });
        const statusRow = new St.BoxLayout({vertical: false, style_class: 'cbl-plan-row', y_align: Clutter.ActorAlign.CENTER});
        const status = new St.Label({
            text: account?.plan || 'unknown',
            style_class: 'cbl-plan-pill',
            y_align: Clutter.ActorAlign.CENTER,
        });
        statusRow.add_child(status);
        const resetView = resetCreditPresentation(account?.reset_credits);

        header.add_child(title);
        header.add_child(statusRow);
        if (/^[a-f0-9]{64}$/.test(account?.account_key || '')) {
            const remove = new St.Button({
                label: '×', style_class: 'cbl-account-remove',
                accessible_name: `Удалить ${accountTitle(account)} из CBL`,
                reactive: true, can_focus: true, track_hover: true,
            });
            remove.connect('clicked', () => onRemove(account));
            header.add_child(remove);
        }
        this.add_child(header);

        this._state = new St.Label({text: '', style_class: 'cbl-account-state'});
        this._detail = new St.Label({text: '', style_class: 'cbl-card-meta'});
        this._detail.clutter_text.line_wrap = true;
        this.add_child(this._state);
        this.add_child(this._detail);
        this._account = account;
        this.setPresentation(presentation);

        const windows = account?.windows || [];
        if (account?.error || (account?.status && account.status !== 'ready')) {
            return;
        }
        this._addMeter('5h', windows[0]);
        this._addMeter('week', windows[1]);
        const credits = account?.credits?.text ?? '—';
        const creditsUsed = clamp(account?.credits?.used, 0, 100);
        this._addMeter('credits', {remaining: credits === '0.00' ? 0 : 100 - creditsUsed, label: credits});
        if (resetView.visible) {
            const details = new St.BoxLayout({vertical: true, style_class: 'cbl-reset-details'});
            details.visible = false;
            const count = Number.isInteger(account?.reset_credits?.available_count)
                ? account.reset_credits.available_count : resetView.offers.length;
            const note = resetView.offers.length ? new St.Button({
                label: '▾', style_class: 'cbl-reset-toggle',
                can_focus: true, reactive: true, track_hover: true,
                accessible_name: 'Show reset offers',
            }) : new St.Label({text: resetView.label, style_class: 'cbl-reset-note'});
            if (resetView.offers.length) {
                note.connect('clicked', () => {
                    details.visible = !details.visible;
                    note.label = details.visible ? '▴' : '▾';
                    note.accessible_name = details.visible ? 'Hide reset offers' : 'Show reset offers';
                });
                for (const offer of resetView.offers) {
                    const scope = offer.scope === 'full' ? 'Full · week + 5h' : 'Reset limits';
                    const expiry = offer.expiry === null ? 'Expiry unknown' : `Use by ${new Date(offer.expiry).toLocaleString('en-US', {day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', timeZoneName: 'short'})}`;
                    const row = new St.BoxLayout({vertical: false, style_class: 'cbl-reset-offer'});
                    const info = new St.Label({text: `${scope}\n${expiry}`, style_class: 'cbl-reset-note', x_expand: true});
                    info.clutter_text.line_wrap = true;
                    row.add_child(info);
                    const use = new St.Button({label: 'Apply', style_class: 'cbl-reset-use', can_focus: true, reactive: true, track_hover: true, y_align: Clutter.ActorAlign.CENTER, accessible_name: 'Apply reset credit'});
                    use.connect('clicked', () => onReset(account, offer));
                    row.add_child(use);
                    details.add_child(row);
                }
            } else {
                note.clutter_text.line_wrap = true;
            }
            if (resetView.offers.length) {
                const summary = new St.BoxLayout({vertical: false, style_class: 'cbl-reset-summary'});
                const label = new St.Label({text: `Resets: ${count}`, style_class: 'cbl-reset-note', x_expand: true, y_align: Clutter.ActorAlign.CENTER});
                summary.add_child(label);
                summary.add_child(note);
                this.add_child(summary);
                this.add_child(new St.Label({text: resetView.label, style_class: 'cbl-reset-note'}));
            } else {
                this.add_child(note);
            }
            this.add_child(details);
        }
    }

    setPresentation(options) {
        const state = accountPresentation(this._account, options);
        this._state.text = state.label;
        this._state.visible = Boolean(state.label);
        this._state.style_class = `cbl-account-state cbl-state-${state.tone}`;
        this._detail.text = state.detail || '';
        this._detail.visible = Boolean(state.detail);
    }

    _addMeter(name, data) {
        const remaining = clamp(data?.remaining, 0, 100);
        const meter = new St.BoxLayout({vertical: true, style_class: 'cbl-meter'});
        const row = new St.BoxLayout({vertical: false, style_class: 'cbl-meter-row'});
        const label = new St.Label({text: name, style_class: 'cbl-meter-label'});
        const track = new MiniMeter(remaining);
        this._meters.push(track);
        const value = new St.Label({
            text: data?.label ? String(data.label) : `${remaining}%`,
            style_class: 'cbl-meter-value',
        });
        row.add_child(label);
        row.add_child(track);
        row.add_child(value);
        meter.add_child(row);
        if (data?.reset) {
            const reset = new St.Label({
                text: `reset ${data.reset}`,
                style_class: 'cbl-meter-reset',
            });
            meter.add_child(reset);
        }
        this.add_child(meter);
    }
});

const CblIndicator = GObject.registerClass(
class CblIndicator extends PanelMenu.Button {
    _init(extension) {
        super._init(0.0, 'cbl');
        this._session = Soup.Session.new();
        this._session.timeout = 30;
        this._extension = extension;
        this._loginID = '';
        this._loginActive = false;
        this._latestUserCode = '';
        this._refreshing = false;
        this._refreshTimeoutId = 0;
        this._lastManualRefreshAt = 0;
        this._manualCooldownId = 0;
        this._manualCooldownUntil = 0;
        this._nextRefreshSeconds = REFRESH_SECONDS;
        this._destroyed = false;
        this._requestGeneration = 0;
        this._applyTimeoutId = 0;
        this._removal = new AccountRemoval();

        this._cards = [];

        this._icon = new St.Icon({
            gicon: this._loadIcon('assets/cbl-symbolic.svg'),
            style_class: 'system-status-icon',
        });
        this._label = new St.Label({text: '•••', y_align: Clutter.ActorAlign.CENTER});
        this._box = new St.BoxLayout({style_class: 'panel-status-menu-box'});
        this._box.add_child(this._label);
        this.add_child(this._box);

        this._root = new PopupMenu.PopupBaseMenuItem({reactive: true, can_focus: false});
        this._root.add_style_class_name('cbl-popup-item');
        this._content = new St.BoxLayout({vertical: true, style_class: 'cbl-popup'});
        this._root.add_child(this._content);
        this.menu.addMenuItem(this._root);
        this._themeChangedId = this.menu.box.connect('style-changed', () => this._syncTheme());
        this._syncTheme();

        const header = new St.BoxLayout({vertical: false, style_class: 'cbl-header'});
        const titleBox = new St.BoxLayout({vertical: true, x_expand: true});
        this._heading = new St.Label({text: 'Codex', style_class: 'cbl-heading'});
        this._subheading = new St.Label({text: 'loading…', style_class: 'cbl-subheading'});
        titleBox.add_child(this._heading);
        titleBox.add_child(this._subheading);
        this._headerRefresh = new St.Button({
            label: '↻',
            style_class: 'cbl-header-refresh',
            reactive: true,
            can_focus: true,
            track_hover: true,
            x_align: Clutter.ActorAlign.CENTER,
            y_align: Clutter.ActorAlign.CENTER,
        });
        this._headerRefresh.connect('clicked', () => {
            this._manualRefresh();
        });
        this._badge = new St.Label({text: 'CBL', style_class: 'cbl-badge'});
        header.add_child(titleBox);
        header.add_child(this._headerRefresh);
        header.add_child(this._badge);
        this._content.add_child(header);

        this._accountsBox = new St.BoxLayout({vertical: true, style_class: 'cbl-accounts-box'});
        this._content.add_child(this._accountsBox);

        this._removeBox = new St.BoxLayout({vertical: true, style_class: 'cbl-remove-confirm'});
        this._removeText = new St.Label({text: '', style_class: 'cbl-card-meta'});
        this._removeText.clutter_text.line_wrap = true;
        this._removeBox.add_child(this._removeText);
        const removeActions = new St.BoxLayout({style_class: 'cbl-remove-actions'});
        this._removeCancel = new St.Button({label: 'Отмена', style_class: 'cbl-confirm-button', can_focus: true});
        this._removeConfirm = new St.Button({label: 'Удалить', style_class: 'cbl-confirm-button cbl-danger', can_focus: true});
        this._removeCancel.connect('clicked', () => {
            if (this._removal.cancel())
                this._removeBox.hide();
        });
        this._removeConfirm.connect('clicked', () => this._confirmRemove());
        removeActions.add_child(this._removeCancel);
        removeActions.add_child(this._removeConfirm);
        this._removeBox.add_child(removeActions);
        this._content.add_child(this._removeBox);
        this._removeBox.hide();

        this._loginBox = new St.BoxLayout({vertical: true, style_class: 'cbl-login-box'});
        this._loginTitle = new St.Label({text: '', style_class: 'cbl-login-title cbl-state-error'});
        this._loginBox.add_child(this._loginTitle);
        this._loginTitle.hide();
        this._loginText = new St.Label({text: 'To add an account: click “Add Account…”, confirm in the browser, then click “I confirmed login”.', style_class: 'cbl-card-meta'});
        this._loginText.clutter_text.line_wrap = true;
        this._loginBox.add_child(this._loginText);
        this._content.add_child(this._loginBox);

        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        this._addAccountItem = new PopupMenu.PopupMenuItem('🔑 Add Account…');
        this._addAccountItem.connect('activate', () => this._startLogin());
        this.menu.addMenuItem(this._addAccountItem);

        this._codeItem = new PopupMenu.PopupMenuItem('Code: —');
        this._codeItem.setSensitive(false);
        this._codeItem.connect('activate', () => this._copyLoginCode());
        this.menu.addMenuItem(this._codeItem);
        this._codeItem.hide();

        this._completeLoginItem = new PopupMenu.PopupMenuItem('✓ I confirmed login');
        this._completeLoginItem.setSensitive(false);
        this._completeLoginItem.connect('activate', () => this._completeLogin());
        this.menu.addMenuItem(this._completeLoginItem);
        this._completeLoginItem.hide();

        this._cancelLoginItem = new PopupMenu.PopupMenuItem('Cancel');
        this._cancelLoginItem.connect('activate', () => this._cancelLogin());
        this.menu.addMenuItem(this._cancelLoginItem);
        this._cancelLoginItem.hide();

        this._statusItem = new PopupMenu.PopupMenuItem('Status: local service');
        this._statusItem.setSensitive(false);
        this.menu.addMenuItem(this._statusItem);

        this._timerId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, REFRESH_SECONDS, () => {
            this.refresh();
            return GLib.SOURCE_CONTINUE;
        });
        this._countdownId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 1, () => {
            if (!this._refreshing && this._nextRefreshSeconds > 0) {
                this._nextRefreshSeconds -= 1;
                this._setIdleStatus();
            }
            return GLib.SOURCE_CONTINUE;
        });
        this.refresh();
    }

    destroy() {
        this._destroyed = true;
        if (this._themeChangedId) {
            this.menu.box.disconnect(this._themeChangedId);
            this._themeChangedId = 0;
        }
        this._requestGeneration++;
        this._session.abort();
        if (this._applyTimeoutId) {
            GLib.Source.remove(this._applyTimeoutId);
            this._applyTimeoutId = 0;
        }
        if (this._timerId) {
            GLib.Source.remove(this._timerId);
            this._timerId = 0;
        }
        if (this._countdownId) {
            GLib.Source.remove(this._countdownId);
            this._countdownId = 0;
        }
        if (this._refreshTimeoutId) {
            GLib.Source.remove(this._refreshTimeoutId);
            this._refreshTimeoutId = 0;
        }
        if (this._manualCooldownId) {
            GLib.Source.remove(this._manualCooldownId);
            this._manualCooldownId = 0;
        }
        super.destroy();
    }

    _syncTheme() {
        if (!this._root)
            return;
        // Sample the native menu surface, not the transparent menu item or our overrides.
        const color = this.menu.box.get_theme_node().get_background_color();
        const light = colorLuminance(color) > 0.5;
        this._lightTheme = light;
        if (light)
            this._root.add_style_class_name('cbl-light-theme');
        else
            this._root.remove_style_class_name('cbl-light-theme');
        for (const card of this._cards) {
            for (const meter of card._meters || [])
                meter.setLightTheme(light);
        }
    }

    _loadIcon(relPath) {
        const iconPath = this._extension.dir.get_child(relPath).get_path();
        return Gio.FileIcon.new(Gio.File.new_for_path(iconPath));
    }

    _getJSON(url, method, callback, body = null) {
        try {
            const message = Soup.Message.new(method, url);
            if (body !== null)
                message.set_request_body_from_bytes('application/json', new GLib.Bytes(new TextEncoder().encode(JSON.stringify(body))));
            this._session.send_and_read_async(message, GLib.PRIORITY_DEFAULT, null, (session, res) => {
                if (this._destroyed)
                    return;
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

    _requestReset(account, offer) {
        if (this._resetBusy || !/^[a-f0-9]{64}$/.test(account?.account_key || '')) return;
        if (!this._resetBox) {
            this._resetBox = new St.BoxLayout({vertical: true, style_class: 'cbl-remove-confirm'});
            this._resetText = new St.Label({text: '', style_class: 'cbl-card-meta'});
            this._resetText.clutter_text.line_wrap = true;
            this._resetBox.add_child(this._resetText);
            const actions = new St.BoxLayout({style_class: 'cbl-remove-actions'});
            this._resetCancel = new St.Button({label: 'Cancel', style_class: 'cbl-confirm-button', can_focus: true});
            this._resetConfirm = new St.Button({label: 'Apply', style_class: 'cbl-confirm-button cbl-reset-use', can_focus: true});
            this._resetCancel.connect('clicked', () => {
                if (!this._resetBusy) { this._pendingReset = null; this._resetBox.hide(); }
            });
            this._resetConfirm.connect('clicked', () => this._confirmReset());
            actions.add_child(this._resetCancel);
            actions.add_child(this._resetConfirm);
            this._resetBox.add_child(actions);
            this._content.add_child(this._resetBox);
        }
        this._pendingReset = {accountKey: account.account_key, offerKey: offer.offer_key};
        const expiry = offer.expiry ? new Date(offer.expiry).toLocaleString('en-GB', {timeZoneName: 'short'}) : 'unknown';
        this._resetText.text = `Use reset for ${accountTitle(account)}?\n${offer.scope === 'full' ? 'Full · week + 5h' : 'Reset limits'}\nExpires: ${expiry}\nThis spends one reset credit.`;
        this._resetConfirm.reactive = true;
        this._resetCancel.reactive = true;
        this._resetBox.show();
        this._resetCancel.grab_key_focus();
    }

    _confirmReset() {
        if (this._resetBusy || !this._pendingReset) return;
        const pending = this._pendingReset;
        const account = (this._latestResetAccounts || []).find(a => a.account_key === pending.accountKey && a.status === 'ready');
        const offer = resetCreditPresentation(account?.reset_credits).offers.find(o => o.offer_key === pending.offerKey);
        this._pendingReset = null;
        if (!account || !offer || offer.expiry === null || offer.scope === 'unknown') {
            this._resetText.text = 'Reset unavailable. Refresh the account and try again.';
            this._resetConfirm.reactive = false;
            return;
        }
        this._resetBusy = true;
        this._resetConfirm.reactive = false;
        this._resetCancel.reactive = false;
        this._getJSON(RESET_USE_URL, 'POST', (payload, err) => {
            this._resetBusy = false;
            this._resetCancel.reactive = true;
            if (err || !payload?.ok) {
                this._resetText.text = payload?.error_code === 'reset_unavailable'
                    ? 'Reset unavailable in this build.' : 'Outcome unknown. Check current usage before trying again.';
                this.refresh();
                return;
            }
            this._resetBox.hide();
            this._applyStatus(payload);
        }, {account_key: pending.accountKey, offer_key: pending.offerKey});
    }

    _manualRefresh() {
        const now = Date.now();
        const waitMs = this._manualCooldownUntil - now;
        if (waitMs > 0) {
            if (this._statusItem)
                this._statusItem.label.text = `Status: wait ${Math.ceil(waitMs / 1000)}s before refresh`;
            return;
        }
        this._lastManualRefreshAt = now;
        this._manualCooldownUntil = now + MANUAL_REFRESH_COOLDOWN_MS;
        if (this._manualCooldownId)
            GLib.Source.remove(this._manualCooldownId);
        if (this._headerRefresh)
            this._headerRefresh.label = '⟳';
        this._manualCooldownId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, MANUAL_REFRESH_COOLDOWN_MS, () => {
            this._manualCooldownId = 0;
            if (!this._refreshing && this._headerRefresh)
                this._headerRefresh.label = '↻';
            this._setIdleStatus();
            return GLib.SOURCE_REMOVE;
        });
        this.refresh(true);
    }

    refresh(showProgress = false) {
        if (this._destroyed || this._removal.busy)
            return;
        if (this._refreshing) {
            if (this._statusItem)
                this._statusItem.label.text = 'Status: refreshing…';
            return;
        }
        this._refreshing = true;
        const generation = this._requestGeneration;
        if (showProgress)
            this._setCardPresentation({updating: true});
        const startedAt = Date.now();
        const minVisibleMs = showProgress ? 1500 : 0;
        let finished = false;
        if (this._headerRefresh) {
            this._headerRefresh.label = '⟳';
        }
        if (this._statusItem)
            this._statusItem.label.text = 'Status: refreshing…';

        const finish = (payload, err) => {
            if (finished || this._destroyed || generation !== this._requestGeneration)
                return;
            finished = true;
            if (this._refreshTimeoutId) {
                GLib.Source.remove(this._refreshTimeoutId);
                this._refreshTimeoutId = 0;
            }
            const apply = () => {
                this._applyTimeoutId = 0;
                if (this._destroyed || generation !== this._requestGeneration)
                    return;
                this._refreshing = false;
                this._nextRefreshSeconds = REFRESH_SECONDS;
                if (this._headerRefresh) {
                    this._headerRefresh.label = Date.now() < this._manualCooldownUntil ? '⟳' : '↻';
                }
                if (err) {
                    this._applyError(err);
                    return;
                }
                if (!payload.ok) {
                    this._applyError(new Error(payload.error || 'CBL unavailable'));
                    return;
                }
                this._applyStatus(payload);
            };
            const waitMs = Math.max(0, minVisibleMs - (Date.now() - startedAt));
            if (waitMs > 0) {
                this._applyTimeoutId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, waitMs, () => {
                    apply();
                    return GLib.SOURCE_REMOVE;
                });
            } else {
                apply();
            }
        };

        this._refreshTimeoutId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 10, () => {
            finish(null, new Error('refresh timed out'));
            return GLib.SOURCE_REMOVE;
        });

        this._getJSON(showProgress ? REFRESH_URL : STATUS_URL, showProgress ? 'POST' : 'GET', (payload, err) => {
            finish(payload, err);
        });
    }

    _setIdleStatus() {
        if (!this._statusItem || this._refreshing || this._removal.busy || this._serviceUnavailable)
            return;
        const base = this._hasAccountErrors ? 'account error' : this._hasAccounts ? 'OK' : 'waiting for first account';
        this._statusItem.label.text = `Status: ${base} · next refresh ${this._nextRefreshSeconds}s`;
    }

    _applyStatus(payload) {
        this._serviceUnavailable = false;
        const accounts = payload.accounts || [];
        this._backendRefreshing = Boolean(payload.refreshing);
        this._hasAccounts = accounts.length > 0;
        this._hasAccountErrors = accounts.some(account => account?.error);
        if (!accounts.length) {
            this._label.text = '•••';
            this._icon.gicon = this._stateIcon('good');
            this._heading.text = 'Codex';
            this._subheading.text = 'limits';
            this._badge.hide();
            this._loginBox.show();
            this._setIdleStatus();
            this._showNoAccounts();
            return;
        }
        const stateClass = overallUsageState(accounts);
        this._label.text = '•••';
        this._icon.gicon = this._stateIcon(stateClass);
        this._heading.text = 'Codex';
        this._subheading.text = `${accounts.length} ${accounts.length === 1 ? 'profile' : 'profiles'}`;
        this._badge.hide();
        this._loginBox.visible = this._loginActive;
        this._setIdleStatus();
        this._applyAccounts(accounts);
    }

    _showNoAccounts() {
        this._cards = [];
        this._accountsBox.destroy_all_children();
        const box = new St.BoxLayout({vertical: true, style_class: 'cbl-empty-card'});
        const title = new St.Label({text: 'Accounts: 0', style_class: 'cbl-empty-title'});
        box.add_child(title);
        this._accountsBox.add_child(box);
    }

    _applyAccounts(accounts) {
        this._latestResetAccounts = accounts || [];
        this._cards = [];
        this._accountsBox.destroy_all_children();
        if (!accounts || !accounts.length)
            return;
        for (const account of accounts) {
            const card = new AccountCard(account, value => this._requestRemove(value), {updating: this._backendRefreshing}, (value, offer) => this._requestReset(value, offer));
            this._cards.push(card);
            for (const meter of card._meters || [])
                meter.setLightTheme(this._lightTheme);
            this._accountsBox.add_child(card);
        }
    }

    _setCardPresentation(options) {
        for (const card of this._cards)
            card.setPresentation(options);
    }

    _requestRemove(account) {
        if (!this._removal.request(account.account_key, accountTitle(account)))
            return;
        this._removeText.text = `Удалить «${accountTitle(account)}» из CBL?\nЛокальная запись CBL будет удалена. Аккаунт OpenAI и вход в Codex CLI останутся без изменений.`;
        this._removeConfirm.reactive = true;
        this._removeCancel.reactive = true;
        this._removeConfirm.label = 'Удалить';
        this._removeBox.show();
        this._removeCancel.grab_key_focus();
    }

    _confirmRemove() {
        const key = this._removal.confirm();
        if (!key)
            return;
        // Invalidate older responses so they cannot restore the removed card.
        this._requestGeneration++;
        for (const field of ['_refreshTimeoutId', '_applyTimeoutId']) {
            if (this[field]) {
                GLib.Source.remove(this[field]);
                this[field] = 0;
            }
        }
        this._refreshing = false;
        this._removeConfirm.reactive = false;
        this._removeCancel.reactive = false;
        this._removeConfirm.label = 'Удаляется…';
        this._statusItem.label.text = 'Status: удаление аккаунта…';
        this._getJSON(`${ACCOUNTS_URL}?key=${encodeURIComponent(key)}`, 'DELETE', (payload, err) => {
            const success = !err && payload?.ok;
            this._removal.finish(success);
            this._headerRefresh.label = '↻';
            this._removeConfirm.reactive = true;
            this._removeCancel.reactive = true;
            this._removeConfirm.label = 'Удалить';
            if (!success) {
                this._removeText.text = 'Не удалось удалить аккаунт. Проверьте, что сервис CBL обновлён и доступен, затем повторите попытку.';
                this._setCardPresentation({});
                this._statusItem.label.text = 'Status: ошибка удаления';
                return;
            }
            this._removeBox.hide();
            this._applyStatus(payload);
        });
    }

    _applyError(err) {
        this._serviceUnavailable = true;
        const message = 'Service unavailable';
        this._label.text = '•••';
        this._icon.gicon = this._stateIcon('error');
        this._subheading.text = 'service unavailable';
        this._badge.text = 'ERROR';
        this._badge.show();
        this._setCardPresentation({unavailable: true});
        this._statusItem.label.text = `Status: ${message}`;
    }

    _showLoginError(error) {
        const presentation = loginErrorPresentation(error);
        this._loginTitle.text = presentation.title;
        this._loginTitle.show();
        this._loginText.text = presentation.detail;
        this._addAccountItem.label.text = '↻ Повторить вход';
        this._loginBox.show();
        this._codeItem.label.text = 'Code: —';
        this._codeItem.setSensitive(false);
        this._codeItem.hide();
        this._cancelLoginItem.show();
    }

    _startLogin() {
        this._loginActive = true;
        this._addAccountItem.label.text = '🔑 Add Account…';
        this._loginID = '';
        this._loginTitle.hide();
        this._loginText.text = 'Запрашиваем код входа…';
        this._latestUserCode = '';
        this._codeItem.setSensitive(false);
        this._codeItem.show();
        this._completeLoginItem.show();
        this._cancelLoginItem.show();
        this._loginBox.show();
        this._codeItem.label.text = 'Requesting code…';
        this._getJSON(LOGIN_START_URL, 'POST', (payload, err) => {
            if (err || !payload?.ok) {
                this._showLoginError(err || payload?.error);
                this._completeLoginItem.setSensitive(false);
                this._completeLoginItem.hide();
                return;
            }
            this._loginID = payload.id;
            this._latestUserCode = payload.user_code || '';
            this._codeItem.label.text = `Code: ${payload.user_code}`;
            this._codeItem.setSensitive(true);
            const copied = copyText(this._latestUserCode);
            this._loginText.text = copied
                ? `Opened OpenAI. Code ${payload.user_code} is copied. Paste it in the browser, then click “I confirmed login”.`
                : `Opened OpenAI. Click the Code row to copy ${payload.user_code}, paste it in the browser, then click “I confirmed login”.`;
            this._completeLoginItem.setSensitive(true);
            openURL(payload.verification_url);
        });
    }

    _copyLoginCode() {
        if (!this._latestUserCode)
            return;
        if (copyText(this._latestUserCode))
            this._codeItem.label.text = `Code copied: ${this._latestUserCode}`;
    }

    _cancelLogin() {
        this._loginActive = false;
        this._addAccountItem.label.text = '🔑 Add Account…';
        this._loginTitle.hide();
        this._loginID = '';
        this._latestUserCode = '';
        this._codeItem.label.text = 'Code: —';
        this._codeItem.setSensitive(false);
        this._codeItem.hide();
        this._completeLoginItem.setSensitive(false);
        this._completeLoginItem.hide();
        this._cancelLoginItem.hide();
        this._loginText.text = 'To add an account: click “Add Account…”, confirm in the browser, then click “I confirmed login”.';
        if (this._hasAccounts)
            this._loginBox.hide();
        else
            this._loginBox.show();
    }

    _completeLogin() {
        if (!this._loginID)
            return;
        this._loginTitle.hide();
        this._loginText.text = 'Проверяем подтверждение входа…';
        this._codeItem.label.text = 'Waiting for confirmation…';
        this._getJSON(`${LOGIN_COMPLETE_URL}?id=${encodeURIComponent(this._loginID)}`, 'POST', (payload, err) => {
            if (err || !payload?.ok) {
                this._showLoginError(err || payload?.error);
                return;
            }
            this._loginID = '';
            this._latestUserCode = '';
            this._codeItem.label.text = 'Code: —';
            this._codeItem.setSensitive(false);
            this._codeItem.hide();
            this._loginText.text = '';
            this._loginActive = false;
            this._addAccountItem.label.text = '🔑 Add Account…';
            this._loginTitle.hide();
            this._loginBox.hide();
            this._completeLoginItem.setSensitive(false);
            this._completeLoginItem.hide();
            this._cancelLoginItem.hide();
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
