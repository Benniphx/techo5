"""The native installer trusts only the fork key, including exact signed bytes."""

import importlib.util
import unittest
from contextlib import ExitStack
from pathlib import Path
from unittest.mock import Mock, patch

import techo5lib


FIXTURES = Path(__file__).resolve().parents[1] / "echod/internal/update/testdata"


class NativeInstallerTrustTests(unittest.TestCase):
    def test_real_fork_signature_and_tampering(self):
        message = (FIXTURES / "native-trust-message.json").read_bytes()
        signature = (FIXTURES / "native-trust-message.json.sig").read_bytes()
        techo5lib.verify_manifest(message, signature)
        with self.assertRaises(techo5lib.Fail):
            techo5lib.verify_manifest(message + b"\n", signature)

    def test_genuine_upstream_signature_is_not_trusted(self):
        message = (FIXTURES / "upstream-v0.9.30-manifest.json").read_bytes()
        signature = (FIXTURES / "upstream-v0.9.30-manifest.json.sig").read_bytes()
        with self.assertRaises(techo5lib.Fail):
            techo5lib.verify_manifest(message, signature)

    def test_realtime_release_selects_each_signed_board_image(self):
        spec = importlib.util.spec_from_file_location("install_show", Path(__file__).with_name("install-show.py"))
        installer = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(installer)
        for board, prefix in (("checkers", "techo5-boot-checkers"), ("cronos", "techo5-boot")):
            with self.subTest(board=board), patch.object(installer, "fetch_json") as fetch:
                release = Mock(version="v0.9.30-realtime.1")
                release.signed.return_value = True
                expected = f"{prefix}-v0.9.30-realtime.1.img"
                release.asset.return_value = expected
                self.assertEqual(expected, installer.boot_image(release, "downloads", board))
                release.signed.assert_called_once_with(expected)
                release.asset.assert_called_once_with(expected)
                fetch.assert_not_called()

    def test_unsupported_show8_stops_before_download_or_flash(self):
        spec = importlib.util.spec_from_file_location("install_show", Path(__file__).with_name("install-show.py"))
        installer = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(installer)
        self.assertEqual(installer.REPO, "Benniphx/techo5")
        with ExitStack() as stack:
            stack.enter_context(patch("sys.argv", ["install-show.py", "--serial", "test-unit", "--name", "Test"]))
            for name in ("need", "note", "step"):
                stack.enter_context(patch.object(installer, name))
            stack.enter_context(patch.object(installer, "pick_unit", return_value="test-unit"))
            adb = stack.enter_context(patch.object(installer, "Adb")).return_value
            adb.state.return_value = "device"
            adb.sh.return_value = "crown"
            fastboot = stack.enter_context(patch.object(installer, "Fastboot")).return_value
            console = stack.enter_context(patch.object(installer, "Console")).return_value
            release = stack.enter_context(patch.object(installer, "Release"))
            with self.assertRaisesRegex(techo5lib.Fail, "experimental native fork only publishes firmware"):
                installer.main()
            release.assert_not_called()
            self.assertEqual(fastboot.method_calls, [])
            self.assertEqual(console.method_calls, [])


if __name__ == "__main__":
    unittest.main()
