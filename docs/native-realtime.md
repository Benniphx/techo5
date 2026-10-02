# Native Realtime voice assistant (draft)

This optional mode connects TECHO5 directly to OpenAI's Realtime API. It uses the existing microphone,
speaker, local wake detection and voice screen; no additional server, Wyoming speech services or
Pipecat runner is required. Home Assistant remains the default answering mode. The ordinary direct
speech-and-chat pipeline remains available separately.

This integration is a draft. Offline tests can check protocol and lifecycle behavior; they do not
establish working audio, echo cancellation or interaction on real hardware. Hardware acceptance is
still pending. Do not describe this as a validated replacement for an existing installation yet.

## Configure

Open the device's existing setup page and authorize the browser with a physical button press. In
**Voice assistant**, choose **OpenAI Realtime, directly (cloud)**. Enter an OpenAI API key. The key is
write-only on the page: a blank field preserves the saved key while staying with the same provider.
Changing answering modes into Direct or Realtime when a key is saved requires entering a replacement
key explicitly. Switching via Home Assistant does not bypass this check. Direct mode may instead
remove the key explicitly if its endpoint needs none; Realtime always requires a key. This prevents
sending one provider's saved credential to another by switching modes with a blank field.
The state file is owner-only. Protect the local setup connection while entering the secret; the existing setup page uses local HTTP, not TLS.
Never attach the state file or key to an issue or pull request.

The default model is `gpt-realtime-2.1` and the default voice is `marin`. The model must be a
`gpt-realtime` model name; voice names may be changed to ones supported by that model and account.
Realtime uses a two-letter lowercase ISO 639-1 language such as `en` or `de`; empty means English.
The assistant instructions field adds to the existing TECHO5 prompt. Availability and account access are checked
by the provider when connecting; accepting a syntactically valid name does not guarantee access.

Realtime settings in the private device state are:

```json
{
  "brain": {
    "mode": "realtime",
    "realtime_model": "gpt-realtime-2.1",
    "realtime_voice": "marin",
    "realtime_tools": "",
    "language": "en"
  }
}
```

The API key belongs in the write-only setup field, not in the example above. STT, TTS, direct voice,
custom chat endpoint, chat model and search-server fields are incompatible with Realtime mode.
The page disables those fields when selecting Realtime. Without JavaScript, clear incompatible
fields before saving. Invalid input is rejected before changing either the settings or saved key.

Audio goes directly to OpenAI over an authenticated, certificate-verified TLS WebSocket. Only a local
wake starts a connection, including up to 500 ms of preceding microphone audio. The cloud connection
is closed while idle. Session duration,
input/output queues, messages and tool exchanges are bounded. Cancellation and shutdown close the
connection. A failure is reported for the chosen mode; there is no silent fallback to Home Assistant.

An OpenAI API account is required and audio usage incurs account costs. Check current account pricing
and usage limits before enabling it. Enabling tools also sends their definitions and returned device
information to the cloud model. Read scope can include station names, calendar event titles,
alarm labels and timer labels. Device names and custom assistant instructions may be included in
the session prompt; choose those with the same privacy expectations as speech audio.

## Tools and limits

Tools are disabled by default. Their scope is chosen explicitly in setup, captured once per session,
and enforced for every call. Definitions and execution reuse the existing direct assistant's tools
and instructions, rather than adding another device-control implementation.

| Scope | Existing abilities enabled |
| --- | --- |
| None (`""`) | No access to device information or controls |
| Read (`"read"`) | `list_timers`, `list_alarms`, `list_stations`, `weather`, `calendar` |

The weather, timer, alarm and station-list results are captured from the existing device tools at
session initialization. Their callbacks read those bounded in-memory snapshots during the session;
ask again with a new local wake for refreshed snapshots. The weather snapshot uses the device's
configured current-weather and forecast caches. Constructing a station snapshot may trigger the
existing background list refresh without waiting for network data; unavailable or stale lists remain
unavailable or stale for that session.
Calendar returns the existing cached events and may start the device's normal cache refresh; incomplete
or unavailable data remains an error or is described as incomplete. These are TECHO5's own weather and
calendar semantics, not a promise of identical behavior to any external Home Assistant service.

This draft excludes all device mutations, remote playback, radio-directory lookup, web search,
intercom calls and deferred screen changes. Timer, alarm and volume mutations synchronously persist
configuration with filesystem flushes and may emit hardware callbacks; those operations cannot honor
session cancellation. The callee-list callback reads a contacts file and is excluded as well. These
tools are not advertised or allowed. Device scope is rejected until genuinely cancellable seams exist;
there is no detached mutation worker that could act after cancellation.

There is no arbitrary Home Assistant entity access, shell execution or external Home Assistant bridge.
In particular, the Pipecat pilot's custom `get_home_state` tool is not provided. The assistant cannot
control general home entities simply because this mode is enabled.

Calls are limited to advertised names and valid JSON object arguments. The existing tool's own
semantic checks still apply. Arguments and results have size limits; malformed input and calls after
cancellation are refused. No additional Realtime tool argument/result logs are recorded. The ordinary
TECHO5 components retain their existing operational logs.

## Hardware acceptance before normal use

- Verify an explicit local wake starts one session and no cloud audio is sent while idle.
- Verify microphone and speaker sample rates, intelligible speech and volume on the target model.
- Verify cancel, mute, timeout, disconnect and shutdown release recording, playback and connection.
- Verify the voice screen returns to idle after completion and failures.
- Test unavailable account/model, missing key, TLS failure and interrupted network without a fallback.
- Test None and Read scopes, including refusal of unadvertised tools and current weather.
- Confirm saved setup and key survive restart without exposing the key in HTML or diagnostics.

Dot and Spot builds share the mode and tool filtering; their build/test results do not
replace model-specific hardware acceptance. No unsupported hardware claim should be made in release
notes until these checks have evidence on that device.
