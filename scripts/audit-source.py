#!/usr/bin/env python3
"""Check the public source tree without reading local caches or account data."""
import argparse
from pathlib import Path
from source_export import audit

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--clean-checkout", action="store_true",
                        help="reject ignored artifacts present in a fresh CI checkout")
    args = parser.parse_args()
    try:
        files = audit(Path(__file__).resolve().parent.parent, args.clean_checkout)
    except ValueError as error:
        raise SystemExit(str(error))
    print(f"PASS: {len(files)} public source files checked; local artifacts excluded.")
