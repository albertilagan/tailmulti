#!/usr/bin/env python3
"""Inspect the native service bundle without registering or activating it."""
import argparse
import hashlib
import json
from pathlib import Path
import plistlib
import re
import subprocess

ROOT = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--app", type=Path, default=ROOT / "build/Tailmulti.app")
args = parser.parse_args()
app = args.app.resolve()
resources = app / "Contents/Resources"
manifest = json.loads((resources / "SetupManifest.json").read_text())
assert manifest["version"] == 3 and "ownerUID" not in manifest
assert manifest["registration"] == "SMAppService"
assert manifest["serviceLabel"] == "dev.tailmulti.tailmultid"
assert manifest["signingMode"] in ("development", "developer-id", "adhoc")
assert not list(app.rglob("*.pkg")), "An obsolete installer is still bundled"
helper = app / "Contents/Library/HelperTools/tailmultid"
assert helper.is_file() and not helper.is_symlink()
assert helper.stat().st_mode & 0o022 == 0
assert hashlib.sha256(helper.read_bytes()).hexdigest() == manifest["helperSHA256"]
launch = plistlib.loads((app / "Contents/Library/LaunchDaemons/dev.tailmulti.tailmultid.plist").read_bytes())
assert launch["Label"] == manifest["serviceLabel"]
assert launch["BundleProgram"] == "Contents/Library/HelperTools/tailmultid"
assert launch["ProgramArguments"] == ["tailmultid", "--service"]
assert launch["RunAtLoad"] is True and launch["KeepAlive"] is True and launch["Umask"] == 0o077
assert all(key not in launch for key in ("Program", "UserName", "Sockets", "EnvironmentVariables"))
info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
expected_info = plistlib.loads((ROOT / "macos/Resources/Info.plist").read_bytes())
assert info["CFBundleIdentifier"] == "dev.tailmulti.app"
for key in ("CFBundleVersion", "CFBundleShortVersionString"):
    assert info[key] == expected_info[key], "Bundle version does not match the source"
build_metadata = subprocess.check_output([
    "/usr/bin/xcrun", "vtool", "-show-build", str(app / "Contents/MacOS/Tailmulti")
], text=True, timeout=10)
sdk_versions = re.findall(r"^\s+sdk\s+([\d.]+)$", build_metadata, re.M)
minimum_versions = re.findall(r"^\s+minos\s+([\d.]+)$", build_metadata, re.M)
sdk = subprocess.check_output(["/usr/bin/xcrun", "--sdk", "macosx", "--show-sdk-version"], text=True, timeout=10).strip()
def version_tuple(value):
    parts = [int(part) for part in value.split(".")]
    return tuple(parts + [0] * (3 - len(parts)))
assert sdk_versions and all(version_tuple(v) == version_tuple(sdk) for v in sdk_versions), "App does not record the selected build SDK"
assert version_tuple(sdk) >= version_tuple("27.2"), "Tailmulti requires macOS SDK 27.2 or newer"
assert info["LSMinimumSystemVersion"] == expected_info["LSMinimumSystemVersion"], "Bundle minimum OS does not match source"
assert minimum_versions and all(version_tuple(v) == version_tuple(info["LSMinimumSystemVersion"]) for v in minimum_versions), "App minimum OS does not match its manifest"
binary = (app / "Contents/MacOS/Tailmulti").read_bytes()
assert b"SMAppService" in binary
assert b"TailmultiSetup.pkg" not in binary and b"Installer.app" not in binary
for source in (ROOT / "macos/Sources").rglob("*.swift"):
    assert "openInstaller" not in source.read_text() and "TailmultiSetup.pkg" not in source.read_text()
subprocess.run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(app)], check=True)
teams = []
for target, identifier in ((app, "dev.tailmulti.app"), (helper, "dev.tailmulti.tailmultid")):
    if manifest["signingMode"] != "adhoc":
        subprocess.run(["/usr/bin/codesign", "--verify", "--strict", "-R",
                        f'=anchor apple generic and identifier "{identifier}"', str(target)], check=True)
        result = subprocess.run(["/usr/bin/codesign", "--display", "--verbose=4", str(target)],
                                capture_output=True, text=True, check=True)
        team = re.search(r"^TeamIdentifier=([A-Z0-9]{10})$", result.stderr, re.M)
        assert team, "A signed component has no valid team"
        teams.append(team[1])
if teams:
    assert len(set(teams)) == 1 and teams[0] == manifest["signingTeam"]
metadata = json.loads(subprocess.check_output([str(helper), "--build-info"], text=True, timeout=10))
assert metadata == {"version": 2, "ownerBinding": "first-console-user"}
for name in ("Tailmulti.txt", "NOTICE.txt", "Tailmix.txt", "Tailscale-tsnet.txt", "Tailscale-netns.txt", "Go-runtime.txt", "UPSTREAM.json", "ADAPTATIONS.md"):
    assert (resources / "Licenses" / name).is_file()
report = json.loads(subprocess.check_output([str(helper), "--check-host"], text=True, timeout=20))
assert report["version"] == 1
assert {check["id"] for check in report["checks"]} == {
    "routes", "vpn", "dns", "dns_port", "paths", "automatic_dns"
}
validation = ROOT / "build/validation"
validation.mkdir(exist_ok=True)
(validation / "setup-preflight.json").write_text(json.dumps(report) + "\n")
result = {"nativeRegistration": "SMAppService", "bundleVersion": info["CFBundleVersion"], "linkedSDK": sdk,
          "minimumOS": info["LSMinimumSystemVersion"],
          "signingMode": manifest["signingMode"], "matchingAppleTeam": bool(teams),
          "ownerBoundAtFirstServiceStart": True, "externalInstallerAbsent": True,
          "readOnlyPreflightCompleted": True, "registrationPerformed": False,
          "networkActivationPerformed": False, "notarized": False}
(validation / "native-app.json").write_text(json.dumps(result, indent=2) + "\n")
print("PASS: native service, signatures, owner binding, licenses and read-only checks. No Installer path or registration.")
