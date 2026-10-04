#!/usr/bin/env python3
"""Stage verified Show bootstrap images and publish signed native firmware.

Only stage runs from candidate code. prepare/sign run from the workflow's
original, trusted checkout; sign is the only step receiving the signing seed.
No downloaded program or device binary is executed by this helper.
"""

import argparse
import base64
import gzip
import hashlib
import json
import lzma
import os
import re
import struct
import subprocess
import tarfile
import tempfile
import urllib.request
from pathlib import Path

REPO = 'Benniphx/techo5'
NATIVE_KEY = '2aEMRicgezD1lPv8EP/fKj0MC7pV/7j+Cs5r2t1fEZs='
UPSTREAM_KEY = 'KVUuQUbhyKwPBbIneqFEvXYSI+3Hkfu/heCTy5YNVMk='
BOOT_RELEASE = 'v0.7.16'
BOOT_PINS = {
    'cronos': ('techo5-boot-v0.7.16.img', 13553664,
               '7e9ce93a54fc613cbfab5d15929aa06bfd6f63c2c432d6f8261b3599fc3cd791'),
    'checkers': ('techo5-boot-checkers-v0.7.16.img', 13172736,
                 '46c19fd23c210714e29eb2bf88c77b540f3290c0cf50b680b22032e7a4771da7'),
}


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def download(url, dest, maximum):
    if not url.startswith('https://github.com/HuskerMinion/techo5/releases/download/'):
        raise ValueError('Unexpected bootstrap source URL')
    request = urllib.request.Request(url, headers={'User-Agent': 'techo5-native-firmware'})
    with urllib.request.urlopen(request, timeout=120) as response, dest.open('wb') as stream:
        count = 0
        while block := response.read(1 << 20):
            count += len(block)
            if count > maximum:
                raise ValueError('Bootstrap download exceeds its pinned size limit')
            stream.write(block)


def verify_signature(payload, signature, key):
    raw = base64.b64decode(signature.strip(), validate=True)
    public = base64.b64decode(key, validate=True)
    if len(raw) != 64 or len(public) != 32:
        raise ValueError('Invalid Ed25519 signature or public key length')
    with tempfile.TemporaryDirectory() as temp:
        root = Path(temp)
        (root / 'public.der').write_bytes(bytes.fromhex('302a300506032b6570032100') + public)
        (root / 'payload').write_bytes(payload)
        (root / 'signature').write_bytes(raw)
        subprocess.run(['openssl', 'pkeyutl', '-verify', '-pubin', '-keyform', 'DER',
                        '-inkey', str(root / 'public.der'), '-rawin', '-in', str(root / 'payload'),
                        '-sigfile', str(root / 'signature')], check=True, stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL)


