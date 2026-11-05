// Runs the actual extension handlers against small GNOME API doubles.
// This verifies event wiring/lifecycles, not live GNOME rendering.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {accountPresentation, AccountRemoval, overallUsageState, loginErrorPresentation, resetCreditPresentation} from './ui-data.js';
class Actor {
    constructor(...args) { this._init(...args); }
    _init(props = {}) { Object.assign(this, props); this.children = []; this.handlers = {}; this.clutter_text = {}; this.visible = true; this.styleClasses = new Set(); }
    add_child(actor) { this.children.push(actor); }
    connect(name, callback) { this.handlers[name] = callback; return name; }
    disconnect(id) { this.disconnected = id; }
    click() { if (this.reactive !== false) this.handlers.clicked?.(); }
    hide() { this.visible = false; }
    show() { this.visible = true; }
    grab_key_focus() { this.focused = true; }
    destroy_all_children() { this.children = []; }
    destroy() { this.destroyed = true; }
    set_size() {}
    set_width() {}
    add_style_class_name(name) { this.styleClasses.add(name); }
    remove_style_class_name(name) { this.styleClasses.delete(name); }
    get_theme_node() { return {get_background_color: () => this.nativeMenuSurface ? themeBackground : {red: 0, green: 0, blue: 0, alpha: 0}}; }
    queue_repaint() { this.repaintQueued = true; }
    has_style_class_name(name) { return this.styleClasses.has(name); }
}
class MenuItem extends Actor {
    _init(text) { super._init(); this.label = {text}; }
    setSensitive(value) { this.reactive = value; }
}
class PanelButton extends Actor {
    _init() { super._init(); this.menu = {box: new Actor({nativeMenuSurface: true}), addMenuItem() {}}; }
}
const timers = new Map();
let nextTimer = 1;
const timer = (priority, duration, callback) => { const id = nextTimer++; timers.set(id, callback); return id; };
const requests = [];
const session = {send_and_read_async(message, priority, cancel, callback) { requests.push({message, callback}); },
    send_and_read_finish(result) { return {get_data: () => new TextEncoder().encode(JSON.stringify(result))}; },
    abort() { this.aborted = true; }};
const file = {get_child() { return file; }, get_path() { return '/extension/icon.svg'; }};
let themeBackground = {red: 32, green: 32, blue: 32, alpha: 255};

