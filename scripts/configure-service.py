#!/usr/bin/env python3
"""Bundle and sign the native service. Never register, install or activate it."""
import argparse
import hashlib
import json
import plistlib
import re
import subprocess
from pathlib import Path

LABEL = "dev.tailmulti.tailmultid"
APP_ID = "dev.tailmulti.app"
HELPER = "Contents/Library/HelperTools/tailmultid"


def signature_team(path: Path) -> str:
    result = subprocess.run(["/usr/bin/codesign", "--display", "--verbose=4", str(path)],
                            capture_output=True, text=True, check=True)
    match = re.search(r"^TeamIdentifier=(\w+)$", result.stderr, re.M)
    return match[1] if match and match[1] != "not" else ""


def configure(app: Path, owner: int, signing_config: str) -> None:
    if not 501 <= owner < 2**32 - 1:
        raise ValueError("A desktop owner is required")
    identity, mode = "-", "adhoc"
    if signing_config:
        config = json.loads(Path(signing_config).read_text())
        identity, mode = config["identity"], config["signingMode"]
        if not re.fullmatch(r"[0-9A-Fa-f]{40}", identity) or mode != "development":
            raise ValueError("Use an explicitly selected Apple Development signing identity")
    launch_dir = app / "Contents/Library/LaunchDaemons"
    launch_dir.mkdir(parents=True, exist_ok=True)
    launch = {"Label": LABEL, "BundleProgram": HELPER,
              "ProgramArguments": ["tailmultid", "--service", "--owner-uid", str(owner)],
              "RunAtLoad": True, "KeepAlive": True, "ThrottleInterval": 10,
              "ProcessType": "Background", "Umask": 0o077}
    (launch_dir / (LABEL + ".plist")).write_bytes(plistlib.dumps(launch))
    helper = app / HELPER
    for path, identifier in ((helper, LABEL), (app, APP_ID)):
        if path == app:
            team = signature_team(helper) if mode != "adhoc" else ""
            if mode != "adhoc" and not team:
                raise ValueError("The signed helper has no Apple team identity")
            manifest = {"version": 2, "ownerUID": owner,
                        "helperSHA256": hashlib.sha256(helper.read_bytes()).hexdigest(),
                        "serviceLabel": LABEL, "registration": "SMAppService",
                        "signingMode": mode, "signingTeam": team}
            (app / "Contents/Resources/SetupManifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        subprocess.run(["/usr/bin/codesign", "--force", "--sign", identity, "--identifier", identifier,
                        "--options", "runtime", "--timestamp=none", str(path)], check=True, timeout=60)
        subprocess.run(["/usr/bin/codesign", "--verify", "--strict", str(path)], check=True)
        if mode != "adhoc":
            subprocess.run(["/usr/bin/codesign", "--verify", "--strict", "-R",
                            f'=anchor apple generic and identifier "{identifier}"', str(path)], check=True)
    if mode != "adhoc" and signature_team(app) != signature_team(helper):
        raise ValueError("App and helper signing teams differ")
    print(f"Native service bundled with {mode} signing. No upload, registration or networking change.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, required=True)
    parser.add_argument("--owner-uid", type=int, required=True)
    parser.add_argument("--signing-config", default="")
    args = parser.parse_args()
    configure(args.app, args.owner_uid, args.signing_config)
