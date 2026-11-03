#!/bin/sh
# Offline account tests; no helper registration or host activation.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"
export PYTHONDONTWRITEBYTECODE=1
python3 scripts/audit-source.py
python3 scripts/verify-provenance.py
python3 -m unittest discover -s scripts/tests -v
DEVELOPER_DIR=$(python3 scripts/select-xcode.py)
export DEVELOPER_DIR
./scripts/go.sh test -race ./...
./scripts/go.sh vet ./...
./scripts/swift.sh test
./scripts/build-app.sh --candidate
python3 scripts/test-preview.py
