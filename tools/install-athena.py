#!/usr/bin/env python3
"""Install TECHO5 on an Echo Show 8 2nd gen (athena) from TWRP, in one command.

athena is not like the other Shows. There is no LineageOS for it, and TECHO5 runs on Amazon's own
32-bit kernel, straight from Amazon's `boot` partition, which is never changed. That kernel mounts the
`system` partition read-only as / and runs /init there, so the whole of TECHO5 lives on `system`: the
boot and rescue environment at its root and the two root filesystem slots (a and b) beside it. That
partition, written as one image, is all this installer writes. The drivers stay where Amazon put them,
on the `vendor` partition, mounted read-only. TECHO5's state goes into one folder on userdata
(userdata/techo5); nothing else of Fire OS's data is touched.

    python3 tools/install-athena.py --image system-m10.img
    python3 tools/install-athena.py --image system-m10.img --name Kitchen --wifi MyNetwork
    python3 tools/install-athena.py --release v1.3.0 --dry-run

The unit has to be unlocked and in TWRP, with adb working (`adb devices` says recovery). Each step is
checked before the next:

  1. checks    adb sees an athena in TWRP; its partition table is the one this installer knows (system
               p21, boot p20, userdata p23, the unlock's tee1_real and lk_real where they belong); the
               image fits; userdata can be mounted
  2. backup    every partition but cache and userdata, the boot0 and boot1 areas too, into
               backups/<serial>/partitions/, each checked against the unit's own sha256 (skipped when a
               backup is already there)
  3. image     the store image: yours (--image), or the release's, checked against its signed manifest
  4. write     the image onto `system` (mmcblk1p21) only, read back from the flash and compared by sha256
  5. settings  name, Home Assistant key, SSH key and Wi-Fi into userdata/techo5
  6. boot      restart into TECHO5; the first boot grows the store to fill the partition, then slot a
               starts; the install watches for the daemon over the USB serial console

Writes go to one block device, /dev/block/mmcblk1p21, and only after its name in the partition table
reads `system`. Never tee1 (the unlock: writing it bricks the unit), lk or lk_real, lk2_real, boot0,
recovery (TWRP), boot or anything else.

Windows, Linux and macOS alike; needs Python 3 and adb. The Home Assistant key is kept in
backups/<serial>/home-assistant.key; a unit that already has one keeps it, so Home Assistant keeps the
device. The way back to Fire OS is printed at the end, and in docs/install-athena.md.
"""
import argparse
import gzip
import hashlib
import os
import re
import shutil
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from techo5lib import (CONSOLE_TECHO5, Adb, Console, Release, ask_name, ask_wifi, confirm,  # noqa: E402
                       default_dir, fail, need, new_api_key, note, pick_unit, run_main, step, valid_api_key,
                       wifi_conf, write_private)

REPO = 'HuskerMinion/techo5'
BOARD_NAME = 'Echo Show 8 2nd gen'
MMC = 'mmcblk1'

# The one partition this installer writes, by number and by the name the partition table gives it.
SYSTEM = 21
SYSTEM_DEV = '/dev/block/%sp%d' % (MMC, SYSTEM)

# The partition table of an unlocked athena, as far as this installer relies on it: the numbers it
# writes or mounts, and the unlock's own partitions, which prove the table is the one it was made for.
LAYOUT = {
    3: 'tee1_real', 4: 'tee2_real', 6: 'misc', 7: 'recovery', 12: 'vendor', 18: 'lk_real', 19: 'lk2_real',
    20: 'boot', 21: 'system', 22: 'cache', 23: 'userdata',
}
USERDATA = 23

# Never written, whatever a table says: the unlock (tee1, and the original lk it replaces), the real
# bootloader and TEE it loads, the boot ROM's area, TWRP, Amazon's kernel, the drivers.
PROTECTED = {'tee1', 'tee2', 'tee1_real', 'tee2_real', 'lk', 'lk2', 'lk_real', 'lk2_real', 'boot0', 'boot1',
             'preloader', 'recovery', 'boot', 'vendor', 'kb', 'dkb', 'persist', 'misc', 'nvcfg', 'boot_para',
             'metadata', 'spmfw', 'sspm_1', 'userdata'}

# Not saved: cache is Fire OS's scratch space, userdata is not written (only one folder is added to it).
NOT_SAVED = {'cache', 'userdata'}

