// Runs the actual extension handlers against small GNOME API doubles.
// This verifies event wiring/lifecycles, not live GNOME rendering.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {accountPresentation, AccountRemoval, overallUsageState} from './ui-data.js';
class Actor {
    constructor(...args) { this._init(...args); }
    _init(props = {}) { Object.assign(this, props); this.children = []; this.handlers = {}; this.clutter_text = {}; this.visible = true; }
    add_child(actor) { this.children.push(actor); }
    connect(name, callback) { this.handlers[name] = callback; }
    click() { if (this.reactive !== false) this.handlers.clicked?.(); }
    hide() { this.visible = false; }
    show() { this.visible = true; }
    grab_key_focus() { this.focused = true; }
    destroy_all_children() { this.children = []; }
    destroy() { this.destroyed = true; }
    set_size() {}
    set_width() {}
    add_style_class_name() {}
}
class MenuItem extends Actor {
    _init(text) { super._init(); this.label = {text}; }
    setSensitive(value) { this.reactive = value; }
}
class PanelButton extends Actor {
    _init() { super._init(); this.menu = {addMenuItem() {}}; }
}
const timers = new Map();
let nextTimer = 1;
const timer = (priority, duration, callback) => { const id = nextTimer++; timers.set(id, callback); return id; };
const requests = [];
const session = {send_and_read_async(message, priority, cancel, callback) { requests.push({message, callback}); },
    send_and_read_finish(result) { return {get_data: () => new TextEncoder().encode(JSON.stringify(result))}; },
    abort() { this.aborted = true; }};
const file = {get_child() { return file; }, get_path() { return '/extension/icon.svg'; }};
const context = vm.createContext({
    TextDecoder, Date, Math, console, accountPresentation, AccountRemoval, overallUsageState,
    Gio: {FileIcon: class { static new() { return {}; } }, ThemedIcon: class {}, File: {new_for_path() { return {}; }}},
    GLib: {PRIORITY_DEFAULT: 0, SOURCE_REMOVE: false, SOURCE_CONTINUE: true,
        timeout_add: timer, timeout_add_seconds: timer, Source: {remove(id) { timers.delete(id); }}},
    GObject: {registerClass(klass) { return klass; }},
    St: {BoxLayout: Actor, Widget: Actor, Label: Actor, Button: Actor, Icon: Actor, DrawingArea: Actor},
    Soup: {Session: {new() { return session; }}, Message: {new(method, url) { return {method, url}; }}},
    Clutter: {ActorAlign: {CENTER: 0}}, Extension: class {}, PanelMenu: {Button: PanelButton},
    Main: {}, PopupMenu: {PopupBaseMenuItem: Actor, PopupMenuItem: MenuItem, PopupSeparatorMenuItem: Actor},
});
let source = fs.readFileSync(new URL('./extension.js', import.meta.url), 'utf8');
source = source.replace(/^import .*;\n/gm, '').replace('export default class CblExtension', 'class CblExtension');
vm.runInContext(source + '\nglobalThis.Indicator = CblIndicator;', context);
const indicator = new context.Indicator({dir: file});
assert.equal(requests.length, 1, 'initial status request sent');
const firstRequest = requests.shift();
const a = {account_key: 'a'.repeat(64), account_label: 'First', status: 'ready', fetched_at: '2026-11-03T17:00:00Z', windows: []};
const b = {account_key: 'b'.repeat(64), account_label: 'Second', status: 'auth_required', error: 'invalid_jwt'};
firstRequest.callback(session, {ok: true, accounts: [a, b]});
assert.equal(indicator._cards.length, 2);
assert.equal(indicator._cards[0]._state.text, 'Данные актуальны');
assert.equal(indicator._cards[1]._state.text, 'Нужен вход');
const firstRemove = indicator._cards[0].children[0].children[2];
assert.match(firstRemove.accessible_name, /First/);
firstRemove.click();
assert.equal(requests.length, 0, 'first click does not delete');
assert.equal(indicator._removeBox.visible, true);
assert.equal(indicator._removeCancel.focused, true, 'safe default focus');
indicator._removeCancel.click();
assert.equal(indicator._removeBox.visible, false);
assert.equal(indicator._removal.pending, null);
assert.equal(requests.length, 0, 'cancel does not delete');
// A refresh response arriving after DELETE must not resurrect the old card.
indicator.refresh();
const stale = requests.shift();
firstRemove.click();
indicator._removeConfirm.click();
indicator._removeConfirm.click();
assert.equal(requests.length, 1, 'only one DELETE despite double click');
assert.equal(requests[0].message.method, 'DELETE');
assert.equal(new URL(requests[0].message.url).searchParams.get('key'), a.account_key);
indicator.refresh();
assert.equal(requests.length, 1, 'no refresh while removing');
const deletion = requests.shift();
deletion.callback(session, {ok: true, accounts: [b]});
assert.equal(indicator._cards.length, 1);
stale.callback(session, {ok: true, accounts: [a, b]});
assert.equal(indicator._cards.length, 1, 'late status cannot resurrect removed account');
assert.equal(indicator._removeBox.visible, false);
// Failure preserves the card and supports an explicit retry/cancel.
indicator._cards[0].children[0].children[2].click();
indicator._removeConfirm.click();
requests.shift().callback(session, {ok: false, error: 'internal details must not be shown'});
assert.equal(indicator._cards.length, 1);
assert.equal(indicator._removal.busy, false);
assert.equal(indicator._removeBox.visible, true);
assert.doesNotMatch(indicator._removeText.text, /internal details/);
indicator._removeCancel.click();
// Backend failure retains cards but never presents cached data as fresh.
indicator._applyError(new Error('offline'));
assert.equal(indicator._cards[0]._state.text, 'Сервис недоступен');
const errorStatus = indicator._statusItem.label.text;
indicator._setIdleStatus();
assert.equal(indicator._statusItem.label.text, errorStatus);
indicator._applyStatus({ok: true, accounts: [b], refreshing: true});
assert.equal(indicator._cards[0]._state.text, 'Обновляется…');
indicator._applyStatus({ok: true, accounts: [a]});
indicator.refresh(true);
assert.equal(indicator._cards[0]._state.text, 'Обновляется…');
const inflight = requests.shift();
indicator.destroy();
assert.equal(timers.size, 0, 'all sources removed on disable');
assert.equal(session.aborted, true);
inflight.callback(session, {ok: true, accounts: [b]});
assert.equal(indicator._cards[0]._account.account_key, a.account_key, 'no mutation after destroy');
console.log('Actual extension handlers: confirm/cancel, delete, retry, stale-refresh and disable verified (GNOME API doubles)');
