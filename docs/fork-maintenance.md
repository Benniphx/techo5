# Experimental fork maintenance and downloads

[Benniphx/techo5](https://github.com/Benniphx/techo5) maintains optional native OpenAI Realtime
separately from [HuskerMinion/techo5](https://github.com/HuskerMinion/techo5). Upstream supplies
hardware support and most firmware code; its authors do not support this experimental cloud mode.

## Download complete firmware

Use **[fork Releases](https://github.com/Benniphx/techo5/releases/latest)**. Published releases are
public downloads without an Actions-artifact login and do not expire after a few days. Each release
contains a complete Show 5 root filesystem, both board boot images, the native ARM daemon, build
metadata, checksums and a signed manifest. The first- and second-generation boot images reuse
verified, pinned, keyless upstream v0.7.16 rescue/kernel bytes. The root filesystem includes the
WebRTC AEC helper. The optional Spotify Connect helper is not included.

Read **[installation and migration](native-firmware.md)** before using a payload. This checkout's
Show installer verifies the fork signing key and downloads from the fork. An existing stock updater
cannot trust this new publisher automatically. After explicit migration, the native Show firmware
follows **native-stable**, its own signed release feed; daily publication does not enable device
automatic installation. Complete-image physical boot, restart and rollback acceptance remain open.

Versions are independent **`v1.0.<workflow run number>`**; release notes/build metadata identify the
upstream base and exact tested source. The numeric sequence gives Home Assistant meaningful upgrade
ordering, unlike earlier `_native.<run>` daemon-only labels. GitHub releases are deliberately marked
as regular releases for the `/latest/download` feed, with **EXPERIMENTAL** in the title and notes.
They are not a claim of upstream support or stable hardware acceptance.

## Daily stable check and publication

The **[native daily workflow](https://github.com/Benniphx/techo5/actions/workflows/native-daily.yml)**
checks the latest non-prerelease upstream Show release once daily at **03:37 UTC**. GitHub may delay
schedules. Maintainers can run it manually; **force_build** is off by default and publishes a new
numbered release only when explicitly selected.

The workflow builds when the upstream stable release, firmware source fingerprint, or published
release availability changes. The fingerprint covers the daemon, rootfs/build tools and controlled
workflow code; documentation-only edits do not trigger a full rebuild. Missing/incomplete publication
is recovered by a new release on a later run. With unchanged inputs and a complete published release,
the daily check skips compilation and publication.

A read-only build job merges the stable tag into a candidate, keeps fork workflow definitions,
runs synchronization/release-safety tests and default/Dot/Spot Go suites, and assembles the ARM
rootfs/AEC under QEMU on a disposable GitHub runner. It verifies rootfs contents and the original
signed boot-image provenance before uploading temporary staging artifacts.

A separate promotion job advances `main` to exactly the tested candidate, only if `main` still
matches the starting commit. A separate publisher checks out the **original workflow commit**,
validates staged asset inventory/hashes/source/version, and signs the manifest using the dedicated
**native-release** environment key (main branch only). Candidate programs never run in a job with
write access or signing credentials. It creates a draft, uploads every asset, then publishes it as
latest. Existing tags/releases/assets are not overwritten. TLS, manifest signatures and A/B rollback
remain enforced in the device updater.

Conflicts, retags/downgrades, failed tests or a changed remote branch stop the run. Maintainers must
resolve the cause deliberately; there is no force reset or automatic AI repair. A publication failure
can follow successful source promotion, but leaves the previous complete release available. The next
run checks publication separately. Staging artifacts expire after **3 days**, history after **1 day**;
use durable release downloads for installation.

## Failures and resource use

Failures open/update a maintenance issue and an independent monitor watches failed/stale daily runs,
including cases where Actions cannot start. GitHub may disable scheduled public workflows after
60 days without repository activity; maintainers must re-enable them when needed.

Standard GitHub-hosted Linux Actions in this public repository are free and do not consume private
included Actions minutes. Public release storage and short-lived staging replace an expiring-only
binary download path. The separate private Home Assistant daemon build still consumes private job
minutes and bounded artifact storage, monitored against chosen workload budgets; those budgets do
not assert the remaining account-wide allowance. OpenAI usage is separate on the owner's API account.
See [GitHub Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions)
for current terms and successful workflow logs for actual build duration/size.

## Experimental acceptance and privacy

Basic spoken Realtime worked on one Show 5 first generation with an earlier native daemon. Echo
cancellation, unsolicited additional answers, full-image installation/restart/rollback and updated
firmware spoken regression are not established by the offline build. Other models have no native
hardware acceptance claim. See [native voice setup](native-realtime.md).

Realtime is opt-in and sends audio to OpenAI, needs a paid API account, and has no silent Home
Assistant fallback on provider errors. Tools default off; the public implementation offers bounded
existing-device read tools, no arbitrary HA entity control or web search. Normal Home Assistant
Assist remains available. The private Home Assistant extension and personal command examples are
not included in the public images.

Release payloads contain no OpenAI key, HA token, owner SSH key, private configuration or vendor
drivers. Configure secrets through the write-only setup field in owner-only persistent device state;
protect the local HTTP connection. Report version/source and sanitized symptoms, never credentials,
private state/entity catalogs or conversation logs.


## First complete publication check — 2026-10-04

[Release v1.0.4](https://github.com/Benniphx/techo5/releases/tag/v1.0.4), based on upstream
v0.9.30, was built and published successfully by
[run37212583632](https://github.com/Benniphx/techo5/actions/runs/37212583632). Downloaded
manifest signatures, every payload hash, metadata, both installer boot selections and rootfs
contents passed verification. Its rootfs stamp reports daemon v1.0.4/source5ce9067. The complete
build job took122 seconds and publication19 seconds on the hosted runner. The rootfs is
36,683,887 bytes; temporary Actions staging is75,719,621 bytes with3-day retention. A second
[unchanged run](https://github.com/Benniphx/techo5/actions/runs/37212805609) skipped build/promotion/publication.
No physical image installation or spoken regression was performed.

Standard public runner compute does not use private minutes. Temporary staging artifacts still
need a bounded storage budget; repository footprint is not the account's remaining shared allowance.
At the measured size, three daily full builds would retain about217 MiB in staging before expiry.
Release assets provide the durable download path.