# What wpa_supplicant reads (the format boot.sh writes from Android's saved networks): the name and
# WPA's key, both as hex, so only the key ever reaches the unit, never the passphrase.
WIFI_CONF = 'ctrl_interface=/run/wpa\nupdate_config=0\nnetwork={\n\tssid=%s\n\tpsk=%s\n}\n'

EXT4_MAGIC = b'\x53\xef'
STORE_LABEL = b'techo5-store'


def image_name(version):
    """What a release calls athena's store image."""
    return 'techo5-system-athena-%s.img.gz' % version


def quote(s):
    return "'" + s.replace("'", "'\\''") + "'"


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(1 << 20), b''):
            h.update(chunk)
    return h.hexdigest()


def first_word(text):
    return (text or '').split(' ')[0].strip()


# ------------------------------------------------------------------------------------------ the unit

def partitions(adb):
    """{number: (name, bytes)} for the unit's eMMC, from sysfs. TWRP's grep crashes on this unit, so
    sed reads the names."""
    out = adb.sh('for d in /sys/class/block/%sp*; do n=${d##*%sp}; '
                 'echo "$n $(sed -n "s/^PARTNAME=//p" $d/uevent) $(cat $d/size)"; done' % (MMC, MMC))
    parts = {}
    for line in out.split('\n'):
        f = line.split()
        if len(f) == 3 and f[0].isdigit() and f[2].isdigit():
            parts[int(f[0])] = (f[1], int(f[2]) * 512)
    return parts


def is_athena(adb):
    """What the unit says it is: TWRP's build properties, and the device tree's compatible list."""
    props = ' '.join(adb.sh('getprop %s' % p) for p in
                     ('ro.product.device', 'ro.build.product', 'ro.product.vendor.device', 'ro.product.board'))
    compat = adb.sh("cat /proc/device-tree/compatible 2>/dev/null | tr '\\0' ' '")
    return 'athena' in props.split() or 'amzn,athena' in compat, (props.strip(), compat.strip())


def check_layout(parts):
    for n, name in sorted(LAYOUT.items()):
        got = parts.get(n, ('(none)', 0))[0]
        if got != name:
            fail('partition %d on this unit is %s, not %s: this is not the partition table of an unlocked athena\n'
                 '   this installer knows, so nothing will be written. (Is this an Echo Show 8 2nd gen with the\n'
                 '   amonet-koboreru unlock?)' % (n, got, name))


def mounts_of(adb, block):
    """Where a block device is mounted, by whatever name /proc/mounts gives it (TWRP mounts by-name)."""
    out = adb.sh('awk \'{print $1, $2}\' /proc/mounts | while read d m; do '
                 '[ "$(readlink -f "$d" 2>/dev/null)" = "%s" ] && echo "$m"; done' % block)
    return [m for m in out.split('\n') if m.strip()]


def target_ok(adb, parts):
    """The one write target, checked again right before every write: the node exists, is a block device,
    is the partition named system, and is not a link to anything else."""
    name, size = parts[SYSTEM]
    if name != 'system' or name in PROTECTED:
        fail('refusing to write: partition %d is %s' % (SYSTEM, name))
    out = adb.sh('[ -b {d} ] && echo BLOCK; readlink -f {d}; sed -n "s/^PARTNAME=//p" /sys/class/block/{m}p{n}/uevent'
                 .format(d=SYSTEM_DEV, m=MMC, n=SYSTEM)).split('\n')
    if len(out) < 3 or out[0] != 'BLOCK' or out[1] != SYSTEM_DEV or out[2] != 'system':
        fail('refusing to write: %s is not the system partition as expected (%s)' % (SYSTEM_DEV, ' / '.join(out)))
    return size


def userdata_dir(adb, dry_run):
    """Where userdata is mounted in TWRP, mounting it when TWRP has not. Its root is never encrypted, so
    TECHO5's folder there is readable however TWRP left Fire OS's own."""
    block = '/dev/block/%sp%d' % (MMC, USERDATA)
    for m in mounts_of(adb, block):
        if adb.sh('[ -d %s ] && echo YES' % quote(m)) == 'YES':
            return m
    if dry_run:
        return None
    out = adb.sh('mkdir -p /tmp/t5-userdata && mount -t ext4 %s /tmp/t5-userdata && echo MOUNTED' % block)
    if 'MOUNTED' not in out:
        fail('userdata (%s) could not be mounted in TWRP: %s\n'
             '   TECHO5 keeps its settings there. Mount Data in TWRP (Mount menu) and run this again.' % (block, out))
    return '/tmp/t5-userdata'


# ------------------------------------------------------------------------------------------ backup

