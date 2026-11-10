"""Replay a stable merge relative to shared history, retaining native behavior."""
import subprocess
import tempfile
import unittest
from pathlib import Path

STABLE121 = "a0b0f827f42af0b568c4baff61bac370c998ccff"


class NativeDashboardMergeTest(unittest.TestCase):
    def merge(self, path):
        root = Path(__file__).resolve().parents[1]
        ancestor = subprocess.check_output(["git", "merge-base", "HEAD", STABLE121], cwd=root, text=True).strip()
        with tempfile.TemporaryDirectory() as directory:
            native, base, stable = [Path(directory) / name for name in ("native", "base", "stable")]
            native.write_bytes((root / path).read_bytes())
            base.write_bytes(subprocess.check_output(["git", "show", ancestor + ":" + path], cwd=root))
            stable.write_bytes(subprocess.check_output(["git", "show", STABLE121 + ":" + path], cwd=root))
            merged = subprocess.run(["git", "merge-file", "-p", str(native), str(base), str(stable)], capture_output=True, text=True)
        self.assertEqual(merged.returncode, 0, "Native source conflicts with stable v1.2.1")
        return merged.stdout

    def test_actual_touch_source_merges_stable_video_controls(self):
        merged = self.merge("echod/internal/feature/display/display.go")
        self.assertIn("videoAskGesture", merged)
        self.assertIn("RealtimeBusy", merged)

    def test_actual_native_source_merges_stable_dashboard_gate(self):
        merged = self.merge("echod/internal/feature/display/dashboard.go")
        self.assertIn("!s.showDeck", merged)
        self.assertIn("s.realtime", merged)


if __name__ == "__main__":
    unittest.main()
