# Installing TECHO5 on an Echo Show 8 2nd gen (athena)

The 2nd gen Echo Show 8 (`athena`, 2021, MT8183) installs differently from the other Shows. There is
no LineageOS for it, so TECHO5 runs on Amazon's own 32-bit kernel, booted unchanged from Amazon's `boot`
partition. That kernel mounts the `system` partition read-only as `/` and runs `/init` there, so all of
TECHO5 lives on `system`:

- at its root, the boot and rescue environment (`tools/linux/athena/init`);
- beside it, the slot store: root filesystem slots `a` and `b`, as on the other Shows (`slotctl`).

The drivers (Wi-Fi, sound) stay on Amazon's `vendor` partition, mounted read-only. TECHO5 keeps its
state in one folder on `userdata` (`userdata/techo5`, which is `/data` on the unit) and touches nothing
else there. `boot`, `vendor`, `recovery` (TWRP) and the unlock's own partitions are never written.

Placeholders: `<serial>` is the unit's adb serial, `<image>` the store image
(`system-<version>.img` or a release's `techo5-system-athena-<version>.img.gz`).

## Before you start

- The unit is unlocked with amonet-koboreru and has TWRP in `recovery`. **Never write `tee1` or `lk`:
  that is the unlock, and writing either one bricks the unit.** The installer only ever writes
  `system` (`mmcblk1p21`).
- The unit is in TWRP and `adb devices` lists it as `recovery`.
- Python 3 and adb on the computer.
- About 4 GB free for the backup: the installer saves every partition except `cache` and `userdata`
  (the `system` partition alone is 3 GB) before it writes anything.

## Install

```
python3 tools/install-athena.py --image <image> --name Kitchen
python3 tools/install-athena.py --image <image> --name Kitchen --wifi MyNetwork --ssh-key ~/.ssh/id_ed25519.pub
python3 tools/install-athena.py --image <image> --dry-run
```

Without `--image` it takes the image from a release (`--release <tag>`, default `latest`), checked
against the release's signed manifest. No release carries one yet.

What it does, each step checked before the next:

1. **Checks.** adb sees the unit in TWRP as root; the unit says it is an athena; its partition table
   is the one the installer knows (`system` p21, `boot` p20, `userdata` p23, the unlock's `tee1_real`
   p3 and `lk_real` p18, and the rest). Anything else stops it before a single write. The image is an
   athena store image and fits the partition. `userdata` can be mounted.
2. **Backup.** Every partition except `cache` and `userdata`, plus the `boot0` and `boot1` areas, into
   `backups/<serial>/partitions/`, each compared with the sha256 the unit computes of its own copy.
   Skipped when a backup with `p20-boot.img` and `p21-system.img` is already there.
3. **Image.** Yours, or the release's. It must be ext4 labeled `techo5-store`.
4. **Write.** Asked once (type `ERASE`), then the image goes to TWRP's RAM disk, is checked by sha256,
   written to `/dev/block/mmcblk1p21` only (the node is checked to be the partition named `system`
   right before every write) and read back from the flash. A read back that doesn't match is written
   again once; a second mismatch stops with the unit still in TWRP.
5. **Settings** in `userdata/techo5`: the name (a unit that has one keeps it), the Home Assistant key
   (a unit that has one keeps it, so Home Assistant keeps the device; a copy goes to
   `backups/<serial>/home-assistant.key`), an SSH key (`--ssh-key`, which also turns SSH on) and a
   Wi-Fi network (`--wifi`; only the WPA key reaches the unit, never the passphrase). Without `--wifi`
   the unit takes the network Fire OS saved, or one chosen on the Show's own screen.
6. **First boot.** TWRP restarts the unit. The first boot grows the store to fill the partition (see
   below), then starts slot `a`. The installer watches the USB serial console for the daemon (up to
   five minutes; `--no-watch` skips this).

## The store fills the partition on the first boot

The image is only as big as its contents (about 200 MB), so it writes in seconds. The `system`
partition is 3 GB, and a second slot needs that room. At every boot, before anything else writes to
the store, `/init` runs `slotctl grow /dev/mmcblk1p21`:

- It does nothing once the store fills the partition, so in practice it runs once.
- It grows the filesystem online (the kernel adds the block groups) with `resize2fs`, with the root
  remounted read-write for the moment it takes, then read-only again.
- It refuses a filesystem that was not left clean, one with no room reserved for growing
  (`resize_inode`), and a partition that would need more group descriptor blocks than were reserved,
  so the layout never changes under Amazon's 4.4 kernel. No feature is added; the features are
  compared before and after.
- A failure leaves the store as it was, and the boot goes on.
- The result is in `/data/techo5-linux/grow.log`. By hand, from the rescue shell:
  `STORE=/ slotctl grow /dev/mmcblk1p21`.

What it can't protect against: a power cut during the second or two the grow takes, on a filesystem
without a journal. If that ever happens, run `e2fsck -fy /dev/block/mmcblk1p21` from TWRP, or
reinstall.

## Updates

Updates work as on the other Shows: a new root filesystem goes into the slot that isn't running, on
trial for three boots, and commits itself once the daemon has run for five minutes. A slot that never
gets there is marked bad and the unit goes back to the other one; with no good slot it stays up in
the rescue environment (USB serial and RNDIS at 172.16.42.1).

- **From Home Assistant**: the daemon's update card. A release offers an athena only the rootfs keyed
  `arm-athena` in its signed manifest (published as `techo5-rootfs-athena-<version>.tar.gz`), never
  the other Shows' `arm` one, and the other Shows never take athena's.
- **By hand over SSH**: copy a rootfs to the unit and install it into the other slot:

  ```
  scp techo5-rootfs-athena-<version>.tar.gz root@<address>:/data/techo5-linux/
  ssh root@<address> /usr/local/sbin/slotctl install /data/techo5-linux/techo5-rootfs-athena-<version>.tar.gz
  ssh root@<address> reboot
  ```

  The athena store accepts only a rootfs made for athena (its `device.conf` says `T5_BOARD=athena`);
  anything else is refused before the other slot is emptied.
- `slotctl status`, `slotctl rollback` (mark the running slot bad, boot the other next) and
  `slotctl switch a|b` work as on the other Shows.

## The way back to Fire OS

`boot`, `vendor`, `recovery` and the unlock are as they were; only `system` changed, and its backup is
`backups/<serial>/partitions/p21-system.img`.

- **mtkclient** (the unit in its download mode, as for the unlock): `mtk w system p21-system.img`.
- **From TWRP**, streaming the 3 GB backup straight to the partition from a Git Bash, Linux or macOS
  shell: `adb exec-in "dd of=/dev/block/mmcblk1p21 bs=1048576" < p21-system.img`.

Fire OS replaces TWRP with Amazon's recovery the first time it starts. If you want TWRP back
afterward, write `p7-recovery.img` from the same backup to `recovery` with mtkclient.