def save_partitions(adb, parts, folder):
    """Every partition but cache and userdata, and the boot areas, into folder, each compared with the
    sha256 the unit computes of its own. Read only; nothing on the unit changes."""
    os.makedirs(folder, exist_ok=True)
    sums = []
    todo = [('/dev/block/%sp%d' % (MMC, n), 'p%d-%s.img' % (n, name), size)
            for n, (name, size) in sorted(parts.items()) if name not in NOT_SAVED]
    for b in ('boot0', 'boot1'):
        size = int(adb.sh('cat /sys/class/block/%s%s/size 2>/dev/null' % (MMC, b)) or 0) * 512
        if size:
            todo.append(('/dev/block/%s%s' % (MMC, b), '%s%s.img' % (MMC, b), size))
    for dev, name, size in todo:
        out = os.path.join(folder, name)
        if size >= 64 << 20:
            note('saving %s (%d MiB)...' % (name, size >> 20))
        adb.exec_out_to_file('dd if=%s bs=1048576 2>/dev/null' % dev, out + '.partial')
        got = os.path.getsize(out + '.partial')
        if got != size:
            fail('saving %s gave %d bytes, not %d' % (dev, got, size))
        mine = sha256_file(out + '.partial')
        theirs = first_word(adb.sh('sha256sum %s' % dev))
        if mine != theirs:
            fail('saving %s: the copy here (%s) is not what the unit reads (%s)' % (dev, mine[:16], theirs[:16]))
        os.replace(out + '.partial', out)
        sums.append('%s  %s' % (mine, name))
    with open(os.path.join(folder, 'SHA256SUMS'), 'w') as f:
        f.write('\n'.join(sums) + '\n')
    return len(sums)


def backup_done(folder):
    try:
        with open(os.path.join(folder, 'SHA256SUMS')) as f:
            names = f.read()
    except OSError:
        return False
    return all(re.search(r'\sp%d-%s\.img$' % (n, LAYOUT[n]), names, re.M) and
               os.path.exists(os.path.join(folder, 'p%d-%s.img' % (n, LAYOUT[n]))) for n in (20, 21))


# ------------------------------------------------------------------------------------------ the image

def raw_image(path, work):
    """The image as a raw file: a .gz is unpacked into work first."""
    if not path.endswith('.gz'):
        return path
    out = os.path.join(work, os.path.basename(path)[:-3])
    with gzip.open(path, 'rb') as src, open(out + '.partial', 'wb') as dst:
        shutil.copyfileobj(src, dst, 1 << 20)
    os.replace(out + '.partial', out)
    return out


def check_image(path):
    """A TECHO5 athena store: ext4, labeled techo5-store, a whole number of 4 KiB blocks."""
    size = os.path.getsize(path)
    with open(path, 'rb') as f:
        sb = f.read(2048)[1024:]
    if len(sb) < 136 or sb[56:58] != EXT4_MAGIC:
        fail('%s is not an ext4 image' % path)
    label = sb[120:136].rstrip(b'\0')
    if label != STORE_LABEL:
        fail("%s is labeled '%s', not '%s': not an athena store image (tools/linux/athena/mkstore.sh)"
             % (path, label.decode('ascii', 'replace'), STORE_LABEL.decode()))
    if size % 4096:
        fail('%s is %d bytes, not a whole number of blocks' % (path, size))
    return size


def unmount_system(adb):
    """TWRP may have the system partition mounted; it has to be free before it is written."""
    for m in mounts_of(adb, SYSTEM_DEV):
        adb.sh('umount %s' % quote(m))
    still = mounts_of(adb, SYSTEM_DEV)
    if still:
        fail('the system partition is mounted at %s and would not unmount; unmount it in TWRP (Mount menu)\n'
             '   and run this again' % ', '.join(still))


