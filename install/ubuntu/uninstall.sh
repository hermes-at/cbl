#!/usr/bin/env bash
set -euo pipefail

SYSTEMD_DIR="${HOME}/.config/systemd/user"
EXT_DIR="${HOME}/.local/share/gnome-shell/extensions/cbl@hermes"
TRAY_DIR="${HOME}/.local/share/cbl"
AUTOSTART_FILE="${HOME}/.config/autostart/cbl-indicator.desktop"

systemctl --user disable --now cbl.service 2>/dev/null || true
rm -f "${SYSTEMD_DIR}/cbl.service"
rm -rf "${EXT_DIR}"
rm -rf "${TRAY_DIR}"
rm -f "${AUTOSTART_FILE}"
rm -f "${HOME}/.local/bin/cbl"
systemctl --user daemon-reload 2>/dev/null || true

echo "Removed cbl user install artifacts."
