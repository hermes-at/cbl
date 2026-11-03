#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
usage() {
  printf '%s\n' 'Usage: install.sh [--all|--systemd|--indicator]' 'Installs the standalone CBL window, background user service and panel indicator.' 'Legacy --extension is accepted as a migration alias; no Shell extension is installed.'
}
for arg in "$@"; do
  case "$arg" in
    --systemd|--indicator|--all) ;;
    --extension) echo 'Migrating from GNOME extension to standalone CBL.' ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; usage >&2; exit 1 ;;
  esac
done
# Use the system interpreter: virtualenv Python often cannot import PyGObject.
if ! /usr/bin/python3 -c "import gi; gi.require_version('Gtk', '3.0'); from gi.repository import Gtk" >/dev/null 2>&1; then
  echo 'Install desktop dependencies first: sudo apt install python3-gi gir1.2-gtk-3.0' >&2
  exit 1
fi
BIN_DIR="$HOME/.local/bin"
SYSTEMD_DIR="$HOME/.config/systemd/user"
APP_DIR="$HOME/.local/share/applications"
AUTOSTART_DIR="$HOME/.config/autostart"
mkdir -p "$BIN_DIR" "$SYSTEMD_DIR" "$APP_DIR" "$AUTOSTART_DIR"
echo '[1/3] Installing CBL binaries and window'
if [[ -x "$ROOT_DIR/cbl" && -x "$ROOT_DIR/cbl-tray" ]]; then
  install -m 0755 "$ROOT_DIR/cbl" "$BIN_DIR/cbl"
  install -m 0755 "$ROOT_DIR/cbl-tray" "$BIN_DIR/cbl-tray"
else
  (cd "$ROOT_DIR" && go build -o "$BIN_DIR/cbl" ./cmd/cbl && go build -o "$BIN_DIR/cbl-tray" ./cmd/cbl-tray)
fi
install -m 0755 "$ROOT_DIR/desktop/app/cbl-app" "$BIN_DIR/cbl-app"
# Additional Python modules, if supplied by the app, live beside its launcher.
if [[ -f "$ROOT_DIR/desktop/app/cbl_app.py" ]]; then
  install -m 0644 "$ROOT_DIR/desktop/app/cbl_app.py" "$BIN_DIR/cbl_app.py"
fi
/usr/bin/python3 - "$ROOT_DIR" "$HOME" <<'PY'
from pathlib import Path
import sys
root, home = map(Path, sys.argv[1:])
for source, target, executable in [
    ('desktop/app/cbl.desktop', '.local/share/applications/cbl.desktop', 'cbl-app'),
    ('desktop/indicator/cbl-indicator.desktop', '.config/autostart/cbl-indicator.desktop', 'cbl-tray'),
]:
    # Desktop Entry Exec quoting is not shell quoting.
    path = str(home / '.local/bin' / executable)
    path = path.replace('\\', '\\\\').replace('"', '\\"').replace('`', '\\`').replace('$', '\\$').replace('%', '%%')
    lines = (root / source).read_text().splitlines()
    (home / target).write_text('\n'.join('Exec="' + path + '"' if line.startswith('Exec=') else line for line in lines) + '\n')
PY
echo '[2/3] Installing background user service'
install -m 0644 "$ROOT_DIR/install/systemd/cbl.service" "$SYSTEMD_DIR/cbl.service"
rm -f "$SYSTEMD_DIR/cbl.service.d/proxy.conf"
if systemctl --user daemon-reload; then
  # Unlike enable --now, restart also picks up a replaced binary on upgrade.
  systemctl --user enable cbl.service
  systemctl --user restart cbl.service
  systemctl --user is-active --quiet cbl.service
else
  echo 'No systemd user bus. Files installed; after login run: systemctl --user enable --now cbl.service' >&2
  exit 1
fi
echo '[3/3] Removing obsolete Shell extension (accounts are preserved)'
for uuid in cbl@codex-limits cbl@hermes; do
  if command -v gnome-extensions >/dev/null 2>&1; then
    gnome-extensions disable "$uuid" >/dev/null 2>&1 || true
  fi
  rm -rf "$HOME/.local/share/gnome-shell/extensions/$uuid"
done
if [[ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]]; then
  pkill -xu "$(id -u)" cbl-tray >/dev/null 2>&1 || true
  (setsid "$BIN_DIR/cbl-tray" >/dev/null 2>&1 &)
else
  echo 'Panel indicator will start on next desktop login.'
fi
echo 'Done. Launch CBL from Applications, or run ~/.local/bin/cbl-app.'
echo 'On GNOME, enable Ubuntu AppIndicators / AppIndicator support to see the panel icon.'
