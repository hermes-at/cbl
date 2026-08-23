#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN_NAME="cbl"
INSTALL_BIN="${HOME}/.local/bin/${BIN_NAME}"
TRAY_BIN="${HOME}/.local/bin/cbl-tray"
SYSTEMD_DIR="${HOME}/.config/systemd/user"
EXT_DIR="${HOME}/.local/share/gnome-shell/extensions/cbl@hermes"
TRAY_DIR="${HOME}/.local/share/cbl"
AUTOSTART_DIR="${HOME}/.config/autostart"

usage() {
  cat <<'EOF'
Usage: install.sh [--systemd] [--extension] [--indicator] [--all]

Default: install all three integrations.
EOF
}

want_systemd=0
want_extension=0
want_indicator=0
if [[ $# -eq 0 ]]; then
  want_systemd=1
  want_extension=1
  want_indicator=1
else
  for arg in "$@"; do
    case "$arg" in
      --systemd) want_systemd=1 ;;
      --extension) want_extension=1 ;;
      --indicator) want_indicator=1 ;;
      --all) want_systemd=1; want_extension=1; want_indicator=1 ;;
      -h|--help) usage; exit 0 ;;
      *) echo "Unknown option: $arg" >&2; usage; exit 1 ;;
    esac
  done
fi

mkdir -p "${HOME}/.local/bin" "${SYSTEMD_DIR}" "${EXT_DIR}" "${TRAY_DIR}" "${AUTOSTART_DIR}"

echo "[1/4] Installing cbl binaries"
if [[ -x "$ROOT_DIR/cbl" && -x "$ROOT_DIR/cbl-tray" ]]; then
  install -m 0755 "$ROOT_DIR/cbl" "$INSTALL_BIN"
  install -m 0755 "$ROOT_DIR/cbl-tray" "$TRAY_BIN"
else
  (cd "$ROOT_DIR" && go build -o "$INSTALL_BIN" ./cmd/cbl && go build -o "$TRAY_BIN" ./cmd/cbl-tray)
  chmod 0755 "$INSTALL_BIN" "$TRAY_BIN"
fi

if [[ "$want_systemd" -eq 1 ]]; then
  echo "[2/4] Installing systemd --user service"
  install -m 0644 "$ROOT_DIR/install/systemd/cbl.service" "$SYSTEMD_DIR/cbl.service"
  systemctl --user daemon-reload
  systemctl --user enable --now cbl.service
fi

if [[ "$want_extension" -eq 1 ]]; then
  echo "[3/4] Installing GNOME Shell extension source"
  rm -rf "$EXT_DIR"
  mkdir -p "$EXT_DIR"
  cp -a "$ROOT_DIR/desktop/gnome-extension/cbl@hermes/." "$EXT_DIR/"
  chmod 0644 "$EXT_DIR/"*
  echo "GNOME extension files are in: $EXT_DIR"
  if command -v gnome-extensions >/dev/null 2>&1; then
    gnome-extensions enable cbl@hermes || true
    if gnome-extensions list --enabled 2>/dev/null | grep -Fxq 'cbl@hermes'; then
      echo "GNOME extension enabled: cbl@hermes"
    else
      echo "Run: gnome-extensions enable cbl@hermes"
    fi
  else
    echo "Run: gnome-extensions enable cbl@hermes"
  fi
fi

if [[ "$want_indicator" -eq 1 ]]; then
  echo "[4/4] Installing tray indicator"
  install -m 0644 "$ROOT_DIR/desktop/indicator/cbl-indicator.desktop" "$AUTOSTART_DIR/cbl-indicator.desktop"
  sed -i "s|^Exec=.*|Exec=${TRAY_BIN}|" "$AUTOSTART_DIR/cbl-indicator.desktop"
  echo "Tray indicator installed to: $TRAY_BIN"
  echo "Autostart entry installed to: $AUTOSTART_DIR/cbl-indicator.desktop"
fi

echo "Done."
