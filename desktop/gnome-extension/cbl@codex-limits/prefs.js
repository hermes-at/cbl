import Adw from 'gi://Adw';
import GLib from 'gi://GLib';
import {ExtensionPreferences} from 'resource:///org/gnome/Shell/Extensions/js/extensions/prefs.js';

const CONFIG_DIR = GLib.build_filenamev([GLib.get_home_dir(), '.config', 'cbl']);
const CONFIG_FILE = GLib.build_filenamev([CONFIG_DIR, 'config.json']);

function loadConfig() {
    try {
        const [ok, data] = GLib.file_get_contents(CONFIG_FILE);
        if (!ok) {
            return {};
        }
        return JSON.parse(new TextDecoder().decode(data));
    } catch (err) {
        return {};
    }
}

function saveConfig(config) {
    GLib.mkdir_with_parents(CONFIG_DIR, 0o755);
    const data = `${JSON.stringify(config, null, 2)}\n`;
    GLib.file_set_contents(CONFIG_FILE, data);
}

export default class CblPrefs extends ExtensionPreferences {
    fillPreferencesWindow(window) {
        window.set_title('cbl');
        window.set_default_size(560, 260);

        const config = loadConfig();
        const page = new Adw.PreferencesPage();

        const proxyGroup = new Adw.PreferencesGroup({
            title: 'Connection',
            description: 'Choose whether CBL reaches OpenAI directly or through a proxy.',
        });
        const proxyEnabled = new Adw.SwitchRow({
            title: 'Use proxy',
            subtitle: 'Off means a direct connection and ignores HTTP(S)_PROXY.',
            active: config.proxy_enabled !== false,
        });
        const proxyRow = new Adw.EntryRow({
            title: 'Proxy URL',
            text: config.proxy || '',
        });
        proxyRow.set_sensitive(proxyEnabled.active);
        proxyGroup.add(proxyEnabled);
        proxyGroup.add(proxyRow);

        const noteGroup = new Adw.PreferencesGroup({title: 'How it works'});
        const noteRow = new Adw.ActionRow({
            title: 'The panel shows weekly and 5h usage',
            subtitle: 'The reset time appears below each 5h/week meter when OpenAI provides it.',
        });
        noteRow.activatable = false;
        noteGroup.add(noteRow);

        page.add(proxyGroup);
        page.add(noteGroup);
        window.add(page);

        let saveTimer = 0;
        const scheduleSave = () => {
            if (saveTimer) {
                GLib.Source.remove(saveTimer);
            }
            saveTimer = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 250, () => {
                saveTimer = 0;
                try {
                    saveConfig({
                        ...config,
                        proxy_enabled: proxyEnabled.active,
                        proxy: proxyRow.text.trim(),
                    });
                    noteRow.subtitle = `Saved to ${CONFIG_FILE}. Refresh the panel to apply it now.`;
                } catch (err) {
                    noteRow.subtitle = `Save failed: ${err.message ?? err}`;
                }
                return GLib.SOURCE_REMOVE;
            });
        };

        proxyEnabled.connect('notify::active', () => {
            proxyRow.set_sensitive(proxyEnabled.active);
            scheduleSave();
        });
        proxyRow.connect('changed', scheduleSave);
    }
}
