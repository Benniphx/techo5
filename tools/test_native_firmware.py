"""Check full-image validation and real Ed25519 signing without production keys."""

import base64
import hashlib
import importlib.util
import io
import json
import os
import struct
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('native_firmware', Path(__file__).with_name('native_firmware.py'))
firmware = importlib.util.module_from_spec(spec)
spec.loader.exec_module(firmware)


class FirmwareTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.version = 'v0.9.30-realtime.10'
        self.commit = 'a' * 40
        self.daemon = self.root / 'echod-arm'
        self.daemon.write_bytes(b'arm daemon fixture ' + firmware.NATIVE_KEY.encode() +
                                f'https://github.com/{firmware.REPO}/releases'.encode())

    def rootfs(self, version=None, daemon=None, extra=None):
        path = self.root / f'techo5-rootfs-{self.version}.tar.gz'
        members = {
            'usr/local/bin/techo5': daemon if daemon is not None else self.daemon.read_bytes(),
            'etc/techo5-release': f'techo5 rootfs {version or self.version} built now'.encode(),
            'usr/local/sbin/slotctl': b'fixture',
            'usr/local/bin/techo5-aec': b'fixture',
            'etc/techo5/boot.sh': b'fixture',
            'usr/share/techo5/models/okay_nabu.tflite': b'fixture',
            'usr/share/techo5/models/jarvis.tflite': b'fixture',
        }
        members.update(extra or {})
        with tarfile.open(path, 'w:gz') as archive:
            for name, data in members.items():
                entry = tarfile.TarInfo(name)
                entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data))
        return path

    def test_rootfs_accepts_embedded_exact_published_daemon(self):
        firmware.check_rootfs(self.rootfs(), self.daemon, self.version)

    def test_rootfs_rejects_changed_daemon_and_wrong_version(self):
        for kwargs in ({'daemon': b'other binary'}, {'version': 'v1.0.9'}):
            with self.assertRaises(ValueError):
                firmware.check_rootfs(self.rootfs(**kwargs), self.daemon, self.version)

    def test_rootfs_rejects_owner_keys_vendor_and_traversal(self):
        for name in ('root/.ssh/authorized_keys', 'vendor/lib/proprietary', '../escape'):
            with self.assertRaises(ValueError):
                firmware.check_rootfs(self.rootfs(extra={name: b'unsafe'}), self.daemon, self.version)

    def test_rootfs_requires_aec_and_models(self):
        path = self.rootfs()
        with tarfile.open(path, 'r:gz') as archive:
            members = [(entry, archive.extractfile(entry).read()) for entry in archive]
        with tarfile.open(path, 'w:gz') as archive:
            for entry, data in members:
                if entry.name != 'usr/local/bin/techo5-aec':
                    archive.addfile(entry, io.BytesIO(data))
        with self.assertRaisesRegex(ValueError, 'required installation asset'):
            firmware.check_rootfs(path, self.daemon, self.version)

    def stage(self):
        self.rootfs()
        for suffix in ('', '-checkers'):
            (self.root / f'techo5-boot{suffix}-{self.version}.img').write_bytes(b'boot fixture')
        info = {'version': self.version, 'commit': self.commit,
                'release_tag': 'v0.9.30', 'upstream_commit': 'b' * 40,
                'native_release_key': firmware.NATIVE_KEY}
        info['asset_hashes'] = {name: {'sha256': firmware.sha(self.root / name),
                                     'size': (self.root / name).stat().st_size}
                               for name in firmware.required_assets(self.version) - {'build-info.json'}}
        (self.root / 'build-info.json').write_text(json.dumps(info))
        firmware.checksums(self.root)
        pins = {device: (name, len(b'boot fixture'), hashlib.sha256(b'boot fixture').hexdigest())
                for device, (name, _, _) in firmware.BOOT_PINS.items()}
        return patch.object(firmware, 'BOOT_PINS', pins)

    def test_manifest_covers_rootfs_and_both_boots_under_own_release(self):
        with self.stage(), patch.object(firmware, 'check_boot'):
            firmware.prepare(self.root, self.version, self.commit)
        manifest = json.loads((self.root / 'manifest.json').read_text())
        self.assertEqual(manifest['version'], self.version)
        self.assertIn('arm', manifest['rootfs'])
        self.assertEqual(set(manifest['assets']), {'build-info.json', 'techo5-boot-v0.9.30-realtime.10.img', 'techo5-boot-checkers-v0.9.30-realtime.10.img'})
        for entry in [*manifest['assets'].values(), manifest['rootfs']['arm'], manifest['binaries']['arm']]:
            self.assertTrue(entry['url'].startswith('https://github.com/Benniphx/techo5/releases/download/v0.9.30-realtime.10/'))

    def test_publisher_rejects_extra_file_mismatched_commit_and_tampering(self):
        with self.stage(), patch.object(firmware, 'check_boot'):
            with self.assertRaisesRegex(ValueError, 'metadata'):
                firmware.prepare(self.root, self.version, 'c' * 40)
            (self.root / 'unexpected').write_text('extra')
            with self.assertRaisesRegex(ValueError, 'Unexpected'):
                firmware.prepare(self.root, self.version, self.commit)
            (self.root / 'unexpected').unlink()
            self.daemon.write_bytes(self.daemon.read_bytes() + b'tampered')
            with self.assertRaisesRegex(ValueError, 'checksums'):
                firmware.prepare(self.root, self.version, self.commit)

    def test_publisher_rejects_version_with_wrong_upstream_base(self):
        with self.stage(), patch.object(firmware, 'check_boot'):
            path = self.root / 'build-info.json'
            info = json.loads(path.read_text())
            info['release_tag'] = 'v0.9.29'
            path.write_text(json.dumps(info))
            firmware.checksums(self.root)
            with self.assertRaisesRegex(ValueError, 'version.*upstream'):
                firmware.prepare(self.root, self.version, self.commit)

    def test_publisher_rejects_asset_symlink(self):
        with self.stage(), patch.object(firmware, 'check_boot'):
            data = self.root / 'original'
            self.daemon.rename(data)
            self.daemon.symlink_to(data)
            # Keep inventory identical, but target outside staged directory.
            external = self.root.parent / (self.root.name + '-daemon')
            data.rename(external)
            self.addCleanup(lambda: external.unlink(missing_ok=True))
            self.daemon.unlink()
            self.daemon.symlink_to(external)
            with self.assertRaisesRegex(ValueError, 'ordinary files'):
                firmware.prepare(self.root, self.version, self.commit)

    def test_real_signature_verifies_and_tampering_is_rejected(self):
        seed = bytes(range(32))  # deterministic disposable test key, never production
        private = self.root / 'test-key.der'
        private.write_bytes(bytes.fromhex('302e020100300506032b657004220420') + seed)
        public = subprocess.check_output(['openssl', 'pkey', '-inform', 'DER', '-in', str(private),
                                          '-pubout', '-outform', 'DER'])[-32:]
        private.unlink()
        payload = b'{"version":"v0.9.30-realtime.10"}\n'
        (self.root / 'manifest.json').write_bytes(payload)
        with patch.object(firmware, 'NATIVE_KEY', base64.b64encode(public).decode()), \
                patch.dict(os.environ, {'TECHO5_NATIVE_SIGN_KEY': base64.b64encode(seed).decode()}):
            firmware.sign(self.root)
            signature = (self.root / 'manifest.json.sig').read_bytes()
            firmware.verify_signature(payload, signature, firmware.NATIVE_KEY)
            with self.assertRaises(subprocess.CalledProcessError):
                firmware.verify_signature(payload + b' ', signature, firmware.NATIVE_KEY)
            with self.assertRaises(subprocess.CalledProcessError):
                firmware.verify_signature(payload, signature, firmware.UPSTREAM_KEY)
        self.assertEqual({path.name for path in self.root.iterdir()},
                         {'echod-arm', 'manifest.json', 'manifest.json.sig', 'SHA256SUMS'})

    def test_wrong_secret_fails_before_signature_output(self):
        (self.root / 'manifest.json').write_text('{}')
        with patch.dict(os.environ, {'TECHO5_NATIVE_SIGN_KEY': base64.b64encode(bytes(32)).decode()}):
            with self.assertRaisesRegex(ValueError, 'compiled native public key'):
                firmware.sign(self.root)
        self.assertFalse((self.root / 'manifest.json.sig').exists())

    def test_versions_must_rank_without_ignored_build_suffix(self):
        for version in ('v0.9.30_native.1', 'v1.0.2-rc.1', 'v1.0.2;evil', 'v1.0.2.3', 'v1.0.4', 'v0.9.30-realtime.0', 'v0.9.30-realtime.01'):
            with self.assertRaises(ValueError):
                firmware.required_assets(version)
        self.assertEqual(len(firmware.required_assets('v0.9.30-realtime.11')), 5)


if __name__ == '__main__':
    unittest.main()
