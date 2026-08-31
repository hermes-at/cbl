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
Usage: install.sh [--systemd] [--indicator] [--extension] [--all] [--proxy URL]

Default on GNOME: install the user service and GNOME top-bar extension.
Default elsewhere: install the user service and tray indicator fallback.
Use --indicator if you explicitly want the AppIndicator tray helper.
Use --proxy to bake an HTTP/SOCKS5 proxy into the user service.
EOF
}

proxy="${CBL_PROXY:-}"
want_systemd=0
want_extension=0
want_indicator=0
component_seen=0
if [[ $# -eq 0 ]]; then
  want_systemd=1
  if command -v gnome-extensions >/dev/null 2>&1; then
    want_extension=1
  else
    want_indicator=1
  fi
else
  for arg in "$@"; do
    case "$arg" in
      --systemd) want_systemd=1; component_seen=1 ;;
      --extension) want_extension=1; component_seen=1 ;;
      --indicator) want_indicator=1; component_seen=1 ;;
      --all) want_systemd=1; want_extension=1; want_indicator=1; component_seen=1 ;;
      --proxy) prev=1 ;;
      --proxy=*) proxy="${arg#--proxy=}" ;;
      -h|--help) usage; exit 0 ;;
      *)
        if [[ ${prev:-0} -eq 1 ]]; then
          proxy="$arg"
          prev=0
        else
          echo "Unknown option: $arg" >&2; usage; exit 1
        fi
        ;;
    esac
  done
fi

if [[ "$component_seen" -eq 0 ]]; then
  want_systemd=1
  if command -v gnome-extensions >/dev/null 2>&1; then
    want_extension=1
  else
    want_indicator=1
  fi
fi

if [[ -z "$proxy" ]]; then
  prev=0
  for arg in "$@"; do
    if [[ $prev -eq 1 ]]; then
      proxy="$arg"
      prev=0
    elif [[ "$arg" == --proxy=* ]]; then
      proxy="${arg#--proxy=}"
    elif [[ "$arg" == "--proxy" ]]; then
      prev=1
    fi
  done
fi
proxy="${proxy//\"}"

if [[ -n "$proxy" && ! "$proxy" =~ ^(http|https|socks5|socks5h):// ]]; then
  echo "Proxy must be a full URL, e.g. socks5h://127.0.0.1:2080" >&2
  exit 1
fi

if [[ "$want_systemd" -eq 1 || "$want_indicator" -eq 1 || -n "$proxy" ]]; then
  want_systemd=1
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
  mkdir -p "$SYSTEMD_DIR/cbl.service.d"
  {
    echo "[Service]"
    echo "Environment=\"CBL_PROXY=$proxy\""
  } > "$SYSTEMD_DIR/cbl.service.d/proxy.conf"
  if systemctl --user daemon-reload >/dev/null 2>&1; then
    systemctl --user enable --now cbl.service >/dev/null 2>&1 || true
    echo "User service installed: cbl.service"
  else
    echo "systemd user bus is not available right now; start cbl.service after login"
  fi
fi

if [[ "$want_extension" -eq 1 ]]; then
  echo "[3/4] Installing GNOME Shell extension source"
  if command -v gnome-extensions >/dev/null 2>&1; then
    gnome-extensions disable cbl@hermes >/dev/null 2>&1 || true
  fi
  rm -rf "$EXT_DIR"
  mkdir -p "$EXT_DIR"
  cp -a "$ROOT_DIR/desktop/gnome-extension/cbl@hermes/." "$EXT_DIR/"
  find "$EXT_DIR" -type d -exec chmod 0755 {} +
  find "$EXT_DIR" -type f -exec chmod 0644 {} +
  echo "GNOME extension files are in: $EXT_DIR"
  if command -v gnome-extensions >/dev/null 2>&1; then
    package_dir="$(mktemp -d)"
    if "$ROOT_DIR/install/ubuntu/package-gnome-extension.sh" "$package_dir" >/dev/null 2>&1; then
      package_zip="$package_dir/cbl-gnome-extension.zip"
      gnome-extensions install --force "$package_zip" || true
      gnome-extensions enable cbl@hermes || true
      if gnome-extensions list --enabled 2>/dev/null | grep -Fxq 'cbl@hermes'; then
        echo "GNOME extension enabled: cbl@hermes"
      else
        echo "Run: gnome-extensions enable cbl@hermes"
      fi
      rm -rf "$package_dir"
    else
      echo "Could not package GNOME extension; run: $ROOT_DIR/install/ubuntu/package-gnome-extension.sh"
    fi
  else
    echo "Run: gnome-extensions enable cbl@hermes"
  fi
  if [[ "$want_indicator" -eq 0 ]]; then
    rm -f "$AUTOSTART_DIR/cbl-indicator.desktop"
    if command -v pkill >/dev/null 2>&1; then
      pkill -xu "$(id -u)" cbl-tray >/dev/null 2>&1 || true
    fi
  fi
fi

if [[ "$want_indicator" -eq 1 ]]; then
  echo "[4/4] Installing tray indicator"
  install -m 0644 "$ROOT_DIR/desktop/indicator/cbl-indicator.desktop" "$AUTOSTART_DIR/cbl-indicator.desktop"
  sed -i "s|^Exec=.*|Exec=${TRAY_BIN}|" "$AUTOSTART_DIR/cbl-indicator.desktop"
  echo "Tray indicator installed to: $TRAY_BIN"
  echo "Autostart entry installed to: $AUTOSTART_DIR/cbl-indicator.desktop"
  if command -v pgrep >/dev/null 2>&1 && pgrep -xu "$(id -u)" cbl-tray >/dev/null 2>&1; then
    echo "Tray indicator is already running."
  elif [[ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]]; then
    (setsid "$TRAY_BIN" >/dev/null 2>&1 &)
    echo "Tray indicator started for this session."
  else
    echo "No desktop session detected; tray indicator will start on next login."
  fi
fi

echo "Done."