def write_image(adb, parts, image, size, want):
    """The image onto the system partition, through TWRP's RAM disk, read back from the flash."""
    psize = target_ok(adb, parts)
    if size > psize:
        fail('the image (%d MiB) is larger than the system partition (%d MiB)' % (size >> 20, psize >> 20))
    unmount_system(adb)
    free = adb.sh("df -k /tmp | awk 'NR==2 {print $4}'")
    if free.isdigit() and int(free) * 1024 < size + (32 << 20):
        fail("TWRP's /tmp has %d MiB free, and the image needs %d MiB" % (int(free) >> 10, size >> 20))
    remote = '/tmp/techo5-system.img'
    adb.push(image, remote)
    if first_word(adb.sh('sha256sum ' + remote)) != want:
        adb.sh('rm -f ' + remote)
        fail('the image changed on its way to the unit; nothing was written')

    def readback():
        # From the flash rather than the page cache, which would only give back what was just written.
        return first_word(adb.sh('sync; echo 3 > /proc/sys/vm/drop_caches; head -c %d %s | sha256sum'
                                 % (size, SYSTEM_DEV)))

    for attempt in (1, 2):
        target_ok(adb, parts)
        adb.sh('dd if=%s of=%s bs=1048576 2>/dev/null; sync' % (remote, SYSTEM_DEV))
        if readback() == want:
            adb.sh('rm -f ' + remote)
            return attempt
        note('the system partition did not read back as written (try %d of 2)' % attempt)
    fail('the system partition did not read back as written, twice. Do not restart the unit from here.\n'
         '   The image is still in TWRP\'s /tmp; try again with this installer, or put Fire OS\'s system back\n'
         '   (see the way back in docs/install-athena.md).')


# ------------------------------------------------------------------------------------------ settings

def unit_file(adb, path):
    out = adb.sh('[ -f %s ] && cat %s' % (quote(path), quote(path)))
    return out.strip() or None


