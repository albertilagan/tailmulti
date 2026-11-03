#!/usr/bin/env python3
import hashlib
import json
from pathlib import Path
root = Path(__file__).resolve().parent.parent
source = root / "upstream/tailmix"
record = json.loads((root / "engine/UPSTREAM.json").read_text())
for entry in record["files"]:
    actual = hashlib.sha256((source / entry["path"]).read_bytes()).hexdigest()
    assert actual == entry["sha256"], f"Upstream changed: {entry['path']}"
baseline = root / "upstream/tailmix.SHA256.json"
expected = json.loads(baseline.read_text())
actual = {str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest() for p in source.rglob("*") if p.is_file()}
assert actual == expected, "Reference snapshot differs from the pinned manifest"
assert not any(p.is_symlink() for p in source.rglob("*")), "Snapshot contains a symlink"
print(f"Verified {len(record['files'])} imported hashes and all {len(expected)} reference files.")
