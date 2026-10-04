#!/usr/bin/env python3
"""Prepare a tested stable-release merge; never install firmware or force-push."""

import argparse
import hashlib
import json
import os
import re
import subprocess
import urllib.request
from pathlib import Path

UPSTREAM = "HuskerMinion/techo5"
MARKER = Path(".github/native-build.json")


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def source_digest() -> str:
    return hashlib.sha256(git("ls-tree", "-r", "HEAD", "--", "echod").encode()).hexdigest()


def prepare(tag: str, fetch_url: str, run_number: str) -> dict:
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        raise ValueError("Latest release must be a stable Show vX.Y.Z tag")
    if not re.fullmatch(r"[0-9]+", run_number):
        raise ValueError("Invalid run number")
    if git("status", "--porcelain"):
        raise ValueError("Candidate checkout must be clean")
    base = git("rev-parse", "HEAD")
    ref = f"refs/native-upstream/{tag}"
    git("fetch", "--no-tags", fetch_url, f"refs/tags/{tag}:{ref}")
    upstream_commit = git("rev-parse", f"{ref}^{{commit}}")
    old = json.loads(MARKER.read_text()) if MARKER.exists() else {}
    # Never follow an older release or silently accept a retagged release.
    previous = old.get("upstream_commit")
    if previous:
        if old.get("release_tag") == tag and previous != upstream_commit:
            raise ValueError("Upstream retagged the last built release; manual review required")
        git("merge-base", "--is-ancestor", previous, upstream_commit)
    git("merge", "--no-commit", "--no-ff", upstream_commit)
    # CI permissions, schedules and fork publishing stay under our control.
    # GITHUB_TOKEN cannot promote upstream edits to workflow files.
    git("restore", f"--source={base}", "--staged", "--worktree", "--", ".github/workflows")
    if (Path(git("rev-parse", "--absolute-git-dir")) / "MERGE_HEAD").exists():
        git("commit", "-m", f"chore: merge stable {tag}")
    marker = {"release_tag": tag, "upstream_commit": upstream_commit, "source_digest": source_digest()}
    changed = old != marker
    if changed:
        MARKER.parent.mkdir(exist_ok=True)
        MARKER.write_text(json.dumps(marker, indent=2) + "\n")
        git("add", str(MARKER))
        git("commit", "-m", f"chore: track native build for {tag}")
    elif git("rev-parse", "HEAD") != base:
        # An unexpected merge without a changed marker must not be silently published.
        raise ValueError("Unexpected upstream history change requires manual review")
    return {**marker, "changed": changed, "base": base, "commit": git("rev-parse", "HEAD"),
            "version": f"{tag}_native.{run_number}"}


def latest_release() -> str:
    request = urllib.request.Request(f"https://api.github.com/repos/{UPSTREAM}/releases/latest",
                                     headers={"Accept": "application/vnd.github+json", "User-Agent": "techo5-native-daily"})
    token = os.environ.get("GH_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(request, timeout=30) as response:
        release = json.load(response)
    if release.get("draft") or release.get("prerelease"):
        raise ValueError("Latest release is not stable")
    return release["tag_name"]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run-number", required=True)
    args = parser.parse_args()
    result = prepare(latest_release(), f"https://github.com/{UPSTREAM}.git", args.run_number)
    output = os.environ.get("GITHUB_OUTPUT")
    if output:
        with open(output, "a", encoding="utf-8") as stream:
            for key in ("changed", "base", "commit", "version"):
                stream.write(f"{key}={str(result[key]).lower()}\n")
    if result["changed"]:
        Path("bin").mkdir(exist_ok=True)
        Path("bin/build-info.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
