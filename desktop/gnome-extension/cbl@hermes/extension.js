import St from 'gi://St';
import Soup from 'gi://Soup';
import GLib from 'gi://GLib';
import GObject from 'gi://GObject';
import {PanelMenu} from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

const URL = 'http://127.0.0.1:18088/waybar';
const TIMEOUT_MS = 4000;

class CblIndicator extends PanelMenu.Button {
    _init() {
        super._init(0.0, 'cbl');
        this._session = Soup.Session.new();
        this._label = new St.Label({text: 'cbl'});
        this.add_child(this._label);

        this._statusItem = new PopupMenu.PopupMenuItem('cbl starting…');
        this.menu.addMenuItem(this._statusItem);
        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        this._refreshItem = new PopupMenu.PopupMenuItem('Refresh now');
        this._refreshItem.connect('activate', () => this.refresh());
        this.menu.addMenuItem(this._refreshItem);
        this._openItem = new PopupMenu.PopupMenuItem('Open local status');
        this._openItem.connect('activate', () => this._openStatus());
        this.menu.addMenuItem(this._openItem);

        this._timerId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 300, () => {
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
        const text = payload.text || 'cbl';
        this._label.text = text;
        this._statusItem.label.text = payload.tooltip || text;
    }

    _applyError(err) {
        this._label.text = 'cbl!';
        this._statusItem.label.text = `cbl unavailable: ${err.message ?? err}`;
    }

    _openStatus() {
        this.menu.close();
        this.refresh();
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
