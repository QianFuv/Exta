"""Exercise conflicting identities and partial publication without external writes."""

import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import publish


class PublicationSafetyTests(unittest.TestCase):
    """Keep retries from replacing tags, images or accepted release assets."""

    def test_tag_peels_annotations_and_rejects_other_source(self):
        """A pre-existing tag must identify the accepted commit, including annotations."""
        ref = [{"ref": "refs/tags/v0.1.2", "object": {"type": "tag", "sha": "annotation"}}]
        with patch.object(publish, "api", side_effect=[ref, {"object": {"type": "commit", "sha": "accepted"}}]):
            self.assertTrue(publish.verify_tag("accepted"))
        with patch.object(publish, "api", return_value=[{
            "ref": "refs/tags/v0.1.2", "object": {"type": "commit", "sha": "other"},
        }]):
            with self.assertRaisesRegex(RuntimeError, "conflicts"):
                publish.verify_tag("accepted")

    def test_wrong_release_version_is_rejected_before_external_operations(self):
        """The new patch must reject artifacts from the previous published version."""
        with tempfile.TemporaryDirectory() as directory:
            candidate = Path(directory)
            (candidate / "release-manifest.json").write_text(json.dumps({
                "head": "accepted", "version": "0.1.1", "artifacts": [],
            }))
            with patch.object(publish, "command") as external:
                with self.assertRaisesRegex(RuntimeError, "source/version mismatch"):
                    publish.publish(candidate, "accepted")
                external.assert_not_called()

    def test_registry_absence_does_not_include_authentication_failure(self):
        """Only an explicit missing manifest permits a first push."""
        for error in ["unauthorized", "403 Forbidden", "connection timeout"]:
            with patch.object(subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", error)), patch.object(publish, "package_absent", return_value=False):
                with self.assertRaisesRegex(RuntimeError, "absence is not established"):
                    publish.remote_image("image:tag", "expected")
        with patch.object(subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "ERROR: image:tag: not found\n")):
            self.assertIsNone(publish.remote_image("image:tag", "expected"))

    def test_existing_image_must_match_tested_config(self):
        """A mutable version tag cannot silently replace a different image."""
        with patch.object(subprocess, "run", return_value=subprocess.CompletedProcess(
            [], 0, json.dumps({"config": {"digest": "other"}}), "",
        )):
            with self.assertRaisesRegex(RuntimeError, "conflicts"):
                publish.remote_image("image:tag", "expected")

    def test_initial_package_requires_authenticated_not_found(self):
        """Registry denial alone cannot authorize a new package push."""
        with patch.object(subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "gh: Not Found (HTTP 404)")):
            self.assertTrue(publish.package_absent())
        for message in ["gh: Forbidden (HTTP 403)", "gh: Unauthorized (HTTP 401)", "timeout"]:
            with patch.object(subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", message)):
                with self.assertRaisesRegex(RuntimeError, "could not be established"):
                    publish.package_absent()

    def test_publish_resumes_interrupted_draft_upload(self):
        """A failed upload is cleaned only on a matching draft and finalized after readback."""
        with tempfile.TemporaryDirectory() as directory:
            candidate = Path(directory)
            names = ["exta-windows-amd64.exe.zip", "exta-linux-amd64.zip", "SHA256SUMS",
                     "dependency-inventory.json", "RELEASE-NOTES.txt"]
            for name in names:
                (candidate / name).write_bytes(name.encode())
            (candidate / "release-manifest.json").write_text(json.dumps({
                "head": "accepted", "version": "0.1.2",
                "artifacts": [{"target": "windows/amd64"}, {"target": "linux/amd64"}],
            }))
            names += ["release-manifest.json", "container-digest.txt"]
            release = {"draft": True, "assets": [{"id": 1, "name": names[0], "state": "starter"}]}
            deleted = []
            uploaded = []

            def fake_command(*args):
                """Model interrupted upload state and assert publication order."""
                if args[:3] == ("docker", "image", "inspect"):
                    return "config"
                if args[:3] == ("gh", "release", "download"):
                    name = args[args.index("--pattern") + 1]
                    (Path(args[-1]) / name).write_bytes((candidate / name).read_bytes())
                elif args[:2] == ("gh", "api") and args[-1] == "DELETE":
                    self.assertTrue(release["draft"])
                    deleted.append(args[2])
                    release["assets"] = []
                elif args[:3] == ("gh", "release", "upload"):
                    name = Path(args[4]).name
                    uploaded.append(name)
                    release["assets"].append({"id": len(uploaded) + 1, "name": name, "state": "uploaded"})
                elif args[:3] == ("gh", "release", "edit"):
                    self.assertEqual(set(uploaded), set(names))
                    release["draft"] = False
                else:
                    self.fail(f"Unexpected mutation: {args}")
                return ""

            with patch.object(publish, "verify_tag", return_value=True), \
                    patch.object(publish, "remote_image", return_value="sha256:" + "a" * 64), \
                    patch.object(publish, "release_state", side_effect=lambda: release), \
                    patch.object(publish, "command", side_effect=fake_command):
                publish.publish(candidate, "accepted")
                self.assertEqual(len(deleted), 1)
                self.assertFalse(release["draft"])
                before = len(uploaded)
                publish.publish(candidate, "accepted")
                self.assertEqual(len(uploaded), before)

    def test_draft_resumes_only_missing_assets_after_hash_readback(self):
        """Partial drafts resume while conflicting and incomplete public releases fail."""
        with tempfile.TemporaryDirectory() as directory:
            first = Path(directory) / "first.zip"
            second = Path(directory) / "second.zip"
            first.write_bytes(b"accepted")
            second.write_bytes(b"second")
            release = {"draft": True, "assets": [{"name": first.name, "state": "uploaded"}]}

            def download(*args):
                """Simulate a downloaded existing asset without accessing GitHub."""
                (Path(args[-1]) / first.name).write_bytes(b"accepted")

            with patch.object(publish, "command", side_effect=download):
                self.assertEqual(publish.verify_assets(release, [first, second]), [second])
                release["draft"] = False
                with self.assertRaisesRegex(RuntimeError, "incomplete"):
                    publish.verify_assets(release, [first, second])
                release["draft"] = True
                first.write_bytes(b"changed")
                with self.assertRaisesRegex(RuntimeError, "differs"):
                    publish.verify_assets(release, [first, second])


if __name__ == "__main__":
    unittest.main()
