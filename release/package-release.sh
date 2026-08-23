#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/dist}"
VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)}"
BIN_DIR="$OUT_DIR/cbl-$VERSION"
ZIP_PATH="$OUT_DIR/cbl-$VERSION-linux-amd64.zip"
TAR_PATH="$OUT_DIR/cbl-$VERSION-linux-amd64.tar.gz"

mkdir -p "$BIN_DIR" "$OUT_DIR"
( cd "$ROOT_DIR" && go build -o "$BIN_DIR/cbl" ./cmd/cbl )
cp "$ROOT_DIR/install.sh" "$BIN_DIR/install.sh"
cp -R "$ROOT_DIR/install" "$BIN_DIR/install"
cp -R "$ROOT_DIR/desktop" "$BIN_DIR/desktop"
cp "$ROOT_DIR/README.md" "$BIN_DIR/README.md"
cp "$ROOT_DIR/Makefile" "$BIN_DIR/Makefile"
cp "$ROOT_DIR/.gitignore" "$BIN_DIR/.gitignore"
( cd "$OUT_DIR" && tar -czf "$TAR_PATH" "cbl-$VERSION" )
python3 - <<'PY' "$OUT_DIR" "$VERSION" "$ZIP_PATH"
from pathlib import Path
from zipfile import ZipFile, ZIP_DEFLATED
import sys
out = Path(sys.argv[1])
version = sys.argv[2]
zip_path = Path(sys.argv[3])
root = out / f"cbl-{version}"
with ZipFile(zip_path, 'w', ZIP_DEFLATED) as zf:
    for path in sorted(root.rglob('*')):
        if path.is_file():
            zf.write(path, path.relative_to(out))
print(zip_path)
PY
printf '%s\n' "$TAR_PATH"
