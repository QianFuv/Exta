"""Keep CI reports accurate, bounded and free from unrelated step outputs."""

import json
from pathlib import Path
import tempfile
import unittest

from report import metadata, require_success, summary, write_report


class CiReportTests(unittest.TestCase):
    """Exercise artifact evidence rather than snapshotting report formatting."""

    def identity(self, status="success"):
        """Create validated metadata with a deliberately unrelated sensitive output."""
        return metadata({"GITHUB_SHA": "a" * 40, "CI_STAGE": "quality", "CI_PLATFORMS": "independent", "CI_STATUS": status, "CI_CHECKS": json.dumps({"tests": {"outcome": "success", "outputs": {"duration_seconds": "12", "private": "must-not-leak"}}})})

    def test_gate_rejects_each_incomplete_required_stage(self):
        """No skipped or cancelled platform can authorize a successful publication gate."""
        stages = {"frontend": "success", "linux": "success", "windows": "success"}
        require_success(stages)
        for name in stages:
            for status in ["failure", "skipped", "cancelled", ""]:
                with self.assertRaisesRegex(RuntimeError, "all succeed"):
                    require_success({**stages, name: status})
        with self.assertRaises(RuntimeError):
            require_success({})

    def test_hashes_exact_payload_and_omits_other_step_output(self):
        """Selected artifacts retain real bytes, hashes and numeric check duration."""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "payload").write_bytes(b"accepted")
            report = write_report(root, ["payload"], self.identity())
            self.assertEqual(report["files"][0]["bytes"], 8)
            self.assertEqual(len(report["files"][0]["sha256"]), 64)
            self.assertEqual(report["checks"][0]["durationSeconds"], 12)
            self.assertNotIn("must-not-leak", json.dumps(report))
            self.assertIn("CI / quality", summary(report))
            self.assertEqual(len(write_report(root, ["."], self.identity())["files"]), 1)

    def test_missing_success_fails_but_failed_jobs_keep_diagnostics(self):
        """A success cannot be reported for an absent payload; failures remain visible."""
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(RuntimeError, "missing requested"):
                write_report(directory, ["missing"], self.identity())
            report = write_report(directory, ["missing"], self.identity("failure"))
            self.assertEqual(report["missing"], ["missing"])
            self.assertIn("Status: failure", summary(report))
            with self.assertRaisesRegex(RuntimeError, "escaped"):
                write_report(directory, ["../outside"], self.identity())
