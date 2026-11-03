#!/usr/bin/env python3
"""Create a reviewed source archive and checksum locally. Never publishes."""
import argparse
import subprocess
import sys
from pathlib import Path
from source_export import archive

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag", help="version tag matching Info.plist, for example v0.4.4")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    subprocess.run([sys.executable, str(root / "scripts/verify-provenance.py")], check=True)
    try:
        output = archive(root, args.tag, root / "dist")
    except (ValueError, FileExistsError) as error:
        raise SystemExit(str(error))
    print(f"Created {output.relative_to(root)} and its SHA-256 checksum. Nothing uploaded.")
