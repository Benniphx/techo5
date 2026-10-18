#!/bin/bash
# SPDX-License-Identifier: MIT
#
# build.sh: build TECHO5's mainline kernel for the Skylight Calendar 15 (150-CAL; Rockchip RK3566,
# Skyworth board D156) from the kernel.org linux-6.18.56 tarball and the board files here, and pack
# it as the Android boot image (header v2 with a Rockchip resource image) that the stock U-Boot starts
# from the recovery partition. Runs on Linux or in WSL. See README.md.
#
#   tools/linux/skylight/kernel/build.sh
#
# Environment:
#   WORK       work directory (default ~/techo5-skylight-kbuild): downloads, the kernel tree, output
#   OUT        output directory (default $WORK/out)
#   KTREE      use this unpacked linux-6.18.56 tree in place instead of unpacking the tarball afresh
#   JOBS       parallel make jobs (default: nproc)
#   BENCH      also pack boot-skylight-bench.img: the same kernel with a bench initramfs (a root
#              shell on USB serial; bench/init), which needs BUSYBOX (a static arm64 busybox)
#   INSTALLER  also pack boot-skylight-installer.img: the same kernel with the installer initramfs
#              (installer/init, ../install/install.sh and an empty techo5-data image), which
#              tools/install-skylight.py writes into recovery for the one boot that installs; needs
#              BUSYBOX and mke2fs (e2fsprogs)
#   CROSS_COMPILE  default aarch64-linux-gnu- (built and tested with Ubuntu 24.04's GCC 13.3)
#
# Needs: curl, sha256sum, tar, xz, make, gcc, bison, flex, bc, perl, python3 with Pillow, cpio, gzip,
# an aarch64 cross GCC, libssl-dev, libelf-dev.
#
# Nothing of the unit's own goes in: its Wi-Fi and Bluetooth firmware are copied from its stock
# vendor partition into the store at install (tools/linux/skylight/README.md), never shipped.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../../../.." && pwd)
WORK=${WORK:-$HOME/techo5-skylight-kbuild}
OUT=${OUT:-$WORK/out}
JOBS=${JOBS:-$(nproc)}

KVER=6.18.56
KURL=https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-$KVER.tar.xz
KSHA256=4633bbda2ab95eaf2a7af4041236451d6759c72e6946265531af4b3aacefbf89

export ARCH=arm64 CROSS_COMPILE=${CROSS_COMPILE:-aarch64-linux-gnu-}
export KBUILD_BUILD_USER=techo5 KBUILD_BUILD_HOST=skylight-build
# Fixed, so a build is reproducible; bump it whenever the kernel changes, or a unit cannot tell the
# new kernel from the one before by `uname -v`.
export KBUILD_BUILD_TIMESTAMP="2026-10-17 00:00:00 UTC" KBUILD_BUILD_VERSION=1
export LC_ALL=C TZ=UTC

mkdir -p "$WORK/cache" "$OUT"
WORK=$(cd "$WORK" && pwd) OUT=$(cd "$OUT" && pwd)

fetch() { # fetch URL DEST SHA256
	if [ ! -f "$2" ] || ! echo "$3  $2" | sha256sum -c --quiet - 2>/dev/null; then
		mkdir -p "$(dirname "$2")"
		curl -sSfL -o "$2.part" "$1"
		mv "$2.part" "$2"
	fi
	echo "$3  $2" | sha256sum -c --quiet - || { echo "sha256 mismatch: $2" >&2; exit 1; }
}

# 1. Kernel source
if [ -n "${KTREE:-}" ]; then
	KTREE=$(cd "$KTREE" && pwd)
	grep -q "^SUBLEVEL = ${KVER##*.}$" "$KTREE/Makefile" || { echo "$KTREE is not linux-$KVER" >&2; exit 1; }
else
	fetch "$KURL" "$WORK/cache/linux-$KVER.tar.xz" "$KSHA256"
	KTREE=$WORK/linux-$KVER
	rm -rf "$KTREE"
	tar -C "$WORK" -xJf "$WORK/cache/linux-$KVER.tar.xz"
