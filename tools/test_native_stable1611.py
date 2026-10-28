"""Replay exact stable v1.6.11 native-function conflicts without network."""
import subprocess
import tempfile
import unittest
from pathlib import Path

STABLE = "daa0d4b5920f58448e32bd0a9cc037d9cef9821d"

class Stable1611MergeTests(unittest.TestCase):
    def test_native_functions_merge_exact_stable_sources(self):
        root = Path(__file__).resolve().parents[1]
        available = subprocess.run(["git", "cat-file", "-e", STABLE + "^{commit}"], cwd=root, capture_output=True)
        ancestor = (subprocess.check_output(["git", "merge-base", "HEAD", STABLE], cwd=root, text=True).strip()
                    if available.returncode == 0 else "1537c3e5b0cea5cbe64c310a97c9a11606ea5f83")
        cases = {
            "detect": ("voice.RealtimeActive.Listen(e.Pause)", "RealtimeBusy()", "routeFor(slot, h)", "d.wakeHeard(slot)"),
            "display": ("realtimeEnded && night", "view.Realtime", "SetTint(t)", "SetLift(", "cueTurn(", "!s.realtime && equalizerOn()"),
            "setup": ("realtime-brain", "b.Reader", "b.RealtimeModel", "b.RealtimeTools"),
        }
        for feature, retained in cases.items():
            path = f"echod/internal/feature/{feature}/" + ("brain.go" if feature == "setup" else feature + ".go")
            with self.subTest(feature=feature), tempfile.TemporaryDirectory() as directory:
                native, base = Path(directory) / "native", Path(directory) / "base"
                native.write_bytes((root / path).read_bytes())
                base.write_bytes(subprocess.check_output(["git", "show", ancestor + ":" + path], cwd=root))
                fixture = root / "tools/fixtures/stable1611" / (feature + "-" + Path(path).name + ".txt")
                merged = subprocess.run(["git", "merge-file", "-p", str(native), str(base), str(fixture)], capture_output=True, text=True)
                self.assertEqual(merged.returncode, 0, "Native function conflicts with exact stable v1.6.11")
                for fragment in retained:
                    self.assertIn(fragment, merged.stdout)
                if feature == "detect":
                    self.assertLess(merged.stdout.index("RealtimeBusy()"), merged.stdout.index("switch routeFor(slot, h)"))

if __name__ == "__main__":
    unittest.main()
