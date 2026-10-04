# Experimental fork maintenance and downloads

[Benniphx/techo5](https://github.com/Benniphx/techo5) maintains the optional native OpenAI Realtime
integration separately from [HuskerMinion/techo5](https://github.com/HuskerMinion/techo5).
The upstream project supplies the firmware and hardware support. Its authors do not support this
fork's experimental cloud voice mode.

## Daily upstream check

The **[native daily workflow](https://github.com/Benniphx/techo5/actions/workflows/native-daily.yml)**
checks the latest non-prerelease upstream release once a day, at **03:37 UTC**. GitHub may delay
scheduled runs. Maintainers can also run it manually with **Run workflow** on the Actions page.

When the upstream stable release or the `echod` source fingerprint changes, the workflow merges that
release into a candidate containing this fork's native changes. It runs the default, Dot and Spot
test suites and cross-compiles the Show's ARM daemon. It promotes the tested candidate to the fork's
`main` only after success, and refuses promotion if `main` changed during the run. An unchanged,
already-built release and source fingerprint are skipped. Upstream workflow files are kept at the fork versions; changes to CI require deliberate local maintenance. It does not track unreleased upstream `main` commits,
force-reset the fork, or install anything on a device.

Merge conflicts and failing checks stop promotion. Maintainers need to resolve the conflict or fix
the failure, then run the workflow again. A successful compile does not establish that changed audio,
wake-word or device behavior works on hardware.

## Download a candidate

1. Open the [workflow runs](https://github.com/Benniphx/techo5/actions/workflows/native-daily.yml).
2. Choose a successful run that built a candidate, rather than a run that skipped unchanged source.
3. Read its summary and logs for the upstream release, candidate version and source commit.
4. Download the **Artifacts** entry named `native-show5-<version>-<run_id>-<attempt>`. GitHub requires a
   signed-in account for artifact downloads. Artifacts expire after **7 days**.
5. Unpack it and verify `SHA256SUMS` before using the `echod-arm` binary. `build-info.json` records
   the source information. An additional internal candidate bundle is for workflow promotion,
   not device installation.

Candidate versions look like `v0.9.30_native.<run_number>`; the base identifies the upstream release.
This label does not establish a supported OTA upgrade ordering scheme. The archive contains the
Show daemon binary and build metadata. These are **not complete boot/rootfs images**,
not signed OTA releases, and not a replacement for the device's A/B slot installer. Keep the source
commit with any hardware test result so a report identifies exactly what ran.

## Device installation and recovery

There is currently no supported fork-specific OTA installation path. The built-in **Stable** channel
still follows upstream TECHO5 and verifies upstream-signed releases. Installing one replaces the
fork's changes. The **Stable** label alone does not identify the daemon currently running.

The original [getting-started guide](getting-started.md), [installer](install.md) and README clone
command install **upstream firmware**. Downloading a daily artifact does not change those commands
into a fork installer. A complete fork deployment needs tested full images, its own signing and
update feed, and hardware acceptance of install, restart and rollback behavior.

Use an existing, verified local deployment/recovery procedure for experiments. A temporary daemon
overlay does not survive restart or stock firmware replacement. Retain a known-good firmware backup
and USB recovery access before testing. Do not enable automatic stock updates expecting this fork's
native additions to survive.

## Failures and resource use

The workflow records failure information in its logs and opens or updates a fork maintenance issue
for actionable failures. An independent monitor checks for failed or missing daily runs, including
cases where GitHub cannot start the workflow. Fixing a missing schedule may require re-enabling it:
GitHub disables scheduled workflows in public repositories after 60 days without repository activity.

This is a **public fork** using standard GitHub-hosted Linux runners. Those runs are free and do not
consume a private repository's included Actions minutes. Short artifact retention bounds accumulated
build storage. OpenAI audio usage is a separate cost on the device owner's API account. See
[GitHub Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions)
for current terms and the run summaries for measured build time and artifact size.

## Experimental acceptance and privacy

A basic spoken Realtime session has worked on one Echo Show 5, 1st gen. Full regression acceptance,
echo cancellation, unintended additional answers and model-specific hardware checks remain open.
The [native Realtime guide](native-realtime.md) lists the checks and configuration limits.

Realtime is opt-in. It sends audio and any enabled tool definitions/results to OpenAI, needs a paid
API account, and has no silent fallback to Home Assistant if that provider fails. Tools are disabled
by default; the public implementation provides optional bounded read access to existing device data.
It provides no arbitrary Home Assistant entity control or web search. The separate private Home
Assistant extension and personal command examples are not part of this public fork.

Keep credentials in the device's owner-only state file, using the write-only setup field. Protect
its local HTTP setup connection. Report version, source commit and sanitized symptoms; never upload
API keys, state files, private entity catalogs or personal conversation logs.
