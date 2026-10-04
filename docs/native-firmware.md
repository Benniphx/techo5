# Experimental native Realtime firmware

Download the [latest fork release](https://github.com/Benniphx/techo5/releases/latest).
It contains the complete Alpine root filesystem with the native OpenAI Realtime daemon,
not just an executable to mount over another installation. No Pipecat service is needed.
The firmware and installer use this fork's Ed25519 signing key and release feed.

These releases are **experimental**. Basic speech worked on one first-generation Show 5 with
an earlier native daemon; the new full image has not yet passed physical install, restart or
rollback acceptance. Build and signature checks do not establish those results.

## What the release contains

- `techo5-rootfs-<version>.tar.gz`: complete shared Show 5 A/B slot root filesystem.
- `techo5-boot-checkers-<version>.img`: first-generation Show 5 rescue/kernel image.
- `techo5-boot-<version>.img`: second-generation Show 5 rescue/kernel image.
- `echod-arm`: the same native Show daemon used in the root filesystem.
- `manifest.json`, `manifest.json.sig`, `SHA256SUMS` and build metadata.

The boot images are unchanged upstream v0.7.16 rescue images. The build verifies their upstream
signature and pinned hashes before including those exact bytes in the fork's signed release.
The daemon/root filesystem and WebRTC AEC helper are built from the tested fork candidate.
The optional Spotify Connect helper is not included. Boot images contain no SSH
key. The root filesystem contains no OpenAI key, Home Assistant token, personal configuration or
vendor drivers; installation retains the target device's own driver tree.

Fork versions are **`v1.0.<daily workflow run number>`**. This is an independent, increasing
firmware sequence, not TECHO5's upstream version or a claim of stable hardware support.
Release notes and `build-info.json` record the upstream stable version and exact source commit.
A manual rebuild gets a new version. Underscore suffixes alone cannot order OTA upgrades.

## Fresh installation on an unlocked Show 5

For **checkers (1st gen)** or **cronos (2nd gen)**, first follow the upstream bootloader-unlock
and LineageOS prerequisites in [the install guide](install.md). Then clone **this fork**:

```sh
git clone https://github.com/Benniphx/techo5.git
cd techo5
python3 tools/install-show.py --dry-run
python3 tools/install-show.py
```

The dry run requires the connected supported device, downloads both the needed boot image and
root filesystem, verifies the signed manifest and their hashes, and writes nothing to the device.
The actual installer asks before erasing LineageOS; keep its backup and USB recovery access.
Use `python` on Windows. This fork does not publish or accept Show 8, Dot or Spot installation
images. Their upstream hardware support does not establish native Realtime acceptance here.

## Migrating a Show already running TECHO5 Linux

Its upstream updater trusts the upstream key, so it cannot install a fork-signed manifest.
A temporary daemon overlay is insufficient: it disappears after restart or an upstream update.
Download and verify the full fork root filesystem on the computer using this checkout's tools:

```sh
python3 - <<'PY'
import sys
sys.path.insert(0, 'tools')
from techo5lib import Release
release = Release('Benniphx/techo5', 'latest', 'downloads')
print(release.rootfs('arm'))
PY
```

This checks the fork signature and payload hash before printing the downloaded path. With a
known-good slot, persistent state backup and working USB recovery, transfer that tarball to the
Show and install it with the existing `slotctl install <tarball>` procedure on the inactive slot.
Inspect `slotctl status` before deciding to reboot. Do not rerun the Android installer on a
working Linux slot store. The root filesystem changes the daemon and its update trust; the
existing compatible rescue/kernel image can stay. Preserve state under `/data/misc/techo5` rather
than injecting credentials into the image. Physical migration is a separate acceptance step.

After boot, verify the exact fork version/source commit and the **native-stable** channel in
Home Assistant/device configuration. The compiled feed is
`https://github.com/Benniphx/techo5/releases/latest/download/manifest.json`.
Only fork-signed manifests are accepted. The existing A/B trial boot, commit and rollback
mechanism remains in place. Automatic installation remains an explicit device setting; daily
publication does not turn it on. Returning to upstream requires an explicit signed/downloaded
upstream rootfs migration, rather than changing the native channel to an arbitrary URL.

## Voice and Home Assistant setup

Once booted and networked, open the device's setup page, authorize it with the physical button,
select **OpenAI Realtime, directly (cloud)** and enter your own paid OpenAI API key. The
[native voice guide](native-realtime.md) explains model, voice, tool scope and privacy settings.
The key stays in the owner-only persistent device state; protect the local HTTP setup connection.

For the normal Home Assistant satellite mode, add the device through Home Assistant's ESPHome
integration using the installer-generated API encryption key and configure an Assist pipeline.
Home Assistant remains the default answering mode. Realtime mode directly sends audio to OpenAI;
it does not use that Assist pipeline to control arbitrary Home Assistant entities. The public
read tools expose only bounded existing device information. There is no web search bridge.

Native Realtime with general home control currently needs the separate private Home Assistant
extension and its dedicated Home Assistant access configuration. That extension and personal
command examples are not included in these public images. Users of this public fork can choose
ordinary Home Assistant Assist for home control or Realtime for direct cloud conversation.
