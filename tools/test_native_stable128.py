"""Merge the actual native sources with the stable v1.2.8 changes."""
import subprocess
import tempfile
import unittest
from pathlib import Path


class Stable128MergeTests(unittest.TestCase):
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
                base = subprocess.check_output(["git", "show", "475be5a88423cf47964676ae4a7c7ac44b9c5e9a:" + path], cwd=root)
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
