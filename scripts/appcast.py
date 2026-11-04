#!/usr/bin/env python3
"""Sparkle appcast tooling. `item` describes one signed release zip; `feed` builds appcast.xml from published items.

Never signs or uploads anything itself. The EdDSA signature comes from engine/tools/sparklesign.
"""
import argparse
import base64
import email.utils
import json
import plistlib
import re
import sys
import xml.etree.ElementTree as ET
from datetime import datetime
from pathlib import Path

SPARKLE = "http://www.andymatuschak.org/xml-namespaces/sparkle"
REPO = re.compile(r"[A-Za-z0-9-]+/[A-Za-z0-9._-]+")
TAG = re.compile(r"v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?")


def make_item(zip_path: Path, info_plist: Path, tag: str, repo: str, signature: str) -> dict:
    if not TAG.fullmatch(tag) or not REPO.fullmatch(repo):
        raise ValueError("invalid tag or repository")
    info = plistlib.loads(info_plist.read_bytes())
    short, build = info["CFBundleShortVersionString"], info["CFBundleVersion"]
    if tag.split("-")[0] != "v" + short or not build.isdigit():
        raise ValueError("tag, short version and build number disagree")
    if len(base64.b64decode(signature, validate=True)) != 64:
        raise ValueError("signature must be a base64 Ed25519 signature")
    return {
        "version": build, "shortVersion": short, "minimumSystemVersion": info["LSMinimumSystemVersion"],
        "url": f"https://github.com/{repo}/releases/download/{tag}/{zip_path.name}",
        "length": zip_path.stat().st_size, "edSignature": signature, "tag": tag,
    }


def validate(item: dict, repo: str) -> None:
    prefix = f"https://github.com/{repo}/releases/download/{item['tag']}/"
    if (not TAG.fullmatch(item["tag"]) or not str(item["version"]).isdigit() or not item["url"].startswith(prefix)
            or not isinstance(item["length"], int) or item["length"] <= 0
            or len(base64.b64decode(item["edSignature"], validate=True)) != 64):
        raise ValueError(f"invalid appcast item for {item.get('tag')}")


def build_feed(releases: list[dict], repo: str) -> bytes:
    """releases: [{"item": {...}, "publishedAt": ISO 8601, "htmlUrl": str}] for published releases only."""
    ET.register_namespace("sparkle", SPARKLE)
    rss = ET.Element("rss", {"version": "2.0"})
    channel = ET.SubElement(rss, "channel")
    ET.SubElement(channel, "title").text = "Tailmulti"
    ET.SubElement(channel, "link").text = f"https://github.com/{repo}"
    ET.SubElement(channel, "description").text = "Tailmulti releases"
    for release in sorted(releases, key=lambda r: int(r["item"]["version"]), reverse=True):
        item = release["item"]
        validate(item, repo)
        node = ET.SubElement(channel, "item")
        ET.SubElement(node, "title").text = f"Tailmulti {item['shortVersion']}"
        published = datetime.fromisoformat(release["publishedAt"].replace("Z", "+00:00"))
        ET.SubElement(node, "pubDate").text = email.utils.format_datetime(published)
        ET.SubElement(node, f"{{{SPARKLE}}}version").text = str(item["version"])
        ET.SubElement(node, f"{{{SPARKLE}}}shortVersionString").text = item["shortVersion"]
        ET.SubElement(node, f"{{{SPARKLE}}}minimumSystemVersion").text = item["minimumSystemVersion"]
        if release["htmlUrl"].startswith(f"https://github.com/{repo}/releases/"):
            ET.SubElement(node, f"{{{SPARKLE}}}fullReleaseNotesLink").text = release["htmlUrl"]
        ET.SubElement(node, "enclosure", {
            "url": item["url"], "length": str(item["length"]), "type": "application/octet-stream",
            f"{{{SPARKLE}}}edSignature": item["edSignature"],
        })
    ET.indent(rss)
    return b'<?xml version="1.0" encoding="utf-8"?>\n' + ET.tostring(rss, encoding="utf-8") + b"\n"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    item = sub.add_parser("item")
    item.add_argument("--zip", type=Path, required=True)
    item.add_argument("--info-plist", type=Path, required=True)
    item.add_argument("--tag", required=True)
    item.add_argument("--repo", required=True)
    item.add_argument("--signature", required=True)
    item.add_argument("--out", type=Path, required=True)
    feed = sub.add_parser("feed")
    feed.add_argument("--releases", type=Path, required=True, help="JSON list of published releases with items")
    feed.add_argument("--repo", required=True)
    feed.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "item":
            data = make_item(args.zip, args.info_plist, args.tag, args.repo, args.signature)
            args.out.write_text(json.dumps(data, indent=2) + "\n")
        else:
            args.out.write_bytes(build_feed(json.loads(args.releases.read_text()), args.repo))
    except (ValueError, KeyError) as error:
        sys.exit(f"appcast: {error}")


if __name__ == "__main__":
    main()
