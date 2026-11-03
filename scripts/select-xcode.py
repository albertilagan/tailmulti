#!/usr/bin/env python3
"""Print a compatible developer directory without changing xcode-select."""
import os
import subprocess
from pathlib import Path


def sdk_version(developer: Path):
    env = dict(os.environ, DEVELOPER_DIR=str(developer))
    try:
        result = subprocess.run(["/usr/bin/xcrun", "--sdk", "macosx", "--show-sdk-version"],
                                env=env, capture_output=True, text=True, timeout=15, check=True)
        parts = tuple(int(p) for p in result.stdout.strip().split("."))
        return parts if parts >= (27, 2) else ()
    except (OSError, ValueError, subprocess.SubprocessError):
        return ()


def select() -> Path:
    if os.environ.get("DEVELOPER_DIR"):
        developer = Path(os.environ["DEVELOPER_DIR"]).resolve()
        if sdk_version(developer):
            return developer
        raise ValueError("DEVELOPER_DIR must select Xcode with macOS SDK 27.2 or newer.")
    root = Path(__file__).resolve().parent.parent
    apps = list(Path("/Applications").glob("Xcode*.app"))
    apps += [p for p in (root / ".cache/toolchains").glob("*/Xcode.app")
             if not any(word in p.parent.name.lower() for word in ("incomplete", "retry"))]
    candidates = {app / "Contents/Developer" for app in apps}
    try:
        selected = subprocess.check_output(["/usr/bin/xcode-select", "-p"], text=True, timeout=10).strip()
        candidates.add(Path(selected))
    except (OSError, subprocess.SubprocessError):
        pass
    versions = [(sdk_version(p), str(p)) for p in candidates]
    versions = [v for v in versions if v[0]]
    if not versions:
        raise ValueError("Install Xcode 27.2 or newer and finish its first-launch setup, then try again.")
    return Path(max(versions)[1])


if __name__ == "__main__":
    try:
        print(select())
    except ValueError as error:
        raise SystemExit(str(error))
