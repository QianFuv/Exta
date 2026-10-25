"""Generate complete categorized notes from reachable published release history."""

import html
from pathlib import Path
import re
import subprocess

CATEGORIES = {
    "feat": "Features", "fix": "Bug fixes", "perf": "Performance",
    "refactor": "Refactoring", "test": "Tests", "build": "Build", "ci": "CI",
    "docs": "Documentation", "style": "Style", "chore": "Maintenance",
    "revert": "Reverts", "other": "Other commits",
}


def parse_version(value):
    """Parse a canonical stable version without lexical ordering errors."""
    if not re.fullmatch(r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)", value):
        raise RuntimeError("Expected a stable MAJOR.MINOR.PATCH version")
    return tuple(int(part) for part in value.split("."))


def read_git(*args):
    """Read the complete checked-out history without mutating Git state."""
    return subprocess.check_output(
        ["git", *args], cwd=Path(__file__).resolve().parents[2],
        text=True, encoding="utf-8", timeout=60,
    ).strip()


def generate_notes(version, source, request, git=read_git):
    """Build notes for all commits since the highest reachable earlier release.

    Args:
        version: Canonical target stable version.
        source: Exact accepted full commit SHA.
        request: Read-only repository API callback.
        git: Read-only Git callback, injectable for offline regression tests.

    Returns:
        Complete Markdown release notes.
    """
    current = parse_version(version)
    if not re.fullmatch(r"[0-9a-f]{40}", source):
        raise RuntimeError("Release notes require a full source SHA")
    if git("rev-parse", "--is-shallow-repository") != "false":
        raise RuntimeError("Release notes require full Git history")
    reachable = set(git("rev-list", source).splitlines())
    releases = []
    for page in range(1, 101):
        batch = request(f"releases?per_page=100&page={page}")
        if not isinstance(batch, list):
            raise RuntimeError("Cannot list published releases")
        releases.extend(batch)
        if len(batch) < 100:
            break
    else:
        raise RuntimeError("Release history pagination exceeded its bound")
    candidates = []
    for release in releases:
        tag = release["tag_name"]
        if release["draft"] or release["prerelease"] or not re.fullmatch(r"v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)", tag):
            continue
        if parse_version(tag[1:]) < current:
            candidates.append(release)
    previous = None
    base = None
    for release in sorted(candidates, key=lambda item: parse_version(item["tag_name"][1:]), reverse=True):
        commit = request(f"commits/{release['tag_name']}")["sha"]
        if commit in reachable:
            previous, base = release["tag_name"], commit
            break
    selected = f"{base}..{source}" if base else source
    log = git("log", "--reverse", "--format=%H%x00%s", selected, "--")
    groups = {category: [] for category in CATEGORIES}
    commits = log.splitlines() if log else []
    repository_url = "https://github.com/QianFuv/Exta"
    for line in commits:
        commit, separator, subject = line.partition("\0")
        if not separator or not re.fullmatch(r"[0-9a-f]{40}", commit):
            raise RuntimeError("Invalid commit history entry")
        match = re.match(r"([a-z]+)(?:\([^\r\n)]+\))?!?:\s+", subject)
        category = match.group(1) if match and match.group(1) in groups else "other"
        escaped = re.sub(r"([\\`*_{}\[\]()#!|~])", r"\\\1", html.escape(subject, quote=False))
        groups[category].append(f"- {escaped} ([{commit[:7]}]({repository_url}/commit/{commit}))")
    sections = [f"## Commits ({len(commits)})", f"Changes since {previous}." if previous else "All commits in this first stable release."]
    for category, entries in groups.items():
        if entries:
            sections.append(f"### {CATEGORIES[category]} ({len(entries)})\n\n" + "\n".join(entries))
    comparison = f"{previous}...v{version}" if previous else f"v{version}"
    path = f"compare/{comparison}" if previous else f"commits/{comparison}"
    sections.append(f"**Full changelog:** [{comparison}]({repository_url}/{path})")
    notes = "\n\n".join(sections) + "\n"
    if len(notes.encode("utf-8")) > 125000:
        raise RuntimeError("Complete commit list exceeds the release body limit")
    return notes
