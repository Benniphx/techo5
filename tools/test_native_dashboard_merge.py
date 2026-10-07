"""Regression for the real native-dashboard conflict with stable v1.2.1."""
import subprocess
import tempfile
import unittest
from pathlib import Path


class NativeDashboardMergeTest(unittest.TestCase):
    def test_actual_native_source_merges_stable_dashboard_gate(self):
        root = Path(__file__).resolve().parents[1]
        path = 'echod/internal/feature/display/dashboard.go'
        base = subprocess.check_output(['git', 'show', '9fd82a881b61302eb83aac97424da81f8e4e8cde:' + path], cwd=root)
        with tempfile.TemporaryDirectory() as directory:
            candidate, ancestor = Path(directory) / 'candidate.go', Path(directory) / 'base.go'
            candidate.write_bytes((root / path).read_bytes())
            ancestor.write_bytes(base)
            merged = subprocess.run(['git', 'merge-file', '-p', str(candidate), str(ancestor),
                                     str(root / 'tools/fixtures/dashboard-v1.2.1.go')], capture_output=True, text=True)
        self.assertEqual(merged.returncode, 0, merged.stdout)
        self.assertIn('!s.showDeck', merged.stdout)
        self.assertIn('s.realtime', merged.stdout)


if __name__ == '__main__':
    unittest.main()
