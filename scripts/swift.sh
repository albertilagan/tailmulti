#!/bin/sh
# Keep compiler and linker SDK selection consistent, while Package.swift controls minimum OS.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p "$root/.cache/build-tmp"
chmod 700 "$root/.cache/build-tmp"
export TMPDIR="$root/.cache/build-tmp/"
case "${1:-}" in
    build|test) command=$1; shift ;;
    *) echo 'Usage: scripts/swift.sh build|test [SwiftPM arguments]' >&2; exit 2 ;;
esac
sdk=$(/usr/bin/xcrun --sdk macosx --show-sdk-path)
sdk_version=$(/usr/bin/xcrun --sdk macosx --show-sdk-version)
python3 - "$sdk_version" <<'PY'
import sys
version = tuple(int(p) for p in sys.argv[1].split('.'))
if version < (27, 2):
    sys.exit('Tailmulti requires macOS SDK 27.2 or newer. Select Xcode 27.2 or newer using DEVELOPER_DIR.')
PY
# This toolchain passes --sysroot to clang, which finds libraries but records the deployment
# target as the linked SDK. -isysroot makes clang read the SDK's actual version for AppKit.
exec /usr/bin/xcrun swift "$command" --package-path macos --scratch-path .cache/swift-build \
    --sdk "$sdk" -Xswiftc -Xclang-linker -Xswiftc -isysroot -Xswiftc -Xclang-linker -Xswiftc "$sdk" "$@"
