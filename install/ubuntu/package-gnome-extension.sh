#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/dist}"
ZIP_PATH="${OUT_DIR}/cbl-gnome-extension.zip"
EXT_SRC="$ROOT_DIR/desktop/gnome-extension/cbl@codex-limits"

mkdir -p "$OUT_DIR"
python3 - <<'PY' "$EXT_SRC" "$ZIP_PATH"
from pathlib import Path
from zipfile import ZipFile, ZIP_DEFLATED
import sys
src = Path(sys.argv[1])
out = Path(sys.argv[2])
with ZipFile(out, 'w', ZIP_DEFLATED) as zf:
    for path in sorted(src.rglob('*')):
        if path.is_file():
            zf.write(path, path.relative_to(src))
print(out)
PY