fi

# 2. Board files: the device tree, the panel driver, the boot logo
cp "$HERE/rk3566-skylight-cal150.dts" "$KTREE/arch/arm64/boot/dts/rockchip/"
grep -q skylight "$KTREE/arch/arm64/boot/dts/rockchip/Makefile" ||
	echo 'dtb-$(CONFIG_ARCH_ROCKCHIP) += rk3566-skylight-cal150.dtb' >> "$KTREE/arch/arm64/boot/dts/rockchip/Makefile"
P=$KTREE/drivers/gpu/drm/panel
cp "$HERE/panel-skylight-cal150.c" "$P/"
grep -q SKYLIGHT_CAL150 "$P/Makefile" ||
	echo 'obj-$(CONFIG_DRM_PANEL_SKYLIGHT_CAL150) += panel-skylight-cal150.o' >> "$P/Makefile"
grep -q SKYLIGHT_CAL150 "$P/Kconfig" || python3 - "$P/Kconfig" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
entry = ('config DRM_PANEL_SKYLIGHT_CAL150\n'
         '\ttristate "Skylight Calendar 15 (150-CAL) DSI-to-LVDS panel"\n'
         '\tdepends on OF && DRM_MIPI_DSI && BACKLIGHT_CLASS_DEVICE\n\n')
i = s.rindex("endmenu")
open(p, "w").write(s[:i] + entry + s[i:])
EOF
python3 "$HERE/mklogo.py" "$REPO/logo/TECHO5_logo.png" "$KTREE/drivers/video/logo/logo_linux_clut224.ppm" "$OUT/logo.bmp"

# 3. Configuration: arm64 defconfig with the fragment over it, and every line of the fragment checked
cd "$KTREE"
export KCFLAGS="-fdebug-prefix-map=$KTREE=." KAFLAGS="-fdebug-prefix-map=$KTREE=."
make -s defconfig
scripts/kconfig/merge_config.sh -m .config "$HERE/skylight.config" >/dev/null
make -s olddefconfig
bad=0
while read -r l; do
	case "$l" in
	"# CONFIG_"*" is not set") s=${l#\# }; s=${s% is not set}
		grep -q "^$s=" .config && { echo "not off: $s" >&2; bad=1; } ;;
	CONFIG_*) grep -qx "$l" .config || { echo "did not stick: $l" >&2; bad=1; } ;;
	esac
done < "$HERE/skylight.config"
[ $bad = 0 ] || exit 1

# 4. Build. Modules stay enabled in the configuration but none are shipped.
make -s -j"$JOBS" Image
make -s DTC_FLAGS=-@ rockchip/rk3566-skylight-cal150.dtb
cp arch/arm64/boot/Image "$OUT/Image"
cp arch/arm64/boot/dts/rockchip/rk3566-skylight-cal150.dtb "$OUT/rk3566-skylight-cal150.dtb"

# 5. The boot image: an empty ramdisk (the root is the store), the store as root
R=$WORK/ramdisk
rm -rf "$R" && mkdir -p "$R"
(cd "$R" && find . | sort | cpio -o -H newc --quiet --owner=0:0 | gzip -9 -n) > "$OUT/ramdisk.cpio.gz"
CMDLINE="console=ttyS2,1500000n8 earlycon=uart8250,mmio32,0xfe660000 panic=10 loglevel=7"
python3 "$HERE/pack-sky.py" --kernel "$OUT/Image" --dtb "$OUT/rk3566-skylight-cal150.dtb" \
	--ramdisk "$OUT/ramdisk.cpio.gz" --logo "$OUT/logo.bmp" \
	--cmdline "$CMDLINE ro rootwait root=PARTLABEL=techo5-store init=/init vt.global_cursor_default=0 fbcon=logo-count:1 fbcon=logo-pos:center" \
	--out "$OUT/boot-skylight.img"
(cd "$OUT" && sha256sum boot-skylight.img > boot-skylight.img.sha256)

