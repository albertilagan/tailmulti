#!/bin/sh
# Build only. The user opens the app and approves its service separately.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"
export PYTHONDONTWRITEBYTECODE=1
DEVELOPER_DIR=$(python3 scripts/select-xcode.py)
export DEVELOPER_DIR
python3 scripts/prepare-local-signing.py
exec ./scripts/build-app.sh --development-sign --candidate