def check_boot(path):
    data = path.read_bytes()
    if data[:8] != b'ANDROID!' or len(data) < 40:
        raise ValueError('Bootstrap is not an Android boot image')
    kernel, _, ramdisk = struct.unpack('<3I', data[8:20])
    page = struct.unpack('<I', data[36:40])[0]
    if page not in (2048, 4096, 8192, 16384):
        raise ValueError('Invalid boot image page size')
    offset = page + ((kernel + page - 1) // page) * page
    compressed = data[offset:offset + ramdisk]
    archive = gzip.decompress(compressed) if compressed[:2] == b'\x1f\x8b' else lzma.decompress(compressed)
    if b'root/.ssh/authorized_keys\0' in archive:
        raise ValueError('Bootstrap contains an authorized SSH key')


def required_assets(version):
    if not re.fullmatch(r'v1\.0\.[0-9]+', version):
        raise ValueError('Expected independent native v1.0.N version')
    return {'echod-arm', 'build-info.json', f'techo5-rootfs-{version}.tar.gz',
            f'techo5-boot-{version}.img', f'techo5-boot-checkers-{version}.img'}


def check_rootfs(path, daemon, version):
    with tarfile.open(path, 'r:gz') as archive:
        members = {}
        for member in archive:
            name = member.name.removeprefix('./')
            if name.startswith('/') or '..' in Path(name).parts or name in members:
                raise ValueError('Unsafe or duplicate rootfs path')
            members[name] = member
            if name.startswith('vendor/') or name.endswith('authorized_keys') or 'private_key' in name:
                raise ValueError('Rootfs contains vendor data or an owner credential')
        member = members.get('usr/local/bin/techo5')
        if not member or not member.isfile():
            raise ValueError('Rootfs does not contain the native daemon')
        with archive.extractfile(member) as stream:
            embedded = hashlib.file_digest(stream, 'sha256').hexdigest()
        if embedded != sha(daemon):
            raise ValueError('Rootfs daemon differs from the published ARM daemon')
        stamp = members.get('etc/techo5-release')
        if not stamp or not stamp.isfile() or stamp.size > 4096:
            raise ValueError('Rootfs version stamp missing')
        if f'techo5 rootfs {version} '.encode() not in archive.extractfile(stamp).read():
            raise ValueError('Rootfs version stamp differs from release version')
        for name in ('usr/local/sbin/slotctl', 'usr/local/bin/techo5-aec', 'etc/techo5/boot.sh',
                     'usr/share/techo5/models/okay_nabu.tflite', 'usr/share/techo5/models/jarvis.tflite'):
            if name not in members:
                raise ValueError(f'Rootfs missing required installation asset: {name}')


def stage(directory, version, commit):
    info_path = directory / 'build-info.json'
    info = json.loads(info_path.read_text())
    if info['version'] != version or info['commit'] != commit:
        raise ValueError('Candidate build metadata does not match workflow outputs')
    origin = f'https://github.com/HuskerMinion/techo5/releases/download/{BOOT_RELEASE}'
    with tempfile.TemporaryDirectory() as temp:
        root = Path(temp)
        download(origin + '/manifest.json', root / 'manifest.json', 65536)
        download(origin + '/manifest.json.sig', root / 'manifest.json.sig', 1024)
        payload = (root / 'manifest.json').read_bytes()
        verify_signature(payload, (root / 'manifest.json.sig').read_bytes(), UPSTREAM_KEY)
        manifest = json.loads(payload)
        provenance = {}
        for device, (name, size, digest) in BOOT_PINS.items():
            signed = manifest['assets'][name]
            if signed != {'url': origin + '/' + name, 'size': size, 'sha256': digest}:
                raise ValueError('Upstream bootstrap manifest differs from reviewed pins')
            suffix = '-checkers' if device == 'checkers' else ''
            target = directory / f'techo5-boot{suffix}-{version}.img'
            download(origin + '/' + name, target, size)
            if target.stat().st_size != size or sha(target) != digest:
                raise ValueError('Bootstrap image differs from reviewed size/hash')
            check_boot(target)
            provenance[device] = {'upstream_release': BOOT_RELEASE, **signed}
    check_rootfs(directory / f'techo5-rootfs-{version}.tar.gz', directory / 'echod-arm', version)
    info['bootstrap_sources'] = provenance
    info['native_release_key'] = NATIVE_KEY
    info['hardware_acceptance'] = 'New full image not yet boot-tested; first-generation spoken pilot only.'
    info['optional_helpers'] = {'aec': True, 'librespot': False}
    info['asset_hashes'] = {name: {'sha256': sha(directory / name), 'size': (directory / name).stat().st_size}
                            for name in sorted(required_assets(version) - {'build-info.json'})}
    info_path.write_text(json.dumps(info, indent=2) + '\n')
    checksums(directory)


def checksums(directory):
    lines = [f'{sha(path)}  {path.name}\n' for path in sorted(directory.iterdir())
             if path.is_file() and path.name != 'SHA256SUMS']
    (directory / 'SHA256SUMS').write_text(''.join(lines))


def prepare(directory, version, commit):
    """Validate only data from build artifacts; do not import candidate modules."""
    expected = required_assets(version) | {'SHA256SUMS'}
    if {path.name for path in directory.iterdir()} != expected:
        raise ValueError('Unexpected or missing staged release assets')
    if any(not path.is_file() or path.is_symlink() for path in directory.iterdir()):
        raise ValueError('Staged assets must be ordinary files')
    if not re.fullmatch('[a-f0-9]{40}', commit):
        raise ValueError('Invalid tested candidate commit')
    info_path = directory / 'build-info.json'
    if info_path.stat().st_size > 65536:
        raise ValueError('Oversized build metadata')
    info = json.loads(info_path.read_text())
    if info.get('commit') != commit or info.get('version') != version or info.get('native_release_key') != NATIVE_KEY:
        raise ValueError('Candidate metadata/key does not match trusted release inputs')
    if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+', info.get('release_tag', '')):
        raise ValueError('Invalid upstream stable version')
    if not re.fullmatch('[a-f0-9]{40}', info.get('upstream_commit', '')):
        raise ValueError('Invalid upstream source commit')
    wanted = expected - {'SHA256SUMS', 'build-info.json'}
    if set(info.get('asset_hashes', {})) != wanted:
        raise ValueError('Incomplete staged asset inventory')
    lines = (directory / 'SHA256SUMS').read_text().splitlines()
    expected_lines = {f'{sha(directory / name)}  {name}' for name in expected - {'SHA256SUMS'}}
    if len(lines) != len(expected_lines) or set(lines) != expected_lines:
        raise ValueError('Artifact checksums do not match staged files')
    for name in wanted:
        path = directory / name
        if info['asset_hashes'][name] != {'size': path.stat().st_size, 'sha256': sha(path)}:
            raise ValueError('Artifact metadata hash or size mismatch')
    for device, (_, size, digest) in BOOT_PINS.items():
        suffix = '-checkers' if device == 'checkers' else ''
        path = directory / f'techo5-boot{suffix}-{version}.img'
        if path.stat().st_size != size or sha(path) != digest:
            raise ValueError('Publisher bootstrap differs from reviewed pins')
        check_boot(path)
    check_rootfs(directory / f'techo5-rootfs-{version}.tar.gz', directory / 'echod-arm', version)
    base = f'https://github.com/{REPO}/releases/download/{version}'
    entries = {name: {'url': f'{base}/{name}', 'size': (directory / name).stat().st_size,
                      'sha256': sha(directory / name)} for name in wanted | {'build-info.json'}}
    manifest = {'version': version, 'binaries': {'arm': entries['echod-arm']},
                'rootfs': {'arm': entries[f'techo5-rootfs-{version}.tar.gz']},
                'assets': {name: entry for name, entry in entries.items() if name.endswith('.img') or name == 'build-info.json'},
                'title': f'EXPERIMENTAL native Realtime {version} (upstream {info["release_tag"]})',
                'notes': 'Optional paid cloud voice fork. New image boot/OTA acceptance pending. Not upstream supported.',
                'release_url': f'https://github.com/{REPO}/releases/tag/{version}'}
    (directory / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    return info


def sign(directory):
    seed = base64.b64decode(os.environ['TECHO5_NATIVE_SIGN_KEY'], validate=True)
    if len(seed) != 32:
        raise ValueError('Signing seed must be a base64 Ed25519 32-byte seed')
    with tempfile.TemporaryDirectory() as temp:
        root = Path(temp)
        private = root / 'private.der'
        private.write_bytes(bytes.fromhex('302e020100300506032b657004220420') + seed)
        private.chmod(0o600)
        public = subprocess.check_output(['openssl', 'pkey', '-inform', 'DER', '-in', str(private),
                                          '-pubout', '-outform', 'DER'], stderr=subprocess.DEVNULL)
        if public != bytes.fromhex('302a300506032b6570032100') + base64.b64decode(NATIVE_KEY):
            raise ValueError('Signing seed does not match the compiled native public key')
        signature = subprocess.check_output(['openssl', 'pkeyutl', '-sign', '-inkey', str(private),
                                             '-keyform', 'DER', '-rawin', '-in', str(directory / 'manifest.json')],
                                            stderr=subprocess.DEVNULL)
        if len(signature) != 64:
            raise ValueError('Unexpected Ed25519 signature length')
    (directory / 'manifest.json.sig').write_bytes(base64.b64encode(signature) + b'\n')
    verify_signature((directory / 'manifest.json').read_bytes(), (directory / 'manifest.json.sig').read_bytes(), NATIVE_KEY)
    checksums(directory)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('stage', 'prepare', 'sign'))
    parser.add_argument('--directory', type=Path, default=Path('bin/release'))
    parser.add_argument('--version')
    parser.add_argument('--commit')
    args = parser.parse_args()
    if args.command == 'sign':
        sign(args.directory)
    else:
        if not args.version or not args.commit:
            parser.error('--version and --commit required')
        globals()[args.command](args.directory, args.version, args.commit)


if __name__ == '__main__':
    main()
