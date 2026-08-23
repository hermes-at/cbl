#!/usr/bin/env bash
set -euo pipefail

SCRIPT_SOURCE="${BASH_SOURCE[0]-}"
if [[ -n "$SCRIPT_SOURCE" && -f "$SCRIPT_SOURCE" ]]; then
  ROOT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
  if [[ -x "$ROOT_DIR/install/ubuntu/uninstall.sh" ]]; then
    exec "$ROOT_DIR/install/ubuntu/uninstall.sh"
  fi
fi

SYSTEMD_DIR="${HOME}/.config/systemd/user"
EXT_DIR="${HOME}/.local/share/gnome-shell/extensions/cbl@hermes"
TRAY_DIR="${HOME}/.local/share/cbl"
AUTOSTART_FILE="${HOME}/.config/autostart/cbl-indicator.desktop"

if command -v gnome-extensions >/dev/null 2>&1; then
  gnome-extensions uninstall cbl@hermes >/dev/null 2>&1 || true
fi
systemctl --user disable --now cbl.service 2>/dev/null || true
rm -f "${SYSTEMD_DIR}/cbl.service"
rm -rf "${EXT_DIR}"
rm -rf "${TRAY_DIR}"
rm -f "${AUTOSTART_FILE}"
rm -f "${HOME}/.local/bin/cbl"
rm -f "${HOME}/.local/bin/cbl-tray"
systemctl --user daemon-reload 2>/dev/null || true

echo "Removed cbl user install artifacts."