# 6. The bench image, on request: the same kernel with a root shell on USB serial in its initramfs
if [ -n "${BENCH:-}" ]; then
	[ -x "${BUSYBOX:-}" ] || { echo "BENCH needs BUSYBOX, a static arm64 busybox" >&2; exit 1; }
	B=$WORK/bench
	rm -rf "$B" && mkdir -p "$B/bin" "$B/dev" "$B/proc" "$B/sys" "$B/tmp"
	cp "$BUSYBOX" "$B/bin/busybox"
	sed 's/\r$//' "$HERE/bench/init" > "$B/init" && chmod 755 "$B/init"
	${CROSS_COMPILE}gcc -static -O2 -o "$B/bin/reboot-normal" "$HERE/bench/reboot-normal.c"
	(cd "$B" && find . | sort | cpio -o -H newc --quiet --owner=0:0 | gzip -9 -n) > "$OUT/bench.cpio.gz"
	python3 "$HERE/pack-sky.py" --kernel "$OUT/Image" --dtb "$OUT/rk3566-skylight-cal150.dtb" \
		--ramdisk "$OUT/bench.cpio.gz" --logo "$OUT/logo.bmp" --cmdline "$CMDLINE rdinit=/init" \
		--out "$OUT/boot-skylight-bench.img"
	(cd "$OUT" && sha256sum boot-skylight-bench.img > boot-skylight-bench.img.sha256)
fi
# 7. The installer image, on request: the same kernel with install.sh in its initramfs, run by
#    installer/init on the set install-skylight.py stages on /cache
if [ -n "${INSTALLER:-}" ]; then
	[ -x "${BUSYBOX:-}" ] || { echo "INSTALLER needs BUSYBOX, a static arm64 busybox" >&2; exit 1; }
	command -v mke2fs >/dev/null || { echo "INSTALLER needs mke2fs (e2fsprogs)" >&2; exit 1; }
	I=$WORK/installer
	rm -rf "$I" && mkdir -p "$I/bin" "$I/dev" "$I/proc" "$I/sys" "$I/tmp" "$I/cache" "$I/installer"
	cp "$BUSYBOX" "$I/bin/busybox"
	sed 's/$//' "$HERE/installer/init" > "$I/init" && chmod 755 "$I/init"
	sed 's/$//' "$HERE/../install/install.sh" > "$I/installer/install.sh" && chmod 755 "$I/installer/install.sh"
	${CROSS_COMPILE}gcc -static -O2 -o "$I/bin/reboot-normal" "$HERE/bench/reboot-normal.c"
	# techo5-data as the first boot finds it: an empty ext4 of 64 MiB, which the store's /init grows
	# online to fill its partition. Fixed UUID, hash seed and time, so the image is reproducible.
	rm -f "$WORK/data.img"
	E2FSPROGS_FAKE_TIME=$(date -u -d "${KBUILD_BUILD_TIMESTAMP% UTC}" +%s) mke2fs -q -t ext4 -b 4096 -m 0 		-L techo5-data -U 5ec0da7a-7ec5-4d5a-9a7a-000000000001 		-E root_owner=0:0,hash_seed=5ec0da7a-7ec5-4d5a-9a7a-000000000002 "$WORK/data.img" 64M
	echo "$(stat -c %s "$WORK/data.img") $(sha256sum "$WORK/data.img" | cut -d' ' -f1)" > "$I/installer/data.img.info"
	gzip -9 -n -c "$WORK/data.img" > "$I/installer/data.img.gz"
	(cd "$I" && find . | sort | cpio -o -H newc --quiet --owner=0:0 | gzip -9 -n) > "$OUT/installer.cpio.gz"
	python3 "$HERE/pack-sky.py" --kernel "$OUT/Image" --dtb "$OUT/rk3566-skylight-cal150.dtb" 		--ramdisk "$OUT/installer.cpio.gz" --logo "$OUT/logo.bmp" 		--cmdline "$CMDLINE rdinit=/init techo5.installer=1" --out "$OUT/boot-skylight-installer.img"
	(cd "$OUT" && sha256sum boot-skylight-installer.img > boot-skylight-installer.img.sha256)
fi
cat "$OUT"/*.sha256
