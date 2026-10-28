"""Exercise actual Git histories: idempotence, preservation and safe failures."""

import importlib.util
import json
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
        self.assertEqual(first["version"], "v1.0.0-realtime.1")
        self.assertEqual(second["version"], "v1.0.0-realtime.2")
        self.assertNotEqual(first["commit"], second["commit"])
        self.assertFalse(self.prepare(run_number="3")["changed"])
        self.assertEqual(self.prepare(run_number="4")["version"], "v1.0.0-realtime.2")

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
                    self.assertFalse(daily.release_published("v1.0.0-realtime.1"))
                else:
                    with self.assertRaises(urllib.error.HTTPError):
                        daily.release_published("v1.0.0-realtime.1")
            error.close()

    def test_explicit_rebuild_creates_bundleable_history_without_changing_source(self):
        first = self.prepare()
        rebuilt = self.prepare(force=True)
        self.assertTrue(rebuilt["changed"])
        self.assertNotEqual(first["commit"], rebuilt["commit"])
        self.assertEqual(first["source_digest"], rebuilt["source_digest"])
        self.assertEqual("v1.0.0-realtime.2", rebuilt["version"])
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
        self.assertEqual("v1.0.1-realtime.1", result["version"])
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

    def test_revisions_do_not_depend_on_workflow_run_number(self):
        self.assertEqual("v1.0.0-realtime.1", self.prepare(run_number="100")["version"])
        self.assertEqual("v1.0.0-realtime.2", self.prepare(force=True, run_number="100")["version"])
        for revision in range(3, 11):
            self.assertEqual(f"v1.0.0-realtime.{revision}", self.prepare(force=True)["version"])

    def test_legacy_numbering_migrates_once_then_skips(self):
        self.prepare()
        marker = self.fork / ".github/native-build.json"
        previous = json.loads(marker.read_text())
        previous.update(firmware_schema=1, version="v1.0.4")
        self.commit_file(self.fork, str(marker.relative_to(self.fork)), json.dumps(previous), "legacy version")
        self.assertEqual("v1.0.0-realtime.1", self.prepare()["version"])
        self.assertFalse(self.prepare()["changed"])

    def test_realtime_marker_with_wrong_base_is_rejected(self):
        self.prepare()
        marker = self.fork / ".github/native-build.json"
        previous = json.loads(marker.read_text())
        previous["version"] = "v1.0.1-realtime.1"
        self.commit_file(self.fork, str(marker.relative_to(self.fork)), json.dumps(previous), "bad version")
        with self.assertRaisesRegex(ValueError, "version.*upstream"):
            self.prepare(force=True)

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


class Stable120MergeTests(unittest.TestCase):
    def test_actual_voice_source_preserves_native_and_stock_effect(self):
        root = Path(__file__).resolve().parents[1]
        path = "echod/internal/feature/voice/voice.go"
        stable = "0d4cb94cb08dbae20f70b312ce7a265c20f8510f"
        # The new stable object is absent before first preparation. Its previous
        # stable ancestor is already in the maintained fork's tested history.
        available = subprocess.run(["git", "cat-file", "-e", stable + "^{commit}"],
                                   cwd=root, capture_output=True, check=False)
        ancestor = (subprocess.check_output(["git", "merge-base", "HEAD", stable],
                    cwd=root, text=True).strip() if available.returncode == 0 else
                    "db45fb2f8dc51a420caf0f85a858a31ef04ee2e4")
        base = subprocess.check_output(["git", "show", ancestor + ":" + path], cwd=root)
        with tempfile.TemporaryDirectory() as directory:
            native, original = Path(directory) / "native", Path(directory) / "base"
            native.write_bytes((root / path).read_bytes())
            original.write_bytes(base)
            fixture = root / "tools/fixtures/voice-v1.2.20.txt"
            merged = subprocess.run(["git", "merge-file", "-p", str(native), str(original), str(fixture)],
                                    capture_output=True, text=True, check=False)
        self.assertEqual(merged.returncode, 0, "Native voice conflicts with stable v1.2.20")
        for retained in ("realtime *nativeRealtime", "effect *esphome.Action", "v.nativeRealtimeStart()",
                         "v.Start(0)", "v.realtime.stop()", "HomeAssistant()",
                         "func (v *Voice) ShowEffect(", "v.turn.askEffect(name)", "releaseRing()"):
            # gofmt column alignment is immaterial to the retained fields.
            self.assertIn(" ".join(retained.split()), " ".join(merged.stdout.split()))


class Stable122MergeTests(unittest.TestCase):
    def test_actual_source_merges_upstream_realtime_and_streaming(self):
        root = Path(__file__).resolve().parents[1]
        # Replay changes from the current common ancestor; an already merged
        # old release must not conflict with later stock rendering changes.
        stable = "747246a5811de76d3b16bf26f3b867772d4b8b0c"
        ancestor = subprocess.check_output(["git", "merge-base", "HEAD", stable],
                                           cwd=root, text=True).strip()
        for relative in ("internal/feature/display/render.go",
                         "internal/feature/voice/realtime.go",
                         "internal/feature/voice/realtime_test.go"):
            with self.subTest(path=relative), tempfile.TemporaryDirectory() as directory:
                path = "echod/" + relative
                native, original = Path(directory) / "native", Path(directory) / "base"
                source = root / path
                native.write_bytes(source.read_bytes() if source.exists() else b"")
                base = subprocess.run(["git", "show", ancestor + ":" + path],
                                      cwd=root, capture_output=True, check=False)
                original.write_bytes(base.stdout if base.returncode == 0 else b"")
                fixture = Path(directory) / "upstream"
                fixture.write_bytes(subprocess.check_output(
                    ["git", "show", "747246a5811de76d3b16bf26f3b867772d4b8b0c:" + path],
                    cwd=root))
                merged = subprocess.run(["git", "merge-file", "-p", str(native),
                                         str(original), str(fixture)],
                                        capture_output=True, text=True, check=False)
                self.assertEqual(merged.returncode, 0,
                                 "Native source conflicts with stable v1.2.22: " + relative)


class Stable123ReadmeMergeTests(unittest.TestCase):
    def test_actual_readme_merges_without_losing_native_disclosure(self):
        root = Path(__file__).resolve().parents[1]
        stable = "26db47269f17050fc389ccecbb036803180017de"
        base = subprocess.check_output(["git", "merge-base", "HEAD", stable],
                                       cwd=root, text=True).strip()
        with tempfile.TemporaryDirectory() as directory:
            native, original, upstream = [Path(directory) / name
                                          for name in ("native", "base", "upstream")]
            native.write_bytes((root / "README.md").read_bytes())
            original.write_bytes(subprocess.check_output(
                ["git", "show", base + ":README.md"], cwd=root))
            upstream.write_bytes(subprocess.check_output(
                ["git", "show", stable + ":README.md"], cwd=root))
            merged = subprocess.run(["git", "merge-file", "-p", str(native),
                                     str(original), str(upstream)],
                                    capture_output=True, text=True, check=False)
        self.assertEqual(merged.returncode, 0,
                         "Native README conflicts with stable v1.2.23")
        for retained in ("Two wake words, two assistants",
                         "enabling Realtime sends audio to OpenAI",
                         "requires a paid API account", "docs/native-realtime.md"):
            self.assertIn(retained, merged.stdout)


if __name__ == "__main__":
    unittest.main()
