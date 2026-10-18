# Mainline kernel for the Skylight Calendar 15

Linux 6.18.56 from kernel.org with the board files here, built by `build.sh` into `boot-skylight.img`:
an Android boot image (header v2, the stock load addresses) whose second stage is a Rockchip resource
image holding the device tree (as `rk-kernel.dtb`, `d156-mipi.dtb` and `d156-edp.dtb`, so the stock
U-Boot cannot pick one of its own) and the boot logo. `uname -r` says `6.18.56-skylight`.

- `rk3566-skylight-cal150.dts`: the board. Its values are the unit's stock device tree's.
- `panel-skylight-cal150.c`: the display, a DSI-to-LVDS bridge set up with the stock panel's 38
  generic writes; the chip is not identified.
- `skylight.config`: merged over arm64 `defconfig`; `build.sh` checks that every line stuck. Modules
  stay enabled but none are shipped, so what the board and TECHO5 need is built in (netfilter for the
  Wi-Fi firewall, IPv6, Bluetooth, the sound card, /dev/i2c-N for the light sensor).
- `pack-sky.py`: the boot image. Its header ID is checked against the stock image's algorithm.
- `mklogo.py`: the kernel's boot logo and U-Boot's `logo.bmp`, from `logo/TECHO5_logo.png`.
- `bench/`: the bench initramfs (a root shell on USB serial, `BENCH=1`).
- `installer/`: the installer image's `/init` (`INSTALLER=1`): it runs `../install/install.sh` on the
  set `tools/install-skylight.py` stages on `/cache`, then restarts into TECHO5, or into Android when
  it stopped before writing anything.

Things learned on the unit:

- The panel's GPIOs must be requested as outputs; left as inputs, setting them drives nothing.
- `dclk_vop1` cannot set VPLL's rate itself, so VPLL is given 912 MHz (152 MHz times 6).
- The speaker amplifier hangs off the codec's headphone outputs.
- `mmc-pwrseq-simple` needs `CONFIG_RESET_GPIO`; built-in cfg80211 needs `CONFIG_RFKILL=y`;
  built-in hci_uart needs `CONFIG_POWER_SEQUENCING=y`.
- brcmfmac asks for its firmware at about 1.8 s, after the root is mounted: the firmware lives in the
  store's `/lib/firmware`, where a ramdisk copy would already be hidden.
- fbcon refuses a logo as tall as the screen, so the logo is 1064 rows, centered.
- The stock U-Boot draws its own `logo.bmp` only through vendor display nodes this tree does not
  carry, so the first seconds stay dark.
