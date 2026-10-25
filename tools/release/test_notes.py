"""Verify complete release notes independently of GitHub and local tag state."""

import unittest

from notes import generate_notes, parse_version

SOURCE = "a" * 40
BASE = "b" * 40
OTHER = "c" * 40


class ReleaseNotesTests(unittest.TestCase):
    """Prevent missing commits, misleading bases and unsafe Markdown subjects."""

    def git(self, *args):
        """Supply full immutable history including a malicious commit subject."""
        if args[0] == "rev-parse":
            return "false"
        if args[0] == "rev-list":
            return f"{SOURCE}\n{BASE}"
        self.assertEqual(args[-2], f"{BASE}..{SOURCE}")
        return f"{SOURCE}\0feat(ui): add <script> [view]\n{OTHER}\0fix: preserve *all* commits"

    def test_complete_categories_escape_and_api_resolved_base(self):
        """Missing local tags must not turn a later release into a first release."""
        calls = []

        def request(path):
            """Return an unrelated newer release before a reachable older release."""
            calls.append(path)
            if path.startswith("releases?"):
                return [{"tag_name": tag, "draft": False, "prerelease": False} for tag in ["v0.1.9", "v0.1.4", "v0.1.3"]]
            return {"sha": BASE if path.endswith("v0.1.4") else OTHER}

        notes = generate_notes("0.1.5", SOURCE, request, self.git)
        self.assertIn("## Commits (2)\n\nChanges since v0.1.4.", notes)
        self.assertIn("### Features (1)", notes)
        self.assertIn("### Bug fixes (1)", notes)
        self.assertIn("&lt;script&gt; \\[view\\]", notes)
        self.assertIn("\\*all\\*", notes)
        self.assertEqual(notes.count("/commit/"), 2)
        self.assertIn("/compare/v0.1.4...v0.1.5", notes)
        self.assertNotIn("commits/v0.1.9", calls)

    def test_paginates_and_ignores_drafts_prereleases_and_unreachable_tags(self):
        """Only an older published stable ancestor can define the range."""
        releases = [{"tag_name": "v0.1.4", "draft": True, "prerelease": False}] * 100
        calls = []

        def request(path):
            """Exercise a second release page without a matching local tag."""
            calls.append(path)
            if path.endswith("page=1"):
                return releases
            if path.endswith("page=2"):
                return [{"tag_name": "v0.1.3", "draft": False, "prerelease": False}]
            return {"sha": BASE}

        notes = generate_notes("0.1.5", SOURCE, request, self.git)
        self.assertIn("Changes since v0.1.3.", notes)
        self.assertIn("releases?per_page=100&page=2", calls)

    def test_shallow_history_fails_before_api_calls(self):
        """Incomplete history cannot silently produce incomplete release notes."""
        def forbidden(path):
            """Reject unexpected API access."""
            self.fail(path)

        with self.assertRaisesRegex(RuntimeError, "full Git history"):
            generate_notes("0.1.5", SOURCE, forbidden, lambda *args: "true")

    def test_first_release_and_oversize_are_explicit(self):
        """First releases include all history; oversized bodies fail rather than truncate."""
        def history(*args):
            """Supply a full first-release history with a large subject."""
            if args[0] == "rev-parse":
                return "false"
            if args[0] == "rev-list":
                return SOURCE
            return f"{SOURCE}\0" + "x" * 125000

        with self.assertRaisesRegex(RuntimeError, "body limit"):
            generate_notes("0.1.5", SOURCE, lambda path: [], history)
        for value in ["v0.1.5", "0.01.5", "0.1.5-rc1", "0.1", "1.0.0\n"]:
            with self.assertRaises(RuntimeError):
                parse_version(value)
        self.assertGreater(parse_version("0.1.10"), parse_version("0.1.9"))
