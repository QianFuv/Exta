"""Record common CI stage summaries and selected artifact provenance."""

import hashlib
import json
import os
from pathlib import Path
import re
import sys


def require_success(results):
    """Reject failed, skipped, cancelled or missing required workflow stages."""
    if not results or any(value != "success" for value in results.values()):
        raise RuntimeError("Required verification stages did not all succeed")


def metadata(environment):
    """Keep only source, platform and check outcomes from the CI environment."""
    source = environment.get("CI_SOURCE_SHA", environment.get("GITHUB_SHA", ""))
    stage = environment["CI_STAGE"]
    status = environment["CI_STATUS"]
    platforms = environment["CI_PLATFORMS"].split(",")
    if not re.fullmatch(r"[0-9a-f]{40}", source) or stage not in ["detect-changes", "quality", "build-acceptance", "publish"]:
        raise RuntimeError("Invalid CI source or stage")
    if status not in ["success", "failure", "cancelled", "skipped"] or not platforms or any(item not in ["linux/amd64", "windows/amd64", "independent"] for item in platforms):
        raise RuntimeError("Invalid CI status or platforms")
    checks = []
    for name, check in json.loads(environment.get("CI_CHECKS", "{}")).items():
        duration = check.get("outputs", {}).get("duration_seconds", "")
        checks.append({"name": name, "status": check.get("outcome", check.get("result", "skipped")), "durationSeconds": int(duration) if re.fullmatch(r"\d+", str(duration)) else None})
    return {"schema": 1, "stage": stage, "sourceSha": source, "platforms": platforms, "status": status, "checks": checks}


def write_report(root, selected, identity):
    """Hash selected regular files while refusing links and escaped paths.

    Args:
        root: Dedicated artifact/report directory.
        selected: Relative payload files or directories.
        identity: Validated source and stage metadata.

    Returns:
        Written artifact inventory, including missing payload evidence.
    """
    root = Path(root).absolute()
    files = {}
    missing = []

    def visit(path):
        """Walk one contained artifact without following symbolic links."""
        relative = path.relative_to(root)
        for ancestor in [path, *path.parents]:
            if ancestor.is_symlink():
                raise RuntimeError("CI artifact report refuses symbolic links")
            if ancestor == root:
                break
        if path.is_dir():
            for child in sorted(path.iterdir()):
                visit(child)
        elif path.name != "ci-manifest.json":
            if not path.is_file():
                raise RuntimeError("CI report requires regular payload files")
            digest = hashlib.sha256()
            with path.open("rb") as stream:
                while block := stream.read(1024 * 1024):
                    digest.update(block)
            files[relative.as_posix()] = {"path": relative.as_posix(), "bytes": path.stat().st_size, "sha256": digest.hexdigest()}

    for name in selected:
        relative = Path(name)
        if relative.is_absolute() or ".." in relative.parts:
            raise RuntimeError("CI report path escaped artifact root")
        path = root / relative
        if path.is_symlink():
            raise RuntimeError("CI artifact report refuses symbolic links")
        if path.exists():
            count = len(files)
            visit(path)
            if count == len(files):
                missing.append(name)
        else:
            missing.append(name)
    report = {**identity, "files": [files[name] for name in sorted(files)], "missing": missing}
    root.mkdir(parents=True, exist_ok=True)
    output = root / "ci-manifest.json"
    if output.is_symlink():
        raise RuntimeError("CI report output cannot be a symbolic link")
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if selected and identity["status"] == "success" and (missing or not files):
        raise RuntimeError("Successful CI artifact is missing requested payloads")
    return report


def summary(report):
    """Render the same source/platform/check and artifact tables for each stage."""
    lines = [f"## CI / {report['stage']}", "", f"- Status: {report['status']}", f"- Source: `{report['sourceSha']}`", f"- Platforms: {', '.join(report['platforms'])}", "", "| Check | Status | Duration (s) |", "|---|---|---|"]
    for check in report["checks"]:
        duration = check["durationSeconds"] if check["durationSeconds"] is not None else "—"
        lines.append(f"| {check['name']} | {check['status']} | {duration} |")
    if report.get("files"):
        lines += ["", "| Artifact | Bytes | SHA-256 |", "|---|---:|---|"]
        lines.extend(f"| {item['path']} | {item['bytes']} | `{item['sha256']}` |" for item in report["files"])
    if report.get("missing"):
        lines += ["", "Missing payloads: " + ", ".join(report["missing"])]
    return "\n".join(lines) + "\n"


def main():
    """Write an inventory or append a summary for this task-owned workflow stage."""
    if sys.argv[1] == "gate":
        require_success(json.loads(os.environ["CI_GATE_RESULTS"]))
        return
    identity = metadata(os.environ)
    root = Path(os.environ.get("CI_REPORT_ROOT", "artifacts/ci"))
    if sys.argv[1] == "manifest":
        report = write_report(root, sys.argv[2:], identity)
    elif sys.argv[1] == "summary":
        manifest = root / "ci-manifest.json"
        report = json.loads(manifest.read_text(encoding="utf-8")) if manifest.is_file() else {**identity, "files": [], "missing": []}
        report.update(identity)
    else:
        raise RuntimeError("Expected manifest or summary command")
    if sys.argv[1] == "summary":
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as output:
            output.write(summary(report))


if __name__ == "__main__":
    main()
