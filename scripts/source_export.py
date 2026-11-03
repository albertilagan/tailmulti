"""Shared source-release selection and checks. Never reads ignored local data."""
import gzip
import hashlib
import io
import json
import plistlib
import re
import tarfile
from pathlib import Path

# Local-only names excluded at the repository root; nested source may reuse them.
ROOT_IGNORED = {".git", ".cache", "build", "dist", ".local-notes", ".claude", ".codex", ".si",
                "AGENTS.md", "CLAUDE.md", "CLAUDE.local.md"}
# Tool output excluded at any depth.
IGNORED = {".build", "__pycache__", ".DS_Store", "xcuserdata"}
SENSITIVE_SUFFIXES = {".p12", ".pfx", ".pem", ".key", ".keychain-db",
                      ".mobileprovision", ".provisionprofile", ".xcuserstate"}
PATTERNS = {
    "private key": re.compile(rb"-----BEGIN (?:RSA |EC |OPENSSH |DSA |ENCRYPTED )?PRIVATE KEY-----"),
    "Tailscale credential": re.compile(rb"tskey-(?:auth|api)-[A-Za-z0-9_-]{20,}"),
    "GitHub credential": re.compile(rb"(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})"),
    "AWS access key": re.compile(rb"(?:AKIA|ASIA)[A-Z0-9]{16}"),
    "local home path": re.compile(rb"/Users/(?!runner(?:/|\b)|Shared(?:/|\b))[A-Za-z0-9_.-]+/"),
    "local workspace path": re.compile(rb"/Volumes/[^\s`\"']+/[dp]/"),
}


def ignored(path: Path) -> bool:
    return (path.parts[0] in ROOT_IGNORED or any(part in IGNORED for part in path.parts)
            or path.suffix in {".pyc", ".pyo"})


def public_files(root: Path, clean_checkout: bool = False) -> list[Path]:
    manifest = json.loads((root / "release-manifest.json").read_text())
    if manifest.get("version") != 1:
        raise ValueError("Unsupported source manifest")
    files, directories = set(manifest["files"]), set(manifest["directories"])
    if any(Path(name).name != name or name in {".", ".."} for name in files | directories):
        raise ValueError("Manifest entries must be top-level names")
    result = []

    def visit(path: Path):
        relative = path.relative_to(root)
        if ignored(relative):
            if clean_checkout and relative.parts[0] != ".git":
                raise ValueError(f"Local artifact in clean checkout: {relative}")
            return
        if path.is_symlink():
            raise ValueError(f"Source symlink is not allowed: {relative}")
        if path.is_dir():
            for child in sorted(path.iterdir()):
                visit(child)
        elif path.is_file():
            result.append(relative)
        else:
            raise ValueError(f"Non-regular source entry: {relative}")

    for path in sorted(root.iterdir()):
        if ignored(Path(path.name)):
            visit(path)
        elif path.name in files and path.is_file():
            visit(path)
        elif path.name in directories and path.is_dir():
            visit(path)
        else:
            raise ValueError(f"Review unlisted source entry before release: {path.name}")
    for name in files | directories:
        if not (root / name).exists():
            raise ValueError(f"Missing source manifest entry: {name}")
    return sorted(result)


def audit(root: Path, clean_checkout: bool = False) -> list[Path]:
    files = public_files(root, clean_checkout)
    problems = []
    for relative in files:
        path = root / relative
        if path.suffix.lower() in SENSITIVE_SUFFIXES or path.name.startswith(".env"):
            problems.append(f"{relative}: private configuration or signing file")
        if path.name in {"SetupManifest.json", "native-signing.json", "installation.json"}:
            problems.append(f"{relative}: generated local configuration")
        data = path.read_bytes()
        if b"\x00" in data or len(data) > 5 * 1024 * 1024:
            problems.append(f"{relative}: binary or oversized file needs review")
        for label, pattern in PATTERNS.items():
            if pattern.search(data):
                # Paths and values are never printed, only source filenames and categories.
                problems.append(f"{relative}: possible {label}")
    if problems:
        raise ValueError("Source audit failed:\n" + "\n".join(problems))
    return files


def check_tag(root: Path, tag: str) -> None:
    match = re.fullmatch(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?", tag)
    if not match:
        raise ValueError("Use a version tag such as v0.4.4 or v0.4.4-rc.1")
    info = plistlib.loads((root / "macos/Resources/Info.plist").read_bytes())
    if ".".join(match.groups()) != info["CFBundleShortVersionString"]:
        raise ValueError("Release tag does not match the app's source version")


def archive(root: Path, tag: str, destination: Path) -> Path:
    check_tag(root, tag)
    files = audit(root)
    destination.mkdir(parents=True, exist_ok=True)
    name = f"tailmulti-{tag}-source"
    output = destination / f"{name}.tar.gz"
    # Refuse overwrites so a reviewed release asset cannot silently change on rerun.
    with output.open("xb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as tar:
                for relative in files:
                    path = root / relative
                    data = path.read_bytes()
                    entry = tarfile.TarInfo(f"{name}/{relative.as_posix()}")
                    entry.size = len(data)
                    entry.mode = 0o755 if path.stat().st_mode & 0o111 else 0o644
                    entry.mtime = 0
                    entry.uid = entry.gid = 0
                    entry.uname = entry.gname = ""
                    tar.addfile(entry, io.BytesIO(data))
    checksum = hashlib.sha256(output.read_bytes()).hexdigest()
    (destination / f"{name}.sha256").write_text(f"{checksum}  {output.name}\n")
    return output