const context = vm.createContext({
    TextDecoder, TextEncoder, Date, Math, console, accountPresentation, AccountRemoval, overallUsageState, loginErrorPresentation, resetCreditPresentation,
    Gio: {FileIcon: class { static new() { return {}; } }, ThemedIcon: class {}, File: {new_for_path() { return {}; }}},
    GLib: {Bytes: class {constructor(bytes) {this.bytes = bytes;}}, PRIORITY_DEFAULT: 0, SOURCE_REMOVE: false, SOURCE_CONTINUE: true,
        timeout_add: timer, timeout_add_seconds: timer, Source: {remove(id) { timers.delete(id); }}},
    GObject: {registerClass(klass) { return klass; }},
    St: {BoxLayout: Actor, Widget: Actor, Label: Actor, Button: Actor, Icon: Actor, DrawingArea: Actor},
    Soup: {Session: {new() { return session; }}, Message: {new(method, url) { return {method, url, set_request_body_from_bytes(type, bytes) {this.body = JSON.parse(new TextDecoder().decode(bytes.bytes));}}; }}},
    Clutter: {ActorAlign: {CENTER: 0}}, Extension: class {}, PanelMenu: {Button: PanelButton},
    Main: {}, PopupMenu: {PopupBaseMenuItem: Actor, PopupMenuItem: MenuItem, PopupSeparatorMenuItem: Actor},
});
let source = fs.readFileSync(new URL('./extension.js', import.meta.url), 'utf8');
source = source.replace(/^import .*;\n/gm, '').replace('export default class CblExtension', 'class CblExtension');
vm.runInContext(source + '\nglobalThis.Indicator = CblIndicator;', context);
const indicator = new context.Indicator({dir: file});
assert.equal(requests.length, 1, 'initial status request sent');
assert.equal(Object.hasOwn(indicator.menu.box.handlers, 'style-changed'), true, 'actual menu style changes observed');
assert.equal(indicator._root.styleClasses.has('cbl-light-theme'), false, 'dark popup keeps existing palette');
themeBackground = {red: 245, green: 245, blue: 245, alpha: 255};
indicator.menu.box.handlers['style-changed']();
assert.equal(indicator._root.styleClasses.has('cbl-light-theme'), true, 'light popup switches palette from popup background');
themeBackground = {red: 32, green: 32, blue: 32, alpha: 255};
indicator.menu.box.handlers['style-changed']();
assert.equal(indicator._root.styleClasses.has('cbl-light-theme'), false, 'dark switch restores existing palette');
const firstRequest = requests.shift();
const a = {account_key: 'a'.repeat(64), account_label: 'First', status: 'ready', fetched_at: '2026-11-03T17:00:00Z', windows: []};
const b = {account_key: 'b'.repeat(64), account_label: 'Second', status: 'auth_required', error: 'invalid_jwt'};
firstRequest.callback(session, {ok: true, accounts: [a, b]});
assert.equal(indicator._cards.length, 2);
assert.equal(indicator._cards[0]._state.text, '');
assert.equal(indicator._cards[0]._state.visible, false, 'healthy status consumes no space');
assert.equal(indicator._cards[0]._detail.visible, false, 'timestamp consumes no space');
assert.equal(indicator._cards[1]._state.text, 'Нужен вход');
assert.equal(indicator._cards[1]._state.visible, true);
assert.equal(indicator._cards[0]._meters.length, 3, 'real card retains its meters');
indicator._applyStatus({ok: true, accounts: [{...a, reset_credits: {available_count: 2, applicable_available_count: 0}}, b]});
const resetCard = indicator._cards[0];
assert.equal(resetCard.children[0].children[1].children.length, 1, 'only subscription is shown in header');
assert.equal(resetCard.children[0].children[1].y_align, 0, 'plan row centered vertically');
const resetNote = resetCard.children.find(child => child.style_class === 'cbl-reset-note');
assert.equal(resetNote.text, 'Resets: 2 · expiry unavailable');
assert.equal(resetNote.handlers.clicked, undefined, 'unverified action is not clickable');
assert.equal(requests.length, 0, 'rendering reset inventory never sends mutation requests');
indicator._applyStatus({ok: true, accounts: [{...a, reset_credits: {available_count: 2, offers: [{offer_key: 'c'.repeat(64), scope: 'full', expires_at: '2099-11-30T19:20:00Z'}]}}]});
const disclosure = indicator._cards[0].children.find(child => child.style_class === 'cbl-reset-summary').children[1];
const details = indicator._cards[0].children.find(child => child.style_class === 'cbl-reset-details');
assert.equal(disclosure.label, '▾');
assert.equal(disclosure.can_focus, true);
assert.equal(details.visible, false);
disclosure.click();
assert.equal(details.visible, true);
assert.equal(disclosure.label, '▴');
disclosure.click();
assert.equal(details.visible, false);
assert.equal(disclosure.label, '▾');
assert.equal(requests.length, 0, 'disclosure never consumes a reset');
const useReset = details.children[0].children[1];
assert.equal(useReset.label, 'Apply');
useReset.click();
assert.equal(requests.length, 0, 'first click only asks for confirmation');
assert.equal(indicator._resetCancel.focused, true);
indicator._resetCancel.click();
indicator._resetConfirm.click();
assert.equal(requests.length, 0, 'cancel prevents reset');
useReset.click();
indicator._resetConfirm.click();
indicator._confirmReset();
assert.equal(requests.length, 1, 'only one synthetic POST after explicit confirmation');
const syntheticUse = requests.shift();
assert.equal(syntheticUse.message.method, 'POST');
assert.equal(syntheticUse.message.url, 'http://127.0.0.1:18088/api/reset-credits/use');
assert.equal(syntheticUse.message.body.account_key, a.account_key);
assert.equal(syntheticUse.message.body.offer_key, 'c'.repeat(64));
syntheticUse.callback(session, {ok: true, accounts: [a, b]});
assert.equal(indicator._resetBox.visible, false);
// A disappeared account/offer cannot be confirmed from an old card.
useReset.click();
indicator._applyAccounts([a, b]);
indicator._resetConfirm.click();
assert.equal(requests.length, 0, 'stale offer cannot mutate');
indicator._resetCancel.click();
// A service failure must invalidate confirmation even when old cards remain.
const liveOffer = {offer_key: 'c'.repeat(64), scope: 'full', expires_at: '2099-11-30T19:20:00Z'};
const resetAccount = {...a, reset_credits: {available_count: 1, offers: [liveOffer]}};
indicator._applyStatus({ok: true, accounts: [resetAccount]});
indicator._requestReset(resetAccount, {...liveOffer, expiry: Date.parse(liveOffer.expires_at)});
indicator._applyError(new Error('offline'));
indicator._confirmReset();
assert.equal(requests.length, 0, 'offline service cannot confirm stale reset');
indicator._resetCancel.click();
indicator._applyStatus({ok: true, accounts: [a, b]});
assert.equal(indicator._cards[0].children.some(child => child.style_class === 'cbl-reset-note'), false, 'absent reset data takes no space');
themeBackground = {red: 245, green: 245, blue: 245, alpha: 255};
indicator.menu.box.handlers['style-changed']();
assert.ok(indicator._cards[0]._meters.every(meter => meter._light && meter.repaintQueued), 'existing canvas meters change palette');
indicator._applyStatus({ok: true, accounts: [a, b]});
assert.ok(indicator._cards[0]._meters.every(meter => meter._light), 'new canvas meters use current palette');
indicator.menu.box.handlers['style-changed']();
assert.equal(indicator._root.styleClasses.has('cbl-light-theme'), true, 'repeated notifications do not oscillate');
themeBackground = {red: 32, green: 32, blue: 32, alpha: 255};
indicator.menu.box.handlers['style-changed']();
assert.ok(indicator._cards[0]._meters.every(meter => !meter._light), 'existing meters restore dark palette');
indicator._setCardPresentation({unavailable: true});
assert.equal(indicator._cards[0]._state.visible, true, 'service error becomes visible on a healthy card');
indicator._setCardPresentation({});
assert.equal(indicator._cards[0]._state.visible, false, 'recovered card hides status again');
assert.equal(indicator._cards[0]._detail.visible, false);
const firstRemove = indicator._cards[0].children[0].children[2];
// Login failures must never expand the menu with raw upstream JSON or HTML.
indicator._startLogin();
requests.shift().callback(session, {ok: false, error: '403 Forbidden: {"code":"unsupported_country_region_territory"}' + 'SECRET'.repeat(2000)});
assert.equal(indicator._codeItem.visible, false, 'raw login error is not a menu row');
assert.equal(indicator._loginTitle.text, 'Вход недоступен');
assert.equal(indicator._loginTitle.visible, true);
assert.match(indicator._addAccountItem.label.text, /Повторить вход/);
assert.match(indicator._loginText.text, /прокси/);
assert.doesNotMatch(indicator._loginText.text, /SECRET|403|Forbidden|\{/);
assert.equal(indicator._completeLoginItem.visible, false, 'no confirmation action without a login session');
indicator._applyStatus({ok: true, accounts: [a, b]});
assert.equal(indicator._loginBox.visible, true, 'periodic status must not hide the login error');
indicator._cancelLogin();
assert.equal(indicator._loginTitle.visible, false, 'cancel clears error presentation');
assert.match(indicator._addAccountItem.label.text, /Add Account/);
indicator._startLogin();
assert.equal(indicator._loginTitle.visible, false, 'retry clears error title');
// Simulate the completion stage without launching a browser or real OAuth.
requests.shift();
indicator._loginID = 'lab-only';
indicator._completeLogin();
requests.shift().callback(session, {ok: false, error: 'connection refused ' + '<html>SECRET</html>'.repeat(1000)});
assert.equal(indicator._codeItem.visible, false);
assert.match(indicator._loginText.text, /сеть|прокси/);
assert.doesNotMatch(indicator._loginText.text, /SECRET|html|refused/);
indicator._completeLogin();
requests.shift().callback(session, {ok: true});
assert.equal(indicator._loginTitle.visible, false, 'successful completion clears prior error');
requests.shift().callback(session, {ok: true, accounts: [a, b]});
indicator._cancelLogin();
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
assert.equal(indicator._cards[0]._state.text, 'Нужен вход');
indicator._applyStatus({ok: true, accounts: [a]});
indicator.refresh(true);
assert.equal(indicator._cards[0]._state.visible, false, 'manual refresh keeps healthy card quiet');
assert.equal(indicator._cards[0]._detail.visible, false);
const inflight = requests.shift();
indicator.destroy();
assert.equal(indicator.menu.box.disconnected, 'style-changed', 'theme signal disconnected on destroy');
assert.equal(timers.size, 0, 'all sources removed on disable');
assert.equal(session.aborted, true);
inflight.callback(session, {ok: true, accounts: [b]});
assert.equal(indicator._cards[0]._account.account_key, a.account_key, 'no mutation after destroy');

function luminance(hex) {
    const channels = hex.match(/[0-9a-f]{2}/gi).map(value => parseInt(value, 16) / 255)
        .map(value => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4);
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}
function contrast(a, b) {
    const values = [luminance(a), luminance(b)].sort((x, y) => y - x);
    return (values[0] + 0.05) / (values[1] + 0.05);
}
function cssBlock(selector) {
    return [...css.matchAll(/([^{}]+)\{([^}]+)\}/g)]
        .filter(match => match[1].split(',').some(item => item.trim() === selector))
        .map(match => match[2]).join('\n');
}
const css = fs.readFileSync(new URL('./stylesheet.css', import.meta.url), 'utf8');
const light = css.match(/\.cbl-light-theme\s*\{([^}]+)\}/)?.[1];
assert.ok(light, 'stylesheet defines adaptive light palette');
const lightBackground = light.match(/background-color\s*:\s*(#[0-9a-fA-F]{6})/)?.[1];
const lightText = light.match(/color\s*:\s*(#[0-9a-fA-F]{6})/)?.[1];
assert.ok(lightBackground && lightText, 'light root provides foreground and background');
assert.ok(contrast(lightText, lightBackground) >= 4.5, 'light root text contrast >= 4.5');
for (const selector of ['.cbl-heading', '.cbl-account-title', '.cbl-card-value', '.cbl-card-meta']) {
    const selectorIndex = css.indexOf(`.cbl-light-theme ${selector}`);
    const rule = selectorIndex < 0 ? null : css.slice(selectorIndex).match(/^[^{]*\{([^}]+)\}/);
    const foreground = rule?.[1].match(/color\s*:\s*(#[0-9a-fA-F]{6})/)?.[1];
    assert.ok(foreground && contrast(foreground, lightBackground) >= 4.5, `${selector} light text contrast >= 4.5`);
}
for (const selector of ['.cbl-account-card', '.cbl-card', '.cbl-empty-card', '.cbl-login-box', '.cbl-remove-confirm']) {
    const rule = cssBlock(`.cbl-light-theme ${selector}`);
    const foreground = rule.match(/(?:^|[;\s])color\s*:\s*(#[0-9a-fA-F]{6})/)?.[1];
    const background = rule.match(/background-color\s*:\s*(#[0-9a-fA-F]{6})/)?.[1];
    assert.ok(foreground && background && contrast(foreground, background) >= 4.5, `${selector} actual light colors pass`);
}
for (const tone of ['ready', 'warning', 'error', 'updating']) {
    const foreground = cssBlock(`.cbl-light-theme .cbl-state-${tone}`).match(/color:\s*(#[0-9a-f]{6})/)[1];
    assert.ok(contrast(foreground, '#f1f2f4') >= 4.5, `${tone} text on real light card`);
}
const hover = cssBlock('.cbl-light-theme .cbl-account-remove:focus');
assert.ok(contrast(hover.match(/color:\s*(#[0-9a-f]{6})/)[1], hover.match(/background-color:\s*(#[0-9a-f]{6})/)[1]) >= 4.5, 'remove focus text readable');
for (const remaining of [0, 10, 25, 75, 100]) {
    const meter = indicator._cards[0]._meters[0];
    meter._remaining = remaining;
    meter.setLightTheme(true);
    const colors = [];
    meter.get_context = () => ({newSubPath() {}, arc() {}, closePath() {}, fill() {}, $dispose() {}, setSourceRGBA(...rgba) { colors.push(rgba); }});
    meter.get_surface_size = () => [160, 8];
    meter._repaint();
    const hex = rgba => '#' + rgba.slice(0, 3).map(value => Math.round(value * 255).toString(16).padStart(2, '0')).join('');
    assert.ok(contrast(hex(colors[0]), hex(colors[1])) >= 3, `actual canvas fill at ${remaining}% contrasts with track`);
}
console.log('Actual extension handlers: theme switch/disconnect, confirm/cancel, delete, retry, stale-refresh and disable verified (GNOME API doubles); light CSS contrast verified');
