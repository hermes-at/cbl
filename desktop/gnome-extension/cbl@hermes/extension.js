import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';
import Soup from 'gi://Soup';
import ExtensionUtils from 'resource:///org/gnome/shell/extensions/extension.js';
import {PanelMenu} from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

const URL = 'http://127.0.0.1:18088/waybar';
const REFRESH_SECONDS = 300;
const CONFIG_PATH = GLib.build_filenamev([GLib.get_home_dir(), '.config', 'cbl', 'config.json']);

function openPrefs() {
    try {
        GLib.spawn_command_line_async('gnome-extensions prefs cbl@hermes');
    } catch (err) {
        logError(err);
    }
}

function openPath(path) {
    try {
        const uri = GLib.filename_to_uri(path, null);
        Gio.AppInfo.launch_default_for_uri(uri, null);
    } catch (err) {
        logError(err);
    }
}

function summaryPercent(text) {
    const match = text.match(/(\d+)%/);
    return match ? `${match[1]}%` : 'cbl';
}

function configLabel() {
    return CONFIG_PATH;
}

class CblIndicator extends PanelMenu.Button {
    _init() {
        super._init(0.0, 'cbl');
        this._session = Soup.Session.new();
        this._extension = ExtensionUtils.getCurrentExtension();
        this._icon = new St.Icon({
            gicon: this._loadIcon('assets/cbl-symbolic.svg'),
            style_class: 'system-status-icon',
        });
        this._label = new St.Label({text: 'cbl'});
        this._box = new St.BoxLayout({style_class: 'panel-status-menu-box'});
        this._box.add_child(this._icon);
        this._box.add_child(this._label);
        this.add_child(this._box);

        this._summaryItem = new PopupMenu.PopupMenuItem('Loading Codex status…');
        this._summaryItem.label.clutter_text.ellipsize = 3;
        this.menu.addMenuItem(this._summaryItem);

        this._detailsItem = new PopupMenu.PopupMenuItem('');
        this._detailsItem.label.clutter_text.ellipsize = 3;
        this.menu.addMenuItem(this._detailsItem);

        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        this._profileItem = new PopupMenu.PopupMenuItem('Add / edit profile…');
        this._profileItem.connect('activate', () => openPrefs());
        this.menu.addMenuItem(this._profileItem);

        this._refreshItem = new PopupMenu.PopupMenuItem('Refresh now');
        this._refreshItem.connect('activate', () => this.refresh());
        this.menu.addMenuItem(this._refreshItem);

        this._configItem = new PopupMenu.PopupMenuItem(`Open config: ${configLabel()}`);
        this._configItem.connect('activate', () => openPath(GLib.get_home_dir() + '/.config/cbl'));
        this.menu.addMenuItem(this._configItem);

        this._authItem = new PopupMenu.PopupMenuItem('Open auth folder');
        this._authItem.connect('activate', () => openPath(GLib.get_home_dir() + '/.codex'));
        this.menu.addMenuItem(this._authItem);

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

    refresh() {
        try {
            const message = Soup.Message.new('GET', URL);
            this._session.send_and_read_async(message, GLib.PRIORITY_DEFAULT, null, (session, res) => {
                try {
                    const bytes = session.send_and_read_finish(res);
                    const text = new TextDecoder().decode(bytes.get_data());
                    const payload = JSON.parse(text);
                    this._applyPayload(payload);
                } catch (err) {
                    this._applyError(err);
                }
            });
        } catch (err) {
            this._applyError(err);
        }
    }

    _applyPayload(payload) {
        const summary = payload.text || 'cbl';
        const tooltip = payload.tooltip || summary;
        const stateClass = payload.class || 'good';
        this._label.text = summaryPercent(summary);
        this._icon.gicon = this._stateIcon(stateClass);
        this._summaryItem.label.text = summary;
        this._detailsItem.label.text = tooltip;
        this._summaryItem.reactive = false;
        this._detailsItem.reactive = false;
        this._profileItem.label.text = 'Add / edit profile…';
    }

    _applyError(err) {
        const text = `Codex error`;
        const message = err?.message ?? String(err);
        this._label.text = '!';
        this._icon.gicon = this._stateIcon('error');
        this._summaryItem.label.text = text;
        this._detailsItem.label.text = message;
        this._profileItem.label.text = 'Add / edit profile…';
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
