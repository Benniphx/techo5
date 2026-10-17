# TECHO5 on the Skylight Calendar 15 (150-CAL)

The Skylight Calendar 15 is a 1920x1080 touchscreen calendar built on a Rockchip RK3566 (Skyworth
board D156, FCC ID 2AABK-150). TECHO5 runs on it with its own 64-bit mainline kernel and the Show's
32-bit userland, as on the 2nd gen Show 8. It has no microphones, so it is a screen, a speaker and
Bluetooth: no wake word and no voice assistant.

Status: bench work on one unit. Not in any release.

## What works

| Part | How |
| --- | --- |
| Display | 4-lane MIPI DSI into a DSI-to-LVDS bridge set up over DSI generic writes (`panel-skylight-cal150.c`), VOP2 video port 1, 1920x1080 at 152 MHz (VPLL at 912 MHz) |
| Backlight | pwm4, levels 20..255, enable GPIO0_C6 |
| Touch | Goodix GT911 on I2C5 (m1 pins); it reports 1920x1200, the daemon scales by the driver's ranges |
| Audio | RK809 codec on I2S1; the speaker amplifier (enable GPIO4_C4) is fed from the codec's headphone outputs, not SPKO |
| Wi-Fi | AP6256 (BCM43456) on SDIO, brcmfmac |
| Bluetooth | AP6256 (BCM4345C5) on UART1, hci_uart Broadcom over serdev |
| Clock | HYM8563 on I2C5 (battery backed) and the RK809's |
| CPU | cpufreq 408 MHz to 1.8 GHz, the SYR827 regulator |
| Boot logo | the kernel's fbcon logo, TECHO5's mark where the daemon's splash puts it |

The light sensor (EM3071X) has no mainline driver. The MAC is not the Wi-Fi chip's (the stock NVRAM
gives every unit Broadcom's placeholder): it is made from the eMMC's CID (`device.conf`, and
`echod/internal/layout/skylight.go` for the factory MAC).

## How it boots

The stock U-Boot stays. Android's userdata partition is split into two GPT partitions of TECHO5's
own, `techo5-store` (2 GiB: the slot store, as on the other Shows) and `techo5-data` (the rest:
TECHO5's state, bound to `/data`). TECHO5's boot image goes in the `recovery` partition, and a
`boot-recovery` command at the start of `misc` makes the stock U-Boot start recovery on every boot.

The kernel mounts `techo5-store` read-only as `/` and runs `init` (this directory) at its root, which
picks a slot with `slotctl` and pivots into it, or stays as the rescue environment. `boot`, `super`
and the rest of Android are left as they were.

## Files

- `kernel/`: the kernel's board files and `build.sh` (kernel.org linux-6.18.56, pinned by sha256),
  which makes `boot-skylight.img`; `BENCH=1` also makes a bench image whose initramfs is a root shell
  on USB serial.
- `init`, `mkstore.sh`, `rescue-profile.sh`, `overlay/`: the store and the board's `device.conf`.
- `install/`: `mkgpt.py` (the new GPT, made from the unit's own) and `install.sh` (the writes).

## The unit's own firmware

The Wi-Fi and Bluetooth firmware are the vendor's, from the unit's stock vendor partition
(`/vendor/etc/firmware`: `fw_bcm43456c5_ag.bin`, `nvram_ap6256.txt`, `BCM4345C5.hcd`). They are
never published: `mkstore.sh -F <dir>` puts a unit's own files into its store image, which is then
for that unit only.

## Installing (bench)

On the unit read so far, stock Android is a userdebug build with ADB open and `adb root` working,
which is what this relies on. Every step but the last reads only.

1. Keep a full copy of the eMMC first (`adb root`, then `dd` of the whole block device over
   `adb exec-out`), and check its hashes against the device's own. It is the only way back.
2. Copy the vendor firmware out (`adb pull /vendor/etc/firmware/...`).
3. Build the kernel (`kernel/build.sh`, with `BENCH=1`), the rootfs
   (`tools/linux/deploy-rootfs.sh --out` with `DEVICE_OVERLAY=tools/linux/skylight/overlay
   EXTRA_PACKAGES=busybox-extras`) and the store (`mkstore.sh -F <firmware>`).
4. Make the GPT from the unit's own first 32 MiB (`install/mkgpt.py`) and check it with `sgdisk -v`
   on a sparse image of the eMMC's size.
5. Put the set on `/cache` (the GPT halves, `store.img`, a small ext4 `data.img`, the boot image,
   `install.sh` and its `SHA256SUMS`), write the bench image into `recovery` and `adb reboot
   recovery`. Note: Android's `reboot recovery` also writes `boot-recovery` into `misc`; the bench
   init clears it so that the next reboot is Android again.
6. From the bench shell (USB serial), mount `/cache` read-only and run `install.sh`, which writes and
   reads back each part, and finally sets `misc` so that every boot is TECHO5.
7. On TECHO5, grow `techo5-data` to fill its partition (`resize2fs`, online).

## Back to Android

Write the first 2048 bytes of `misc` back to zeros: the next boot is stock Android, which finds no
userdata and starts fresh. For the unit as it came, write back the eMMC copy from step 1.
