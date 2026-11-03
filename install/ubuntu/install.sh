#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
usage() {
  printf '%s\n' 'Usage: install.sh [--systemd] [--extension|--indicator|--all]' 'Default: GNOME panel popup + background service when gnome-extensions is available; otherwise tray fallback.'
}
want_extension=0
want_indicator=0
explicit_ui=0
for arg in "$@"; do
  case "$arg" in
    --systemd) ;;
    --extension) want_extension=1; explicit_ui=1 ;;
    --indicator) want_indicator=1; explicit_ui=1 ;;
    --all) want_extension=1; want_indicator=1; explicit_ui=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; usage >&2; exit 1 ;;
  esac
done
if [[ "$explicit_ui" -eq 0 ]]; then
  if command -v gnome-extensions >/dev/null 2>&1; then want_extension=1; else want_indicator=1; fi
fi
BIN_DIR="$HOME/.local/bin"
SYSTEMD_DIR="$HOME/.config/systemd/user"
AUTOSTART_DIR="$HOME/.config/autostart"
EXT_UUID="cbl@codex-limits"
EXT_DIR="$HOME/.local/share/gnome-shell/extensions/$EXT_UUID"
mkdir -p "$BIN_DIR" "$SYSTEMD_DIR" "$AUTOSTART_DIR"
echo '[1/3] Installing CBL backend and panel helper'
# Stop tray before replacing its binary (ETXTBSY on a running executable).
pkill -xu "$(id -u)" cbl-tray >/dev/null 2>&1 || true
if [[ -x "$ROOT_DIR/cbl" && -x "$ROOT_DIR/cbl-tray" ]]; then
  # Copy+rename also allows upgrading a binary used by the running service.
  for name in cbl cbl-tray; do
    staging="$(mktemp "$BIN_DIR/.${name}.XXXXXX")"
    if ! install -m 0755 "$ROOT_DIR/$name" "$staging" || ! mv -f "$staging" "$BIN_DIR/$name"; then
      rm -f "$staging"; exit 1
    fi
  done
else
  (cd "$ROOT_DIR" && go build -o "$BIN_DIR/cbl" ./cmd/cbl && go build -o "$BIN_DIR/cbl-tray" ./cmd/cbl-tray)
fi
echo '[2/3] Updating background user service (accounts and settings preserved)'
install -m 0644 "$ROOT_DIR/install/systemd/cbl.service" "$SYSTEMD_DIR/cbl.service"
rm -f "$SYSTEMD_DIR/cbl.service.d/proxy.conf"
systemctl --user daemon-reload
systemctl --user enable cbl.service
systemctl --user restart cbl.service
systemctl --user is-active --quiet cbl.service
# Remove v0.3.0 standalone launchers, not credentials or configuration.
rm -f "$BIN_DIR/cbl-app" "$BIN_DIR/cbl_app.py" "$HOME/.local/share/applications/cbl.desktop"
if [[ "$want_extension" -eq 1 ]]; then
  echo '[3/3] Restoring GNOME top-bar popup'
  if command -v gnome-extensions >/dev/null 2>&1; then
    gnome-extensions disable "$EXT_UUID" >/dev/null 2>&1 || true
    gnome-extensions disable cbl@hermes >/dev/null 2>&1 || true
  fi
  rm -rf "$EXT_DIR" "$HOME/.local/share/gnome-shell/extensions/cbl@hermes"
  mkdir -p "$EXT_DIR"
  cp -a "$ROOT_DIR/desktop/gnome-extension/$EXT_UUID/." "$EXT_DIR/"
  if command -v gnome-extensions >/dev/null 2>&1 && gnome-extensions enable "$EXT_UUID"; then
    echo "GNOME extension enabled: $EXT_UUID"
  else
    echo "Popup installed. Log out and log back in, then run: gnome-extensions enable $EXT_UUID"
  fi
  echo 'On GNOME Wayland, log out and log back in to reload restored extension code.'
  if [[ "$want_indicator" -eq 0 ]]; then rm -f "$AUTOSTART_DIR/cbl-indicator.desktop"; fi
fi
if [[ "$want_indicator" -eq 1 ]]; then
  echo '[3/3] Installing optional tray fallback'
  python3 - "$ROOT_DIR" "$HOME" <<'PY'
from pathlib import Path
import sys
root, home = map(Path, sys.argv[1:])
path = str(home / '.local/bin/cbl-tray')
path = path.replace('\\', '\\\\').replace('"', '\\"').replace('`', '\\`').replace('$', '\\$').replace('%', '%%')
lines = (root / 'desktop/indicator/cbl-indicator.desktop').read_text().splitlines()
(home / '.config/autostart/cbl-indicator.desktop').write_text('\n'.join('Exec="' + path + '"' if line.startswith('Exec=') else line for line in lines) + '\n')
PY
  if [[ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]]; then
    (setsid "$BIN_DIR/cbl-tray" >/dev/null 2>&1 &)
  else
    echo 'Tray indicator will start on next desktop login.'
  fi
fi
echo 'Done. CBL runs from the panel; no standalone application window.'
