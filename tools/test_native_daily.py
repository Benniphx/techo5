"""Exercise actual Git histories: idempotence, preservation and safe failures."""

import importlib.util
import subprocess
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("native_daily", Path(__file__).with_name("native_daily.py"))
daily = importlib.util.module_from_spec(spec)
spec.loader.exec_module(daily)


class StableMergeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.upstream = self.root / "upstream"
        self.upstream.mkdir()
        self.run_git(self.upstream, "init", "-b", "main")
        self.identity(self.upstream)
        (self.upstream / "echod").mkdir()
        self.commit_file(self.upstream, "echod/base", "stable one", "stable one")
        (self.upstream / ".github/workflows").mkdir(parents=True)
        self.commit_file(self.upstream, ".github/workflows/build.yml", "trusted workflow", "workflow")
        self.run_git(self.upstream, "tag", "v1.0.0")
        self.fork = self.root / "fork"
        self.run_git(self.root, "clone", str(self.upstream), str(self.fork))
        self.identity(self.fork)
        self.commit_file(self.fork, "echod/native", "native extension", "native extension")

    def run_git(self, cwd, *args):
        return subprocess.check_output(["git", *args], cwd=cwd, text=True, stderr=subprocess.PIPE).strip()

    def identity(self, repo):
        self.run_git(repo, "config", "user.name", "Test")
        self.run_git(repo, "config", "user.email", "test@example.invalid")

    def commit_file(self, repo, path, content, title):
        (repo / path).write_text(content)
        self.run_git(repo, "add", path)
        self.run_git(repo, "commit", "-m", title)

    def prepare(self, tag="v1.0.0", force=False, published=True, run_number="1"):
        def repo_git(*args):
            return self.run_git(self.fork, *args)
        with patch.object(daily, "git", repo_git), patch.object(daily, "MARKER", self.fork / ".github/native-build.json"):
            return daily.prepare(tag, str(self.upstream), run_number, force=force, published=published)

    def test_failed_release_publication_rebuilds_with_new_ranked_version(self):
        first = self.prepare()
        second = self.prepare(published=False, run_number="2")
        self.assertTrue(second["changed"])
        self.assertEqual(first["version"], "v1.0.1")
        self.assertEqual(second["version"], "v1.0.2")
        self.assertNotEqual(first["commit"], second["commit"])
        self.assertFalse(self.prepare(run_number="3")["changed"])
        self.assertEqual(self.prepare(run_number="4")["version"], "v1.0.2")

    def test_rootfs_or_installer_changes_rebuild_but_readme_does_not(self):
        self.prepare()
        self.commit_file(self.fork, "README.md", "documentation", "docs")
        self.assertFalse(self.prepare()["changed"])
        (self.fork / "tools/linux").mkdir(parents=True)
        self.commit_file(self.fork, "tools/linux/package.txt", "new package", "rootfs input")
        self.assertTrue(self.prepare()["changed"])
        self.assertFalse(self.prepare()["changed"])

    def test_release_probe_recovers_missing_release_without_hiding_api_failure(self):
        self.assertFalse(daily.release_published("v0.9.30_native.1"))
        for status in (404, 403, 500):
            error = urllib.error.HTTPError("https://api.github.com", status, "test", {}, None)
            with patch.object(daily.urllib.request, "urlopen", side_effect=error):
                if status == 404:
                    self.assertFalse(daily.release_published("v1.0.1"))
                else:
                    with self.assertRaises(urllib.error.HTTPError):
                        daily.release_published("v1.0.1")
            error.close()

    def test_explicit_rebuild_creates_bundleable_history_without_changing_source(self):
        first = self.prepare()
        rebuilt = self.prepare(force=True)
        self.assertTrue(rebuilt["changed"])
        self.assertNotEqual(first["commit"], rebuilt["commit"])
        self.assertEqual(first["source_digest"], rebuilt["source_digest"])
        self.run_git(self.fork, "bundle", "create", str(self.root / "candidate.bundle"), "HEAD", "^" + rebuilt["base"])
        self.assertFalse(self.prepare()["changed"])

    def test_first_build_then_skip_without_new_release(self):
        first = self.prepare()
        self.assertTrue(first["changed"])
        head = self.run_git(self.fork, "rev-parse", "HEAD")
        second = self.prepare()
        self.assertFalse(second["changed"])
        self.assertEqual(head, second["commit"])
        self.assertEqual("native extension", (self.fork / "echod/native").read_text())

    def test_new_release_preserves_native_code_and_builds_again(self):
        self.prepare()
        self.commit_file(self.upstream, "echod/base", "stable two", "stable two")
        self.run_git(self.upstream, "tag", "v1.0.1")
        result = self.prepare("v1.0.1")
        self.assertTrue(result["changed"])
        self.assertEqual("stable two", (self.fork / "echod/base").read_text())
        self.assertEqual("native extension", (self.fork / "echod/native").read_text())
        self.assertFalse(self.prepare("v1.0.1")["changed"])

    def test_conflict_keeps_previous_success_marker(self):
        self.prepare()
        marker = (self.fork / ".github/native-build.json").read_bytes()
        self.commit_file(self.fork, "echod/base", "fork edit", "fork edit")
        self.commit_file(self.upstream, "echod/base", "upstream edit", "upstream edit")
        self.run_git(self.upstream, "tag", "v1.0.1")
        with self.assertRaises(subprocess.CalledProcessError):
            self.prepare("v1.0.1")
        self.assertEqual(marker, (self.fork / ".github/native-build.json").read_bytes())

    def test_upstream_workflow_changes_cannot_change_fork_ci(self):
        self.prepare()
        self.commit_file(self.upstream, ".github/workflows/build.yml", "changed workflow", "upstream CI")
        self.commit_file(self.upstream, ".github/workflows/new.yml", "new workflow", "new upstream CI")
        self.run_git(self.upstream, "tag", "v1.0.1")
        self.assertTrue(self.prepare("v1.0.1")["changed"])
        self.assertEqual("trusted workflow", (self.fork / ".github/workflows/build.yml").read_text())
        self.assertFalse((self.fork / ".github/workflows/new.yml").exists())

    def test_dirty_checkout_is_rejected(self):
        (self.fork / "echod/native").write_text("user edit")
        with self.assertRaisesRegex(ValueError, "clean"):
            self.prepare()

    def test_non_stable_or_shell_input_is_rejected(self):
        for tag in ("v1.0.0-rc.1", "dot-v1.0.0", "v1.0.0;echo bad"):
            with self.assertRaises(ValueError):
                self.prepare(tag)

    def test_retagged_release_is_rejected(self):
        self.prepare()
        self.commit_file(self.upstream, "echod/base", "retag", "retag")
        self.run_git(self.upstream, "tag", "-f", "v1.0.0")
        # Fresh Actions runners have no cached custom ref.
        self.run_git(self.fork, "update-ref", "-d", "refs/native-upstream/v1.0.0")
        with self.assertRaisesRegex(ValueError, "retagged"):
            self.prepare()


if __name__ == "__main__":
    unittest.main()
