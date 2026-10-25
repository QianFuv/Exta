"""Protect release authorization, exact artifact handoff and mandatory platform gates."""

from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class WorkflowContractTests(unittest.TestCase):
    """Check safety boundaries that a syntactically valid workflow could still lose."""

    def workflow(self, name):
        """Read a committed-source workflow from the repository root."""
        return (ROOT / ".github/workflows" / name).read_text(encoding="utf-8")

    def test_publication_is_manual_main_only_and_write_permissions_are_scoped(self):
        """Routine CI and untrusted branches must never acquire publishing permissions."""
        release = self.workflow("release.yml")
        self.assertIn("  workflow_dispatch:", release)
        self.assertNotRegex(release, r"(?m)^  (?:push|pull_request|schedule):")
        self.assertIn("github.repository == 'QianFuv/Exta' && github.ref == 'refs/heads/main'", release)
        self.assertIn("cancel-in-progress: false", release)
        self.assertNotIn("--clobber", release)
        self.assertNotIn("--force", release)
        for name in ["ci.yml", "frontend.yml", "backend.yml"]:
            self.assertNotRegex(self.workflow(name), r"(?:contents|packages): write")
        self.assertEqual(release.count("contents: write"), 1)
        self.assertEqual(release.count("packages: write"), 1)
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            text = path.read_text(encoding="utf-8")
            for block in re.split(r"(?=      - )", text):
                if "actions/checkout@" in block:
                    self.assertIn("persist-credentials: false", block)
            for action in re.findall(r"uses: ([^\s]+)", text):
                if not action.startswith("./"):
                    self.assertRegex(action, r"@[0-9a-f]{40}$")

    def test_exact_artifact_identity_flows_through_both_platforms_and_publication(self):
        """A same-name artifact must not substitute different frontend or candidate bytes."""
        frontend = self.workflow("frontend.yml")
        backend = self.workflow("backend.yml")
        release = self.workflow("release.yml")
        self.assertEqual(frontend.count("npm run build --prefix web"), 1)
        self.assertIn("steps.export_ui.outputs.artifact-id", frontend)
        self.assertIn("artifact-ids: ${{ inputs.ui_artifact_id }}", backend)
        self.assertIn("artifact-ids: ${{ needs.linux.outputs.windows_artifact_id }}", backend)
        self.assertIn("$bundle -ne $env:UI_SHA256", backend)
        self.assertIn("artifact-ids: ${{ needs.verify.outputs.candidate_artifact_id }}", release)
        self.assertIn("sha256sum --check candidate-SHA256SUMS", release)
        self.assertIn("sha256sum --check container-SHA256SUMS", release)

    def test_existing_runtime_gates_and_always_aggregate_are_retained(self):
        """Both native/browser lanes, race and container/proxy acceptance remain required."""
        backend = self.workflow("backend.yml")
        self.assertIn("go test -race", backend)
        self.assertEqual(backend.count("-run '^TestPackagedRuntime$'"), 2)
        self.assertEqual(backend.count("npm run test:e2e --prefix web"), 2)
        self.assertIn("bash tools/release/container-smoke.sh", backend)
        self.assertIn("proxy-container-smoke.sh", (ROOT / "tools/release/container-smoke.sh").read_text(encoding="utf-8"))
        self.assertIn("needs: [linux, windows]\n    if: always()", backend)
        self.assertIn("needs: [changes, frontend, backend]\n    if: always()", self.workflow("ci.yml"))
        self.assertIn("report.py gate", backend)
