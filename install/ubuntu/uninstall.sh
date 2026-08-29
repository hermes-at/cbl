#!/usr/bin/env bash
set -euo pipefail

SYSTEMD_DIR="${HOME}/.config/systemd/user"
EXT_DIR="${HOME}/.local/share/gnome-shell/extensions/cbl@hermes"
TRAY_DIR="${HOME}/.local/share/cbl"
AUTOSTART_FILE="${HOME}/.config/autostart/cbl-indicator.desktop"
CONFIG_DIR="${HOME}/.config/cbl"

remove_tree() {
  local path="$1"
  [[ -e "$path" ]] || return 0
  chmod -R u+rwX "$path" 2>/dev/null || true
  if ! rm -rf "$path" 2>/dev/null; then
    echo "Could not remove $path" >&2
    echo "If it was created by an older sudo install, run: sudo rm -rf '$path'" >&2
    return 1
  fi
}

systemctl --user disable --now cbl.service 2>/dev/null || true
pkill -xu "$(id -u)" cbl-tray 2>/dev/null || true
rm -f "${SYSTEMD_DIR}/cbl.service"
rm -rf "${SYSTEMD_DIR}/cbl.service.d"
remove_tree "${EXT_DIR}"
remove_tree "${TRAY_DIR}"
rm -f "${AUTOSTART_FILE}"
rm -f "${HOME}/.local/bin/cbl"
rm -f "${HOME}/.local/bin/cbl-tray"
remove_tree "${CONFIG_DIR}"
systemctl --user daemon-reload 2>/dev/null || true

echo "Removed cbl user install artifacts."
