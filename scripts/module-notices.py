#!/usr/bin/env python3
"""Copy resolved modules' license/notice files for the local binary bundle."""
import json
from pathlib import Path
import sys
import shutil

data = Path(sys.argv[1]).read_text()
output = Path(sys.argv[2]) / "Modules"
if output.exists():
    shutil.rmtree(output)
output.mkdir()
decoder = json.JSONDecoder()
modules = {}
while data.strip():
    module, end = decoder.raw_decode(data.lstrip())
    data = data.lstrip()[end:]
    module = module.get("Module")
    if module is not None:
        modules[module["Path"]] = module
index = []
for module in modules.values():
    if module.get("Main") or "Dir" not in module:
        continue
    directory = Path(module["Dir"])
    target = output / (module["Path"].replace("/", "_") + "@" + module.get("Version", "unknown"))
    target.mkdir(exist_ok=True)
    copied = []
    # Some modules embed third-party notices below their root. Preserve all
    # named license/notice files, with their relative directories.
    for source in directory.rglob("*"):
        if source.is_file() and (source.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "PATENTS"))):
            relative = source.relative_to(directory)
            dest = target / relative
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(source.read_bytes())
            copied.append(str(relative))
    index.append({"module": module["Path"], "version": module.get("Version"), "notices": copied})
(output / "index.json").write_text(json.dumps(index, indent=2) + "\n")
missing = [m["module"] for m in index if not m["notices"]]
if missing:
    raise SystemExit("Missing dependency notices: " + ", ".join(missing))
