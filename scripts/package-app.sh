#!/bin/sh
# Build, Developer ID sign, notarize and staple the app, then zip it into dist/. Never uploads or installs.
# Env: TAILMULTI_DEVELOPER_ID (identity SHA-1), TAILMULTI_TEAM_ID, NOTARY_KEY_PATH, NOTARY_KEY_ID, NOTARY_ISSUER_ID.
set -eu
[ $# -eq 1 ] || { echo 'Usage: scripts/package-app.sh vMAJOR.MINOR.PATCH' >&2; exit 2; }
tag=$1
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"
export PYTHONDONTWRITEBYTECODE=1
for name in TAILMULTI_DEVELOPER_ID TAILMULTI_TEAM_ID NOTARY_KEY_PATH NOTARY_KEY_ID NOTARY_ISSUER_ID; do
    eval "value=\${$name:-}"
    [ -n "$value" ] || { echo "Set $name to package a release app." >&2; exit 1; }
done
python3 -c 'import sys; sys.path.insert(0, "scripts"); from source_export import check_tag; from pathlib import Path; check_tag(Path("."), sys.argv[1])' "$tag"

./scripts/build-app.sh --developer-id-sign --candidate
app=$(cat build/candidate-app-path.txt)
for part in "$app" "$app/Contents/Library/HelperTools/tailmultid"; do
    team=$(/usr/bin/codesign --display --verbose=4 "$part" 2>&1 | sed -n 's/^TeamIdentifier=//p')
    [ "$team" = "$TAILMULTI_TEAM_ID" ] || { echo "Unexpected signing team for $part" >&2; exit 1; }
done

work=$(mktemp -d "$root/build/.notarize.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
/usr/bin/ditto -c -k --keepParent "$app" "$work/submit.zip"
/usr/bin/xcrun notarytool submit "$work/submit.zip" --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" \
    --issuer "$NOTARY_ISSUER_ID" --wait --output-format json > "$work/notary.json"
status=$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1])).get("status", ""))' "$work/notary.json")
if [ "$status" != Accepted ]; then
    id=$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1])).get("id", ""))' "$work/notary.json")
    echo "Notarization status: ${status:-unknown}. Inspect with: xcrun notarytool log $id" >&2
    exit 1
fi
/usr/bin/xcrun stapler staple "$app"
/usr/bin/xcrun stapler validate "$app"
/usr/sbin/spctl --assess --type execute --verbose=2 "$app"

mkdir -p dist
name="Tailmulti-$tag-macOS"
output="dist/$name.zip"
[ ! -e "$output" ] || { echo "$output already exists; remove it before repackaging." >&2; exit 1; }
/usr/bin/ditto -c -k --keepParent "$app" "$output"
(cd dist && /usr/bin/shasum -a 256 "$name.zip" > "$name.sha256")
echo "Created $output and its SHA-256 checksum. Nothing uploaded."
