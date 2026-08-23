import Adw from 'gi://Adw';
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Gtk from 'gi://Gtk?version=4.0';

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

function defaultAuthPath() {
    return GLib.build_filenamev([GLib.get_home_dir(), '.codex', 'auth.json']);
}

export default class CblPrefs {
    fillPreferencesWindow(window) {
        window.set_title('cbl');
        window.set_default_size(560, 360);

        const config = loadConfig();
        const page = new Adw.PreferencesPage();
        const group = new Adw.PreferencesGroup({title: 'Codex profile'});

        const profileRow = new Adw.EntryRow({
            title: 'Profile name',
            text: config.profile_name || 'default',
        });
        const authRow = new Adw.EntryRow({
            title: 'Auth file path',
            text: config.auth_file || defaultAuthPath(),
        });
        const baseRow = new Adw.EntryRow({
            title: 'Base URL',
            text: config.base_url || 'https://chatgpt.com/backend-api',
        });

        group.add(profileRow);
        group.add(authRow);
        group.add(baseRow);

        const noteGroup = new Adw.PreferencesGroup({title: 'How it works'});
        const noteRow = new Adw.ActionRow({
            title: 'The panel shows weekly and 5h usage',
            subtitle: 'Save a Codex auth.json path here, then reload the panel after login if needed.',
        });
        noteRow.activatable = false;
        noteGroup.add(noteRow);

        page.add(group);
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
                        profile_name: profileRow.text.trim(),
                        auth_file: authRow.text.trim(),
                        base_url: baseRow.text.trim(),
                    });
                    noteRow.subtitle = `Saved to ${CONFIG_FILE}`;
                } catch (err) {
                    noteRow.subtitle = `Save failed: ${err.message ?? err}`;
                }
                return GLib.SOURCE_REMOVE;
            });
        };

        profileRow.connect('changed', scheduleSave);
        authRow.connect('changed', scheduleSave);
        baseRow.connect('changed', scheduleSave);

        scheduleSave();
    }
}
