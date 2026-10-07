"""Exercise the owned build command with stable's required helper outputs."""
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest


class NativeWorkflowTests(unittest.TestCase):
    def test_complete_image_build_produces_video_decoder_before_staging(self):
        workflow = (Path(__file__).resolve().parents[1] / '.github/workflows/native-daily.yml').read_text()
        step = workflow.split('      - name: Build complete experimental Show firmware\n', 1)[1]
        command = textwrap.dedent(step.split('        run: |\n', 1)[1].split('      - uses:', 1)[0])
        with tempfile.TemporaryDirectory() as scratch:
            root = Path(scratch)
            (root / 'tools/linux').mkdir(parents=True)
            (root / 'inputs').mkdir()
            (root / 'inputs/apk.static').write_text('fixture')
            (root / 'bin').mkdir()
            stubs = root / 'stubs'
            stubs.mkdir()
            python = stubs / 'python3'
            python.write_text('#!/bin/sh\nexit 0\n')
            python.chmod(0o755)
            for helper in ('aec', 'ffmpeg'):
                (root / f'tools/linux/build-{helper}.sh').write_text(
                    f"printf binary > bin/techo5-{helper}-arm\n")
            (root / 'tools/linux/deploy-rootfs.sh').write_text(textwrap.dedent('''\
                set -eu
                test -s bin/techo5-aec-arm
                test -s bin/techo5-ffmpeg-arm
                printf daemon > bin/echod-arm
                printf metadata > bin/build-info.json
                '''))
            env = dict(os.environ, HOME=str(root / 'home'), VERSION='v1.2.1-realtime.1',
                       SOURCE_COMMIT='a' * 40, PATH=str(stubs) + os.pathsep + os.environ['PATH'])
            result = subprocess.run(['bash', '-eu', '-c', command], cwd=root, env=env,
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, 'Owned workflow omitted a stable-required helper: ' + result.stderr)
            self.assertTrue((root / 'bin/release/echod-arm').is_file())


if __name__ == '__main__':
    unittest.main()
