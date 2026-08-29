#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: install.sh [--systemd] [--indicator] [--extension] [--all]

Default: install the user service and tray indicator.
Use --extension if you explicitly want the GNOME Shell extension too.
EOF
}

if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
  usage
  exit 0
fi

# If this script is running from a checked-out repo or an unpacked release,
# reuse the local installer and skip the network bootstrap.
SCRIPT_SOURCE="${BASH_SOURCE[0]-}"
if [[ -n "$SCRIPT_SOURCE" && -f "$SCRIPT_SOURCE" ]]; then
  SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
  if [[ -x "$SCRIPT_DIR/install/ubuntu/install.sh" ]]; then
    exec "$SCRIPT_DIR/install/ubuntu/install.sh" "$@"
  fi
fi

REPO="${CBL_REPO:-hermes-at/cbl}"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for the bootstrap installer" >&2
  exit 1
fi

api_json="$TMP_DIR/latest.json"
curl -fsSL \
  -H 'Accept: application/vnd.github+json' \
  -H 'User-Agent: cbl-install' \
  "https://api.github.com/repos/${REPO}/releases/latest" \
  -o "$api_json"

version="$(python3 - <<'PY' "$api_json"
import json, pathlib, sys
obj = json.loads(pathlib.Path(sys.argv[1]).read_text())
print(obj["tag_name"])
PY
)"

asset_name="cbl-${version}-linux-amd64.tar.gz"
asset_url="$(python3 - <<'PY' "$api_json" "$asset_name"
import json, pathlib, sys
obj = json.loads(pathlib.Path(sys.argv[1]).read_text())
asset = None
for item in obj.get('assets', []):
    if item.get('name') == sys.argv[2]:
        asset = item
        break
if asset is None:
    raise SystemExit(f"asset not found: {sys.argv[2]}")
print(asset['browser_download_url'])
PY
)"

archive="$TMP_DIR/$asset_name"
curl -fsSL -L "$asset_url" -o "$archive"
mkdir -p "$TMP_DIR/extract"
tar -xzf "$archive" -C "$TMP_DIR/extract"

if [[ $# -eq 0 ]]; then
  exec "$TMP_DIR/extract/cbl-${version}/install.sh"
fi
exec "$TMP_DIR/extract/cbl-${version}/install.sh" "$@"
