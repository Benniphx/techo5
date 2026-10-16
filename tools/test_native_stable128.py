"""Merge the actual native sources with the stable v1.2.8 changes."""
import subprocess
import tempfile
import unittest
from pathlib import Path


STABLE128 = "6cfa9267dcaa555725b6fbe8c50ae84bc0cf3818"


def merge_base_source(root, stable, path):
    # Replay only changes after the shared ancestor. Reapplying an old release
    # against its original base conflicts with later, already integrated stock.
    ancestor = subprocess.check_output(["git", "merge-base", "HEAD", stable], cwd=root, text=True).strip()
    return subprocess.check_output(["git", "show", ancestor + ":" + path], cwd=root)


class Stable128MergeTests(unittest.TestCase):
    def test_already_merged_stable_preserves_later_adjacent_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=root, stderr=subprocess.PIPE)
            git("init", "-q")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.invalid")
            source = root / "source"
            original = b"before\nold\nafter\n"
            stable = b"before\nstable\nafter\n"
            evolved = b"before\nnewer stock and native\nafter\n"
            for content in (original, stable, evolved):
                source.write_bytes(content)
                git("add", "source")
                git("commit", "-qm", "source revision")
                if content == stable:
                    git("tag", "stable")
            ancestor = root / "ancestor"
            ancestor.write_bytes(merge_base_source(root, "stable", "source"))
            fixture = root / "stable-source"
            fixture.write_bytes(stable)
            merged = subprocess.run(["git", "merge-file", "-p", str(source), str(ancestor), str(fixture)],
                                    capture_output=True, check=False)
            self.assertEqual(merged.returncode, 0)
            self.assertEqual(merged.stdout, evolved)
            # The previous fixed-base replay reproduces the false conflict.
            ancestor.write_bytes(original)
            old = subprocess.run(["git", "merge-file", "-p", str(source), str(ancestor), str(fixture)],
                                 capture_output=True, check=False)
            self.assertGreater(old.returncode, 0)

    def test_actual_sources_preserve_native_and_stable_behavior(self):
        root = Path(__file__).resolve().parents[1]
        paths = ("echod/internal/feature/display/dashboard.go",
                 "echod/internal/feature/display/display.go",
                 "echod/internal/feature/voice/voice.go")
        expected = (("s.realtime", "!hidden", "s.dashAsked"),
                    ("view.Realtime", "view.Remaining", "s.atDoor ="),
                    ("nativeRealtimeStart", "v.turn.Woke(slot)", "HomeAssistant()"))
        for path, retained in zip(paths, expected):
            with self.subTest(path=path), tempfile.TemporaryDirectory() as directory:
                base = merge_base_source(root, STABLE128, path)
                candidate, ancestor = Path(directory) / "native", Path(directory) / "base"
                candidate.write_bytes((root / path).read_bytes())
                ancestor.write_bytes(base)
                stable = root / "tools/fixtures" / (Path(path).stem + "-v1.2.8.txt")
                merged = subprocess.run(["git", "merge-file", "-p", str(candidate), str(ancestor), str(stable)],
                                        capture_output=True, text=True, check=False)
                self.assertEqual(merged.returncode, 0, "Native source conflicts with stable v1.2.8")
                for fragment in retained:
                    self.assertIn(fragment, merged.stdout)
                if path.endswith("voice.go"):
                    method = merged.stdout.split("func (v *Voice) open(", 1)[1].split("\n}", 1)[0]
                    self.assertLess(method.index("micTaken()"), method.index("v.nativeRealtimeStart()"))
                    self.assertLess(method.index("v.nativeRealtimeStart()"), method.index("v.turn.Woke(slot)"))


if __name__ == "__main__":
    unittest.main()
