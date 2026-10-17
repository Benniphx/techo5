#!/bin/sh
# Install TECHO5 on the Skylight, run as root on the bench test kernel (nothing of the eMMC mounted
# but cache, where these files are). Writes, in order, each checked by reading it back:
#   1. the new GPT (primary at LBA 1, backup at the last 33 LBAs): userdata -> techo5-store + techo5-data
#   2. techo5-store (store.img) at LBA 6967296, techo5-data (data.img, grown on first boot) at LBA 11161600
#   3. the installed boot image into recovery (by its own partition node, mmcblk0p8)
#   4. "boot-recovery" at the start of misc, so the stock U-Boot always starts recovery (TECHO5)
# Stops at the first mismatch. Nothing outside these ranges is written.
set -e
D=$(dirname "$0")
DISK=/dev/mmcblk0
TOTAL=30535680
say() { echo "install: $*"; echo "install: $*" > /dev/kmsg; }
cd "$D"
sha256sum -c SHA256SUMS
[ "$(cat /sys/class/block/mmcblk0/size)" = "$TOTAL" ] || { say "unexpected eMMC size"; exit 1; }
grep -qx PARTNAME=recovery /sys/class/block/mmcblk0p8/uevent || { say "p8 is not recovery"; exit 1; }
grep -qx PARTNAME=misc /sys/class/block/mmcblk0p4/uevent || { say "p4 is not misc"; exit 1; }

check() { # check FILE SKIP_SECTORS
	n=$(stat -c %s "$1")
	got=$(dd if=$DISK bs=512 skip=$2 count=$((n/512)) 2>/dev/null | sha256sum | cut -d" " -f1)
	want=$(sha256sum "$1" | cut -d" " -f1)
	[ "$got" = "$want" ] || { say "read-back mismatch for $1"; exit 1; }
	say "$1 verified at LBA $2"
}

dd if=gpt-primary.bin of=$DISK bs=512 seek=1 conv=fsync 2>/dev/null; check gpt-primary.bin 1
dd if=gpt-backup.bin of=$DISK bs=512 seek=$((TOTAL-33)) conv=fsync 2>/dev/null; check gpt-backup.bin $((TOTAL-33))
dd if=store.img of=$DISK bs=1048576 seek=3402 conv=fsync 2>/dev/null; check store.img 6967296
dd if=data.img of=$DISK bs=1048576 seek=5450 conv=fsync 2>/dev/null; check data.img 11161600
dd if=boot-sky-store.img of=/dev/mmcblk0p8 bs=1048576 conv=fsync 2>/dev/null
n=$(stat -c %s boot-sky-store.img)
[ "$(head -c $n /dev/mmcblk0p8 | sha256sum | cut -d' ' -f1)" = "$(sha256sum boot-sky-store.img | cut -d' ' -f1)" ] || { say "recovery mismatch"; exit 1; }
say "recovery holds the TECHO5 boot image"
{ printf 'boot-recovery'; dd if=/dev/zero bs=1 count=$((2048-13)) 2>/dev/null; } > /tmp/bcb
dd if=/tmp/bcb of=/dev/mmcblk0p4 bs=2048 count=1 conv=notrunc,fsync 2>/dev/null
[ "$(dd if=/dev/mmcblk0p4 bs=13 count=1 2>/dev/null)" = "boot-recovery" ] || { say "misc not set"; exit 1; }
sync
say "done: misc says boot-recovery; reboot to start TECHO5"
