#!/bin/sh
# Local staging only. Does not install, register, launch, download releases or alter networking.
set -eu
signing_config=
candidate=0
for arg in "$@"; do
    case "$arg" in
        --development-sign) signing_config=.cache/native-signing.json ;;
        --developer-id-sign) signing_config=developer-id ;;
        --candidate) candidate=1 ;;
        *) echo 'Usage: scripts/build-app.sh [--development-sign | --developer-id-sign] [--candidate]' >&2; exit 2 ;;
    esac
done
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p "$root/.cache/build-tmp"
chmod 700 "$root/.cache/build-tmp"
export TMPDIR="$root/.cache/build-tmp/"
[ "$(/usr/bin/id -u)" -ne 0 ] || { echo 'Build as a regular user, not root.' >&2; exit 1; }
if [ "$signing_config" = developer-id ]; then
    # Release signing: the identity's SHA-1 comes from the environment, e.g. a CI keychain import.
    printf '%s' "${TAILMULTI_DEVELOPER_ID:-}" | grep -Eq '^[0-9A-Fa-f]{40}$' ||
        { echo 'Set TAILMULTI_DEVELOPER_ID to the Developer ID Application identity SHA-1.' >&2; exit 1; }
    mkdir -p "$root/.cache"
    signing_config="$root/.cache/developer-id-signing.json"
    printf '{"identity": "%s", "signingMode": "developer-id"}\n' "$TAILMULTI_DEVELOPER_ID" > "$signing_config"
fi
if [ "$candidate" -eq 0 ] && [ -S '/Library/Application Support/Tailmulti/run/control.sock' ]; then
    echo 'Remove the background service in Tailmulti Settings before replacing its app bundle.' >&2
    exit 1
fi
mkdir -p build
build_number=$(/usr/bin/plutil -extract CFBundleVersion raw macos/Resources/Info.plist)
scripts/go.sh build -trimpath -ldflags "-X main.serviceBuild=$build_number" -o ../build/tailmultid ./cmd/tailmultid
scripts/swift.sh build -c release
swiftbin=$(scripts/swift.sh build -c release --show-bin-path)
stage_root=$(mktemp -d "$root/build/.app-stage.XXXXXX")
app="$stage_root/Tailmulti.app"
trap 'rm -rf "$stage_root"' EXIT HUP INT TERM
mkdir -p "$app/Contents/MacOS" "$app/Contents/Library/HelperTools" "$app/Contents/Resources/Licenses"
cp "$swiftbin/Tailmulti" "$app/Contents/MacOS/Tailmulti"
# Sparkle lives in Contents/Frameworks. Its XPC services only serve sandboxed apps, so they are dropped.
mkdir -p "$app/Contents/Frameworks"
/usr/bin/ditto "$swiftbin/Sparkle.framework" "$app/Contents/Frameworks/Sparkle.framework"
rm -rf "$app/Contents/Frameworks/Sparkle.framework/XPCServices" "$app/Contents/Frameworks/Sparkle.framework/Versions/B/XPCServices"
# SwiftPM records build-directory rpaths; ship only the bundle-relative one.
for rpath in $(/usr/bin/otool -l "$app/Contents/MacOS/Tailmulti" | awk '$1 == "cmd" && $2 == "LC_RPATH" { getline; getline; print $2 }'); do
    /usr/bin/install_name_tool -delete_rpath "$rpath" "$app/Contents/MacOS/Tailmulti"
done
/usr/bin/install_name_tool -add_rpath @executable_path/../Frameworks "$app/Contents/MacOS/Tailmulti"
cp build/tailmultid "$app/Contents/Library/HelperTools/tailmultid"
cp macos/Resources/Info.plist "$app/Contents/Info.plist"
cp LICENSE "$app/Contents/Resources/Licenses/Tailmulti.txt"
cp NOTICE "$app/Contents/Resources/Licenses/NOTICE.txt"
cp engine/LICENSE "$app/Contents/Resources/Licenses/Tailmix.txt"
cp engine/tsnet/LICENSE "$app/Contents/Resources/Licenses/Tailscale-tsnet.txt"
cp engine/netns/LICENSE "$app/Contents/Resources/Licenses/Tailscale-netns.txt"
cp engine/UPSTREAM.json engine/ADAPTATIONS.md "$app/Contents/Resources/Licenses/"
cp .cache/swift-build/checkouts/Sparkle/LICENSE "$app/Contents/Resources/Licenses/Sparkle.txt"
scripts/go.sh list -deps -json ./cmd/tailmultid > build/packages.json
cp "$(scripts/go.sh env GOROOT)/LICENSE" "$app/Contents/Resources/Licenses/Go-runtime.txt"
python3 scripts/module-notices.py build/packages.json "$app/Contents/Resources/Licenses"
python3 scripts/configure-service.py --app "$app" --signing-config "$signing_config"
/usr/bin/codesign --verify --deep --strict "$app"
/usr/bin/plutil -lint "$app/Contents/Info.plist"
python3 scripts/verify-app.py --app "$app"
if [ "$candidate" -eq 1 ]; then
    mkdir -p "$root/build/candidates"
    destination=$(mktemp -d "$root/build/candidates/native.XXXXXX")
    mv "$app" "$destination/Tailmulti.app"
    printf '%s\n' "$destination/Tailmulti.app" > "$root/build/candidate-app-path.txt"
    echo "Staged candidate at $destination/Tailmulti.app. The registered app is unchanged."
else
    if [ -S '/Library/Application Support/Tailmulti/run/control.sock' ]; then
        echo 'The service started during this build; its app bundle was not replaced.' >&2
        exit 1
    fi
    rm -rf "$root/build/Tailmulti.app"
    mv "$app" "$root/build/Tailmulti.app"
    echo "Staged build/Tailmulti.app with native background-service registration."
fi
echo 'No package installer, registration or activation performed.'
