"""Publish accepted artifacts without replacing conflicting release identities."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

from notes import generate_notes, parse_version

REPOSITORY = "QianFuv/Exta"
IMAGE = "ghcr.io/qianfuv/exta"
VERSION = (Path(__file__).resolve().parents[2] / "VERSION").read_text(encoding="utf-8").strip()
parse_version(VERSION)
TAG = f"v{VERSION}"


def command(*args):
    """Run a bounded public CLI operation and retain output for validation."""
    result = subprocess.run(args, capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise RuntimeError(f"{args[0]} operation failed: {result.stderr.strip()}")
    return result.stdout


def api(path, *args):
    """Read or update the explicitly authorized repository through GitHub CLI."""
    return json.loads(command("gh", "api", f"repos/{REPOSITORY}/{path}", *args))


def verify_tag(source):
    """Reject an existing lightweight or annotated tag pointing elsewhere."""
    refs = api(f"git/matching-refs/tags/{TAG}")
    exact = [ref for ref in refs if ref["ref"] == f"refs/tags/{TAG}"]
    if not exact:
        return False
    value = exact[0]["object"]
    for _ in range(8):
        if value["type"] == "commit":
            if value["sha"] != source:
                raise RuntimeError("Existing release tag conflicts with accepted source")
            return True
        if value["type"] != "tag":
            break
        value = api(f"git/tags/{value['sha']}")["object"]
    raise RuntimeError("Release tag does not resolve to an accepted commit")


def remote_image(reference, expected_config):
    """Accept only the exact tested image config; distinguish absence from errors."""
    result = subprocess.run(
        ["docker", "buildx", "imagetools", "inspect", reference, "--raw"],
        capture_output=True, text=True, timeout=60,
    )
    if result.returncode:
        message = result.stderr.lower()
        if re.search(r"(?:manifest unknown|: not found)\s*$", message):
            return None
        if ("403" in message or "denied" in message or "unauthorized" in message) and package_absent():
            return None
        raise RuntimeError("Registry inspection failed; absence is not established")
    manifest = json.loads(result.stdout)
    if manifest.get("config", {}).get("digest") != expected_config:
        raise RuntimeError(f"Existing container tag conflicts with accepted image: {reference}")
    descriptor = json.loads(command(
        "docker", "buildx", "imagetools", "inspect", reference,
        "--format", "{{json .Manifest}}",
    ))
    digest = descriptor.get("digest", "")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise RuntimeError("Registry did not return a valid manifest digest")
    return digest


def package_absent():
    """Confirm initial package absence through the authenticated GitHub Packages API."""
    result = subprocess.run(
        ["gh", "api", "/users/QianFuv/packages/container/exta"],
        capture_output=True, text=True, timeout=60,
    )
    if result.returncode == 0:
        package = json.loads(result.stdout)
        if package.get("name") != "exta" or package.get("package_type") != "container":
            raise RuntimeError("Unexpected package identity")
        return False
    if "(HTTP 404)" in result.stderr:
        return True
    raise RuntimeError("GitHub package absence could not be established")


def release_state():
    """Find drafts and published releases without interpreting network errors as absence."""
    pages = json.loads(command(
        "gh", "api", f"repos/{REPOSITORY}/releases", "--paginate", "--slurp",
    ))
    matches = [item for page in pages for item in page if item["tag_name"] == TAG]
    if len(matches) > 1:
        raise RuntimeError("Duplicate release identities")
    return matches[0] if matches else None


def digest_file(path):
    """Hash exact release asset bytes."""
    return hashlib.sha256(path.read_bytes()).hexdigest()


def public_assets(candidate):
    """Require exact public archive membership and verified bytes before remote access.

    Args:
        candidate: Directory containing the accepted candidate artifacts.

    Returns:
        The two versioned archives and their public checksum file.
    """
    names = [f"exta_{VERSION}_linux_amd64.tar.gz", f"exta_{VERSION}_windows_amd64.zip"]
    checksums = {}
    for line in (candidate / "SHA256SUMS").read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9_.-]+)", line)
        if not match or match[2] in checksums:
            raise RuntimeError("Invalid or duplicate public checksum entry")
        checksums[match[2]] = match[1]
    if set(checksums) != set(names):
        raise RuntimeError("Public checksum membership differs from the two platform archives")
    for name in names:
        path = candidate / name
        if not path.is_file() or not path.stat().st_size or digest_file(path) != checksums[name]:
            raise RuntimeError(f"Public archive checksum mismatch: {name}")
    return [candidate / name for name in [*names, "SHA256SUMS"]]


def verify_assets(release, assets):
    """Verify existing assets before uploading any missing members of a draft."""
    expected = {path.name: path for path in assets}
    existing = {item["name"]: item for item in release["assets"]}
    if len(existing) != len(release["assets"]) or set(existing) - set(expected):
        raise RuntimeError("Release contains duplicate or unexpected assets")
    incomplete = {name for name, item in existing.items() if item["state"] == "starter"}
    if incomplete and not release["draft"]:
        raise RuntimeError("Published release contains incomplete uploads")
    with tempfile.TemporaryDirectory(prefix="exta-release-check-") as directory:
        for name in existing.keys() - incomplete:
            if existing[name]["state"] != "uploaded":
                raise RuntimeError("Unknown release upload state")
            command("gh", "release", "download", TAG, "--repo", REPOSITORY,
                    "--pattern", name, "--dir", directory)
            if digest_file(Path(directory) / name) != digest_file(expected[name]):
                raise RuntimeError(f"Existing release asset differs: {name}")
    missing = [expected[name] for name in (expected.keys() - existing.keys()) | incomplete]
    if missing and not release["draft"]:
        raise RuntimeError("Published release is incomplete; refusing mutation")
    return missing


def publish(candidate, source):
    """Resume only matching drafts/tags, then publish after complete readback."""
    candidate = candidate.resolve()
    manifest = json.loads((candidate / "release-manifest.json").read_text())
    if manifest["head"] != source or manifest["version"] != VERSION:
        raise RuntimeError("Candidate source/version mismatch")
    if sorted(item["target"] for item in manifest["artifacts"]) != ["linux/amd64", "windows/amd64"]:
        raise RuntimeError("Candidate target mismatch")
    assets = public_assets(candidate)
    for item in manifest["artifacts"]:
        extension = "tar.gz" if item["target"] == "linux/amd64" else "zip"
        expected = f"exta_{VERSION}_{item['target'].replace('/', '_')}.{extension}"
        if item["archive"] != expected or item["archive_sha256"] != digest_file(candidate / expected):
            raise RuntimeError("Manifest archive identity mismatch")
    if api("git/ref/heads/main")["object"]["sha"] != source:
        raise RuntimeError("Remote main differs from accepted source")
    notes_text = generate_notes(VERSION, source, api)
    notes = candidate / "release-notes.md"
    notes.write_text(notes_text, encoding="utf-8")
    has_tag = verify_tag(source)
    local = f"exta-candidate:{source}"
    config = command("docker", "image", "inspect", local, "--format", "{{.Id}}").strip()
    references = [f"{IMAGE}:sha-{source}", f"{IMAGE}:{VERSION}"]
    known = {reference: remote_image(reference, config) for reference in references}
    present = {value for value in known.values() if value}
    if len(present) > 1:
        raise RuntimeError("Container tags disagree on manifest identity")
    release = release_state()
    digest_path = candidate / "container-digest.txt"
    if release:
        if release["body"] != notes_text or release["name"] != f"Exta {TAG}":
            raise RuntimeError("Existing release notes/title conflict with accepted source")
        if not present:
            raise RuntimeError("Existing release has no matching registry image")
        digest_path.write_text(f"{IMAGE}@{next(iter(present))}\n", encoding="utf-8")
        verify_assets(release, assets)
        if not release["draft"] and (not has_tag or any(value is None for value in known.values())):
            raise RuntimeError("Published release has incomplete tag identities")
    if not has_tag:
        api("git/refs", "--method", "POST", "-f", f"ref=refs/tags/{TAG}", "-f", f"sha={source}")
    for reference in references:
        if known[reference] is None:
            command("docker", "tag", local, reference)
            command("docker", "push", reference)
        known[reference] = remote_image(reference, config)
    if None in known.values() or len(set(known.values())) != 1:
        raise RuntimeError("Published container identities did not converge")
    digest = next(iter(known.values()))
    digest_path.write_text(f"{IMAGE}@{digest}\n", encoding="utf-8")
    if not release:
        command("gh", "release", "create", TAG, "--repo", REPOSITORY, "--verify-tag",
                "--draft", "--title", f"Exta {TAG}", "--notes-file", str(notes))
        release = release_state()
    missing = verify_assets(release, assets)
    for item in release["assets"]:
        if item["state"] == "starter":
            command("gh", "api", f"repos/{REPOSITORY}/releases/assets/{item['id']}", "--method", "DELETE")
    for path in missing:
        command("gh", "release", "upload", TAG, str(path), "--repo", REPOSITORY)
    if verify_assets(release_state(), assets):
        raise RuntimeError("Release asset readback is incomplete")
    verify_tag(source)
    if release["draft"]:
        command("gh", "release", "edit", TAG, "--repo", REPOSITORY, "--draft=false")
    final = release_state()
    if final["draft"] or final["body"] != notes_text or final["name"] != f"Exta {TAG}":
        raise RuntimeError("Public release identity readback mismatch")
    if verify_assets(final, assets):
        raise RuntimeError("Public release asset readback is incomplete")
    verify_tag(source)
    return f"Published {REPOSITORY} {TAG}: {IMAGE}@{digest}"


if __name__ == "__main__":
    if sys.argv[1:] == ["context"]:
        if os.environ.get("GITHUB_REPOSITORY") != REPOSITORY or os.environ.get("GITHUB_REF") != "refs/heads/main":
            raise RuntimeError("Release requires the authorized repository main branch")
        with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
            output.write(f"version={VERSION}\n")
    else:
        print(publish(Path(sys.argv[1]), os.environ["SOURCE_SHA"]))
