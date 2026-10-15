## Echo Show 8 2nd gen (athena)

TECHO5 now runs on the 2nd gen Echo Show 8 with its own 64-bit kernel.

What works:

- Screen and touch. The screen has a real display driver now.
- All four microphones and the speaker.
- Bluetooth audio with your phone.
- The light sensor.
- Wi-Fi. Set it up on the screen, or pass `--wifi` to the installer.

The speaker uses Amazon's own tuning for the amp. TECHO5 doesn't ship it. The installer reads it from
your unit's backup and checks it against the known versions.

### Only one screen for now

Amazon used several screens in this model. TECHO5's kernel supports one: the ILI9881C with BOE glass.
The installer checks your unit and stops if it has a different one. Those units can still run TECHO5
on Amazon's kernel (`--kernel amazon`).

### How to install

1. Update the Show to the latest Fire OS (7080).
2. Unlock it with saiyaku-athena. You end up in TWRP.
3. Run `python3 tools/install-athena.py --name Kitchen` with the unit in TWRP.
4. Add it in Home Assistant with the key the installer prints.

The installer backs up every partition first. Keep that backup. It's your way back to Fire OS, and
the speaker tuning comes from it.

Full steps: docs/install-athena.md.

### Known issues

- Light sensor values aren't calibrated yet.
- The microphone beamformer is off.
- Only Fire OS 7080's speaker tuning is known. On another version the installer stops and says so.
