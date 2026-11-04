import base64
import plistlib
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from appcast import SPARKLE, build_feed, make_item

REPO = "albertilagan/tailmulti"
SIGNATURE = base64.b64encode(bytes(64)).decode()


class AppcastTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.zip = self.root / "Tailmulti-v0.6.0-macOS.zip"
        self.zip.write_bytes(b"zip bytes")
        self.plist = self.root / "Info.plist"
        self.plist.write_bytes(plistlib.dumps({"CFBundleShortVersionString": "0.6.0", "CFBundleVersion": "15",
                                               "LSMinimumSystemVersion": "26.0"}))

    def item(self, tag="v0.6.0"):
        return make_item(self.zip, self.plist, tag, REPO, SIGNATURE)

    def test_item_describes_the_signed_zip(self):
        item = self.item()
        self.assertEqual(item["url"], f"https://github.com/{REPO}/releases/download/v0.6.0/Tailmulti-v0.6.0-macOS.zip")
        self.assertEqual((item["version"], item["shortVersion"], item["length"]), ("15", "0.6.0", 9))

    def test_item_rejects_mismatched_versions_and_bad_signatures(self):
        with self.assertRaises(ValueError):
            self.item("v0.5.9")
        with self.assertRaises(ValueError):
            make_item(self.zip, self.plist, "v0.6.0", REPO, base64.b64encode(b"short").decode())
        with self.assertRaises(ValueError):
            make_item(self.zip, self.plist, "v0.6.0", "../evil", SIGNATURE)

    def test_feed_orders_by_build_and_signs_each_enclosure(self):
        newer = dict(self.item(), version="16", shortVersion="0.6.1", tag="v0.6.1",
                     url=f"https://github.com/{REPO}/releases/download/v0.6.1/Tailmulti-v0.6.1-macOS.zip")
        feed = build_feed([
            {"item": self.item(), "publishedAt": "2026-11-04T03:00:00Z", "htmlUrl": f"https://github.com/{REPO}/releases/tag/v0.6.0"},
            {"item": newer, "publishedAt": "2026-11-05T03:00:00Z", "htmlUrl": f"https://github.com/{REPO}/releases/tag/v0.6.1"},
        ], REPO)
        items = ET.fromstring(feed).findall("channel/item")
        self.assertEqual([i.findtext(f"{{{SPARKLE}}}version") for i in items], ["16", "15"])
        enclosure = items[1].find("enclosure")
        self.assertEqual(enclosure.get(f"{{{SPARKLE}}}edSignature"), SIGNATURE)
        self.assertEqual(enclosure.get("length"), "9")

    def test_feed_rejects_items_pointing_outside_the_repository(self):
        foreign = dict(self.item(), url="https://example.com/Tailmulti.zip")
        with self.assertRaises(ValueError):
            build_feed([{"item": foreign, "publishedAt": "2026-11-04T03:00:00Z", "htmlUrl": ""}], REPO)


if __name__ == "__main__":
    unittest.main()