def put_file(adb, local_dir, text, remote, mode):
    """A small file onto the unit through adb push (no shell quoting of its contents), mode set after.
    Written as bytes, so Windows does not turn its line ends into CRLF on the way: wpa_supplicant and
    the daemon read these exactly. Readable only by its owner here, as write_private makes it."""
    tmp = os.path.join(local_dir, 'athena-setting.tmp')
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | getattr(os, 'O_BINARY', 0), 0o600)
    with os.fdopen(fd, 'wb') as f:
        f.write(text.encode('utf-8'))
    try:
        adb.push(tmp, remote)
    finally:
        os.remove(tmp)
    adb.sh('chmod %s %s' % (mode, quote(remote)))


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--serial', help="the unit's adb serial (adb devices); found, or asked for, when missing")
    ap.add_argument('--image', help='a store image you built (tools/linux/athena/mkstore.sh), .img or .img.gz')
    ap.add_argument('--release', default='latest', help="without --image: a release tag, or latest, whose "
                    "signed manifest names %s" % image_name('<version>'))
    ap.add_argument('--name', help='the name Home Assistant shows, e.g. Kitchen; a unit that has one keeps it')
    ap.add_argument('--key-file', help='where the Home Assistant key is kept (default backups/<serial>/home-assistant.key)')
    ap.add_argument('--ssh-key', help='an SSH public key the unit accepts from the start (SSH is switched on)')
    ap.add_argument('--wifi', help="a Wi-Fi network to join (its passphrase is asked for); otherwise Fire OS's "
                    'saved one, or the Show\'s screen')
    ap.add_argument('--wifi-passphrase-file', help='a file holding the --wifi passphrase, for running from a script')
    ap.add_argument('--dry-run', action='store_true', help='check the unit and the image; write nothing')
    ap.add_argument('--force', action='store_true', help='do not ask before writing the system partition')
    ap.add_argument('--no-watch', action='store_true', help='restart the unit and stop; do not watch the first boot')
    ap.add_argument('--backups', default=default_dir('TECHO5_BACKUPS', 'backups'))
    ap.add_argument('--work', default=default_dir('TECHO5_WORK', 'build'))
    ap.add_argument('--adb', default='adb')
    a = ap.parse_args()

    # ------------------------------------------------------------------------------------ 1. checks
    step('checks')
    need(a.adb, 'install the Android platform tools (adb)')
    if a.image and not os.path.exists(a.image):
        fail('no file at %s' % a.image)
    a.serial = pick_unit(a.serial, a.adb, ('recovery',), 'Echo Show 8 in TWRP')
    adb = Adb(a.serial, a.adb)
    state = adb.state()
    if state != 'recovery':
        fail("adb does not see %s in TWRP (state '%s'): start TWRP (hold Volume Up while it powers on) and "
             'run this again' % (a.serial, state))
    if adb.sh('id').split('(')[0] != 'uid=0':
        fail('adb in TWRP is not root on %s' % a.serial)
    athena, (props, compat) = is_athena(adb)
    if not athena:
        fail("%s does not say it is an athena (properties '%s', device tree '%s')" % (a.serial, props, compat))
    parts = partitions(adb)
    check_layout(parts)
    note('%s: an athena in TWRP; system is p%d, %d MiB' % (a.serial, SYSTEM, parts[SYSTEM][1] >> 20))
    backup = os.path.join(a.backups, a.serial)
    key_file = a.key_file or os.path.join(backup, 'home-assistant.key')
    pub = None
    if a.ssh_key:
        with open(os.path.expanduser(a.ssh_key)) as f:
            pub = f.read().strip()
        if not pub.startswith(('ssh-', 'ecdsa-', 'sk-')) or '\n' in pub:
            fail('%s does not look like one SSH public key' % a.ssh_key)
    wifi = None
    if a.wifi:
        if a.wifi_passphrase_file:
            with open(a.wifi_passphrase_file) as f:
                wifi = wifi_conf(a.wifi, f.read().rstrip('\r\n'))
        else:
            wifi = ask_wifi(a.wifi)
        kv = dict(line.split('=', 1) for line in wifi.strip().split('\n'))
        wifi = (kv['ssid'], kv['psk'])
        note("Wi-Fi: '%s' (only its key goes to the unit)" % a.wifi)
    mounted = mounts_of(adb, SYSTEM_DEV)
    if mounted and a.dry_run:
        note('dry run: the system partition is mounted at %s; the real run unmounts it' % ', '.join(mounted))
    elif mounted:
        unmount_system(adb)
        note('system partition unmounted (TWRP had it at %s)' % ', '.join(mounted))
    ud = userdata_dir(adb, a.dry_run)
    if ud:
        note('userdata mounted at %s; TECHO5 keeps its settings in its techo5 folder' % ud)
    else:
        note('dry run: userdata is not mounted in TWRP; the real run mounts it')
    state_dir = ud + '/techo5/misc/techo5' if ud else None
    unit_name = unit_file(adb, state_dir + '/name') if ud else None
    unit_psk = unit_file(adb, state_dir + '/psk') if ud else None
    if a.name is not None:
        a.name = ask_name(a.name, '')
    elif unit_name:
        a.name = unit_name
        note("name in Home Assistant: '%s' (already on the unit)" % a.name)
    else:
        a.name = ask_name(None, BOARD_NAME)

    # ------------------------------------------------------------------------------------ 2. backup
    step('backup')
    folder = os.path.join(backup, 'partitions')
    if backup_done(folder):
        note('partitions already saved: %s' % folder)
    elif a.dry_run:
        note('dry run: the partitions are saved by the real run')
    else:
        note('this reads about %d MiB over USB; system alone is %d MiB'
             % (sum(s for n, s in parts.values() if n not in NOT_SAVED) >> 20, parts[SYSTEM][1] >> 20))
        note('%d partitions saved and checked by sha256: %s' % (save_partitions(adb, parts, folder), folder))

    # ------------------------------------------------------------------------------------ 3. image
    step('the image')
    os.makedirs(a.work, exist_ok=True)
    if a.image:
        src = os.path.abspath(a.image)
        version = 'your image'
        note('store image: your own, %s' % src)
    else:
        rel = Release(REPO, a.release, a.work)
        version = rel.version
        src = rel.asset(image_name(rel.version))
        note('store image %s checked against the signed manifest' % os.path.basename(src))
    image = raw_image(src, a.work)
    size = check_image(image)
    want = sha256_file(image)
    if size > parts[SYSTEM][1]:
        fail('the image (%d MiB) is larger than the system partition (%d MiB)' % (size >> 20, parts[SYSTEM][1] >> 20))
    note('%s: %d MiB, sha256 %s' % (os.path.basename(image), size >> 20, want))
    if a.dry_run:
        print('\nDry run: the unit and the image check out; nothing was written to the unit.')
        return

    confirm(a.force, [
        "About to install TECHO5 (%s) on %s (%s) as '%s', from TWRP." % (version, a.serial, BOARD_NAME, a.name),
        'This writes the system partition (%s), which replaces Fire OS (or the TECHO5 there now).' % SYSTEM_DEV,
        'boot, vendor, recovery (TWRP) and the unlock are not touched; userdata only gains settings in its',
        'techo5 folder. The backup is in %s.' % folder,
    ])

    # ------------------------------------------------------------------------------------ 4. write
    step('write the system partition')
    tries = write_image(adb, parts, image, size, want)
    note('%s written and read back from the flash (sha256 matches%s)' % (SYSTEM_DEV, '' if tries == 1 else ', second try'))

    # ------------------------------------------------------------------------------------ 5. settings
    step('settings')
    os.makedirs(os.path.dirname(key_file) or '.', exist_ok=True)
    local_psk = None
    if os.path.exists(key_file):
        with open(key_file) as f:
            local_psk = f.read().strip()
        if not valid_api_key(local_psk):
            fail('the key in %s is not 32 bytes of base64' % key_file)
    if unit_psk and valid_api_key(unit_psk):
        # Home Assistant knows this unit by the key it has; keep that one.
        psk = unit_psk
        if local_psk != psk:
            if local_psk:
                os.replace(key_file, key_file + '.old')
                note('the key in %s differed from the unit\'s; kept as %s.old' % (key_file, key_file))
            write_private(key_file, psk)
        note('Home Assistant key: the one already on the unit (copy in %s)' % key_file)
    elif local_psk:
        psk = local_psk
        note('Home Assistant key: the existing one in %s' % key_file)
    else:
        psk = new_api_key()
        write_private(key_file, psk)
        note('Home Assistant key: new, in %s' % key_file)
    linux_dir = ud + '/techo5/techo5-linux'
    adb.sh('mkdir -p %s %s && chmod 700 %s' % (quote(state_dir), quote(linux_dir), quote(state_dir)))
    put_file(adb, a.work, a.name + '\n', state_dir + '/name', '644')
    put_file(adb, a.work, psk + '\n', state_dir + '/psk', '600')
    done = ['name', 'Home Assistant key']
    if pub:
        adb.sh('mkdir -p %s/ssh && chmod 700 %s/ssh' % (quote(state_dir), quote(state_dir)))
        put_file(adb, a.work, pub + '\n', state_dir + '/ssh/authorized_keys', '600')
        if not unit_file(adb, state_dir + '/state.json'):
            put_file(adb, a.work, '{"security":{"ssh":true}}\n', state_dir + '/state.json', '600')
        done.append('SSH key')
    if wifi:
        put_file(adb, a.work, WIFI_CONF % wifi, linux_dir + '/wpa_supplicant.conf', '600')
        done.append('Wi-Fi')
    if ud == '/tmp/t5-userdata':
        adb.sh('sync; umount /tmp/t5-userdata')
    else:
        adb.sh('sync')
    note('%s written to userdata/techo5' % ', '.join(done))

    # ------------------------------------------------------------------------------------ 6. boot
    step('first boot')
    adb.reboot()
    note('restarting into TECHO5; the first boot grows the store to fill the partition, then starts slot a')
    if not a.no_watch:
        console = Console(a.serial, CONSOLE_TECHO5)
        deadline = time.time() + 300
        seen = ''
        while time.time() < deadline:
            seen = console.run('echo slot=$(cat /run/techo5/slot 2>/dev/null)- daemon=$(pidof techo5)-', 4) or ''
            if re.search(r'slot=a- daemon=\d', seen):
                break
            time.sleep(5)
        if re.search(r'slot=a- daemon=\d', seen):
            o = console.run('cat /etc/techo5-release; STORE=/store slotctl status | head -1; '
                            'tail -1 /data/techo5-linux/grow.log 2>/dev/null', 8) or ''
            for line in o.split('\n'):
                note(line)
        else:
            note('the USB serial console did not show slot a with the daemon within 5 minutes. That may only be\n'
                 '   this computer not seeing the console: the Show\'s screen says what it is doing (RESCUE if\n'
                 '   no slot started). /data/techo5-linux/ on the unit has the boot logs.')

    print("\nDone. '%s' runs TECHO5 from slot a; the slot commits itself after five healthy minutes." % a.name)
    print('Home Assistant finds it as an ESPHome device. When it asks for the encryption key, paste:\n\n    %s\n\n'
          '(kept in %s)' % (psk, key_file))
    print('\nThe way back to Fire OS (TWRP stays in recovery; boot, vendor and the unlock were never changed):')
    print('  mtkclient, from a computer with the unit in its download mode:')
    print('      mtk w system %s' % os.path.join(folder, 'p21-system.img'))
    print('  or from TWRP (adb in recovery), streaming the 3 GB backup straight to the partition:')
    print('      adb exec-in "dd of=%s bs=1048576" < %s' % (SYSTEM_DEV, os.path.join(folder, 'p21-system.img')))
    print('      (PowerShell has no "<": run that line through cmd /c "...")')
    print('  Fire OS then replaces TWRP with Amazon\'s recovery on its first start: write p7-recovery.img back')
    print('  the same way (mtk w recovery ...) if you want TWRP again.')


if __name__ == '__main__':
    run_main(main)
