#!/usr/bin/env python3
"""Select an existing development certificate. Does not export keys or sign code."""
import json
import os
import re
import subprocess
import tempfile
from pathlib import Path


def choose(identities: set[str], explicit: str, saved: str) -> str:
    selected = (explicit or saved).upper()
    if selected:
        if not re.fullmatch(r"[A-F0-9]{40}", selected) or selected not in identities:
            raise ValueError("The selected identity is not a valid Apple Development signing identity in this keychain.")
        return selected
    if not identities:
        raise ValueError("Create an Apple Development certificate in Xcode Settings → Accounts → Manage Certificates first.")
    if len(identities) != 1:
        raise ValueError("Choose a certificate with TAILMULTI_SIGNING_IDENTITY=<hash> from:\n" + "\n".join(sorted(identities)))
    return next(iter(identities))


def main():
    output = subprocess.check_output(["/usr/bin/security", "find-identity", "-v", "-p", "codesigning"],
                                     text=True, timeout=15)
    identities = set(re.findall(r'\b([A-F0-9]{40})\s+"Apple Development:[^\n]+"', output))
    root = Path(__file__).resolve().parent.parent
    config = root / ".cache/native-signing.json"
    saved = ""
    if config.is_symlink():
        raise ValueError("Local signing configuration must not be a symlink.")
    if config.exists():
        data = json.loads(config.read_text())
        if data.get("signingMode") == "development":
            saved = data.get("identity", "")
    selected = choose(identities, os.environ.get("TAILMULTI_SIGNING_IDENTITY", ""), saved)
    config.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".signing-", dir=config.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump({"identity": selected, "signingMode": "development"}, stream)
            stream.write("\n")
        os.replace(temporary, config)
    finally:
        Path(temporary).unlink(missing_ok=True)
    print("Selected an existing Apple Development certificate. No keys exported.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, subprocess.SubprocessError) as error:
        raise SystemExit(str(error))
