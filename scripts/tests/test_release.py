import importlib.util
import json
import plistlib
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from source_export import archive, audit, check_tag

spec = importlib.util.spec_from_file_location("signing", Path(__file__).resolve().parents[1] / "prepare-local-signing.py")
signing = importlib.util.module_from_spec(spec)
spec.loader.exec_module(signing)


class SourceReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "release-manifest.json").write_text(json.dumps({
            "version": 1, "files": ["README.md", "release-manifest.json"],
            "directories": ["macos", "scripts"]}))
        (self.root / "README.md").write_text("Public source\n")
        (self.root / "macos/Resources").mkdir(parents=True)
        (self.root / "macos/Resources/Info.plist").write_bytes(plistlib.dumps({"CFBundleShortVersionString": "0.4.4"}))
        (self.root / "scripts").mkdir()
        script = self.root / "scripts/run.sh"
        script.write_text("#!/bin/sh\nexit 0\n")
        script.chmod(0o755)

    def test_archive_excludes_private_data_and_is_reproducible(self):
        for folder in (".cache", "build", ".local-notes", "macos/.build", "scripts/__pycache__"):
            p = self.root / folder
            p.mkdir(parents=True)
            (p / "private.txt").write_text("private data")
        expected = audit(self.root)
        first = archive(self.root, "v0.4.4", self.root / "dist")
        data = first.read_bytes()
        checksum = first.with_suffix("").with_suffix(".sha256")
        self.assertTrue(checksum.read_text().endswith(f"  {first.name}\n"))
        with tarfile.open(first) as tar:
            self.assertEqual(tar.getnames(), [f"tailmulti-v0.4.4-source/{p.as_posix()}" for p in expected])
            entry = tar.getmember("tailmulti-v0.4.4-source/scripts/run.sh")
            self.assertEqual(entry.mode, 0o755)
            self.assertEqual(entry.uid, 0)
            self.assertEqual(entry.uname, "")
        with self.assertRaises(FileExistsError):
            archive(self.root, "v0.4.4", self.root / "dist")
        first.unlink()
        self.assertEqual(data, archive(self.root, "v0.4.4", self.root / "dist").read_bytes())

    def test_clean_checkout_rejects_local_artifacts(self):
        (self.root / ".cache").mkdir()
        with self.assertRaisesRegex(ValueError, "Local artifact"):
            audit(self.root, clean_checkout=True)

    def test_local_names_are_excluded_only_at_root(self):
        (self.root / "AGENTS.md").write_text("local agent notes")
        nested = self.root / "scripts/build"
        nested.mkdir()
        (nested / "tool.py").write_text("print()\n")
        (self.root / "scripts/AGENTS.md").write_text("nested reference\n")
        files = [p.as_posix() for p in audit(self.root)]
        self.assertNotIn("AGENTS.md", files)
        self.assertIn("scripts/build/tool.py", files)
        self.assertIn("scripts/AGENTS.md", files)
        with self.assertRaisesRegex(ValueError, "Local artifact"):
            audit(self.root, clean_checkout=True)

    def test_secret_is_rejected_without_printing_value(self):
        secret = "tskey-" + "auth-" + "a" * 30
        (self.root / "scripts/secret.txt").write_text(secret)
        with self.assertRaises(ValueError) as error:
            audit(self.root)
        self.assertNotIn(secret, str(error.exception))
        self.assertIn("credential", str(error.exception))

    def test_symlink_escape_and_unknown_top_level_are_rejected(self):
        link = self.root / "scripts/link"
        link.symlink_to(self.root.parent)
        with self.assertRaisesRegex(ValueError, "symlink"):
            audit(self.root)
        link.unlink()
        (self.root / "private-notes.txt").write_text("notes")
        with self.assertRaisesRegex(ValueError, "unlisted"):
            audit(self.root)

    def test_signing_material_is_rejected(self):
        (self.root / "scripts/certificate.p12").write_text("not a real certificate")
        with self.assertRaisesRegex(ValueError, "signing file"):
            audit(self.root)

    def test_release_tag_must_match_source_version(self):
        check_tag(self.root, "v0.4.4-rc.1")
        for tag in ("v0.4.5", "../../private", "v0.4.4;echo bad", "v01.4.4", "v0.4.4-", "main"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                check_tag(self.root, tag)


class SigningSelectionTests(unittest.TestCase):
    def test_uses_only_available_development_identity(self):
        a, b = "A" * 40, "B" * 40
        self.assertEqual(signing.choose({a}, "", ""), a)
        self.assertEqual(signing.choose({a, b}, b.lower(), a), b)
        self.assertEqual(signing.choose({a, b}, "", a), a)
        for identities, explicit, saved in ((set(), "", ""), ({a, b}, "", ""), ({a}, b, ""), ({a}, "-", "")):
            with self.subTest(identities=identities), self.assertRaises(ValueError):
                signing.choose(identities, explicit, saved)


if __name__ == "__main__":
    unittest.main()
