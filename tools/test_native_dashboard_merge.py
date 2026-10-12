"""Regression for the real native-dashboard conflict with stable v1.2.1."""
import subprocess
import tempfile
import unittest
from pathlib import Path


class NativeDashboardMergeTest(unittest.TestCase):
    def test_actual_touch_source_merges_stable_video_controls(self):
        root = Path(__file__).resolve().parents[1]
        path = 'echod/internal/feature/display/display.go'
        base = subprocess.check_output(['git', 'show', '9fd82a881b61302eb83aac97424da81f8e4e8cde:' + path], cwd=root).decode()
        anchor = '\t// The microphone open for an announcement:'
        video = (root / 'tools/fixtures/display-video-v1.2.1.txt').read_text()
        # The exact stable v1.2.1 insertion that collided with our touch owner.
        upstream = base.replace(anchor, video + anchor)
        with tempfile.TemporaryDirectory() as directory:
            candidate, ancestor, stable = [Path(directory) / name for name in ('native', 'base', 'stable')]
            candidate.write_bytes((root / path).read_bytes())
            ancestor.write_text(base)
            stable.write_text(upstream)
            merged = subprocess.run(['git', 'merge-file', '-p', str(candidate), str(ancestor), str(stable)], capture_output=True, text=True)
        self.assertEqual(merged.returncode, 0, 'Native touch owner conflicts with stable video controls')
        self.assertIn('videoAskGesture', merged.stdout)
        self.assertIn('RealtimeBusy', merged.stdout)

    def test_actual_native_source_merges_stable_dashboard_gate(self):
        root = Path(__file__).resolve().parents[1]
        path = 'echod/internal/feature/display/dashboard.go'
        source = (root / path).read_text()
        base_source = subprocess.check_output(['git', 'show', '9fd82a881b61302eb83aac97424da81f8e4e8cde:' + path], cwd=root).decode()
        stable_source = (root / 'tools/fixtures/dashboard-v1.2.1.go').read_text()

        def gate(text):
            start = text.index('\twant := mode != config.DashboardOff')
            return text[start:text.index('\ts.showDash, s.dashMode', start)]

        # Isolate this release's overlay gate. Later stock night-screen changes
        # are already merged and must not be merged backwards against v1.0.0.
        base = gate(base_source)
        stable = gate(stable_source)
        native = source[source.index('\toriginal := s'):source.index('\twant := mode != config.DashboardOff')]
        candidate_source = native + gate(source).replace(' && !hidden', '')

        with tempfile.TemporaryDirectory() as directory:
            candidate, ancestor = Path(directory) / 'candidate.go', Path(directory) / 'base.go'
            candidate.write_text(candidate_source)
            ancestor.write_text(base)
            upstream = Path(directory) / 'upstream.go'
            upstream.write_text(stable)
            merged = subprocess.run(['git', 'merge-file', '-p', str(candidate), str(ancestor), str(upstream)], capture_output=True, text=True)
        self.assertEqual(merged.returncode, 0, 'Native dashboard conflicts with stable v1.2.1')
        self.assertIn('!s.showDeck', merged.stdout)
        self.assertIn('s.realtime', merged.stdout)


if __name__ == '__main__':
    unittest.main()
