# TECHO5 and similar projects

This page puts TECHO5 next to other projects that turn old smart displays, Echos and tablets into
Home Assistant voice satellites. It's a snapshot, checked on October 18, 2026, from each project's
own README, docs and release pages (linked at the bottom). These projects move fast, so some of it
will be out of date soon. If something here is wrong or missing, about TECHO5 or anyone else,
please [open an issue](https://github.com/HuskerMinion/techo5/issues) and I'll fix it. Thanks.

Every project here is good work by people who give it away, and several of them do things TECHO5
doesn't. TECHO5 itself started from EchoLocal. Where a cell says "not listed", I didn't find it in
that project's docs. It doesn't mean they can't do it.

## At a glance

| | TECHO5 | EchoLocal | EchoMuse | Kiosk Satellite | View Assist (with VACA) |
|---|---|---|---|---|---|
| **What it is** | Replacement firmware for Echos | Replacement firmware for Echos | Replacement firmware for the Echo Dot, plus a controller | Android kiosk app with a built-in voice satellite | Home Assistant integration, views and blueprints, plus an Android companion app |
| **Devices** | Echo Show 5 1st and 2nd gen, Echo Show 8 1st and 2nd gen, Echo Spot 1st gen, Echo Dot 2nd gen, Echo 2nd gen | Echo Dot 2nd gen, Echo Show 5 1st and 2nd gen, Echo Show 8 1st gen | Echo Dot 2nd gen. Echo Dot 3, Echo 2 and Echo Show 8 ports "in progress" | Android 7.0 or newer. Guides for Fire tablets, Echo Dot 2nd gen (on LineageOS 14.1), Meta Portal and Raspberry Pi 4. Its docs mention Echo Shows | Android 8 or newer for VACA. Tested on Lenovo ThinkSmart View and smart displays; community reports Fire tablets and Echo Show 5 and 8 on LineageOS. Views also work with ESPHome satellites |
| **What runs on the device** | Alpine Linux and one Go daemon (`echod`). No Android | One Go daemon (`echod`) in place of Amazon's software | emOS (its own small Linux userspace on the Dot's kernel), or rooted Fire OS 5 | Android app | Android app (VACA) and a browser showing the views |
| **Install route** | Unlock (amonet, or saiyaku-athena for the Show 8 2nd gen), then one Python install script over USB, on Windows, Linux or macOS. Nothing to build | Unlock to TWRP, then the `echolocal` installer over USB | Unlock with firebreak (amonet-biscuit), then a setup wizard in Chrome or Edge over USB | Install the APK. On an Echo it needs an unlocked device running LineageOS | Install the APK and the HACS integration. On an Echo Show it needs LineageOS |
| **Wake word on the device** | Yes, microWakeWord: twelve words plus your own models; two wake words at once, each with its own assistant. A "stop" word with no wake word. Can also hand the room to Home Assistant's wake word engine (openWakeWord, say), or ask it to confirm | Yes, microWakeWord and openWakeWord, two assistants with their own wake word | Yes by default, or on the controller per Dot. Custom words trained with oww_forge | Yes, vsWakeWord, microWakeWord and openWakeWord, works with the screen off | Yes in VACA, six built-in words plus your own |
| **Voice without Home Assistant** | Yes, if you want it: speech to text and text to speech on your own Wyoming servers, and any OpenAI-style chat model (llama.cpp, Ollama), with timers, alarms, the radio and web search | Not listed | No (it needs Home Assistant) | Not listed | No (it's a Home Assistant integration) |
| **Home Assistant link** | ESPHome API, encrypted | ESPHome API, optional HACS integration | ESPHome API (through the controller) | ESPHome API, plus a long-lived token | Custom integration; VACA uses the Wyoming protocol |
| **Radio** | Yes, Radio Browser lists and favorites, with song and cover | Not listed (media player and Sendspin) | Yes, as a Home Assistant media player | Not listed | Yes, a blueprint plays TuneIn through Music Assistant |
| **Music Assistant** | Yes, Sendspin, in sync as a group | Yes, Sendspin | Yes, Sendspin (v2.18.0) | Yes, Sendspin, synchronized | Yes, through blueprints |
| **AirPlay / Spotify Connect** | Both, on the Show and the Dot (not the Spot). Off until you turn them on | Not listed. Has a Chromecast receiver (audio only on the Dot) | Not listed | AirPlay yes, Spotify Connect not listed | Not listed |
| **Bluetooth** | Audio out to earbuds or a speaker, a phone playing in (Show, Spot and Dot), Home Assistant proxy that scans (no active connections yet), iBeacon on the Dot | Bluetooth speaker on the Shows, proxy with iBeacon | Proxy, and up to three Bluetooth connections for Home Assistant | Proxy | Not listed |
| **Other media** | DLNA speaker, video from `play_video` and DLNA, follow another player, lyrics, sleep sounds made on the device (white, pink and brown noise, rain, ocean, fire and more, in two layers), bass and treble, a sleep timer | Chromecast receiver for YouTube, Prime Video and more on the Shows, white noise | Ducks music under replies | DLNA, follow a Home Assistant or Sonos player, lyrics | Sound machine blueprint |
| **Clock and weather** | Yes, 12 clock styles, 13 themes, in seven languages, forecast, rain radar, U.S. weather alerts | Yes, eight clock faces, weather | No screen | Clock and weather screensavers | Clock and weather views |
| **Dashboards** | Drawn on the device (with a Rooms dashboard from your areas), or streamed exactly by dashcast, a server next to Home Assistant, which also shows web pages | Native dashboard, Tessera dashboards | No screen | Any Home Assistant dashboard, in its browser | Any Home Assistant dashboard and View Assist's own views |
| **Cameras** | Home Assistant and go2rtc cameras live, with sound, up to six at once. Talk through a doorbell's speaker. Reolink doorbell chime with no Home Assistant. Its own camera to Home Assistant, or to an NVR over RTSP and ONVIF (not yet on the Show 8 2nd gen) | RTSP camera with ONVIF, snapshot camera | No camera | Up to twelve live feeds; its own camera over RTSP | Camera view |
| **Photos** | Slideshow from anything Home Assistant's media library can browse (its media folder, network shares, Immich and other media sources), from Immich directly (favorites, dates and a person), or photos kept on the device from the setup page. Behind the clock or as a screensaver, whole photo with blurred sides, two tall photos side by side, date and place captions | Immich screensaver | No screen | Immich and local photos | Not found in their docs |
| **Calendar** | Yes: day and week on their hours, month, Coming up, icons, countdowns, the next event on the clock, event pop-ups. Home Assistant calendars or a calendar's own link (iCal) | Not listed | No screen | Not listed | Yes, "View Calendar" blueprint |
| **Alarms and timers** | Timers from Home Assistant (pause, add time); alarms that ring with Home Assistant down, sunrise light, light-only alarms, reminders by voice that go off on the devices you pick | Timers and alerts | Timers from Home Assistant | Timers; alarms that ring with no network, sunrise light | Alarms, reminders and timers blueprint and view |
| **Intercom** | Yes: call one device from another, from the screen or Home Assistant. Works with Home Assistant down. Optional Drop In | Yes, audio on the Dot, audio and video on the Shows | Not listed | Yes, call one kiosk or broadcast to all | Broadcast messages to other satellites |
| **Nearest device answers** | Yes, off by default (v1.2.21) | Not listed | Yes | Yes, wake word arbitration | Not listed |
| **Announcements** | Yes, from Home Assistant, or device to device with no Home Assistant at all: typed words or your own voice in every room | Yes | Yes | Yes | Yes |
| **Phone calls** | Through your own SIP provider, off until signed in | Not listed | Not listed (no calling) | Not listed | Not listed |
| **Realtime voice** | Yes, OpenAI, xAI or Gemini with your own key (v1.2.22) | Not listed | Not listed | Yes, OpenAI, xAI or Gemini | Not listed |
| **Stream deck** | Yes, TECHO5 Deck on the Shows: OBS scenes, stream, record and mute, plus shortcuts, apps and scripts on your computer | Not listed | No screen | Not listed | Not listed |
| **Presence and gestures** | Presence from the camera (screen off when nobody's near), person detection, a palm over the camera, a double clap, a hold on the clock. Room loudness and light sensors. All off by default (camera ones not yet on the Show 8 2nd gen) | Not listed | Not listed | Finger-count and clap gestures | Not listed |
| **Settings across devices** | Link any everyday setting with the devices you pick, copy once, or save to a file and restore. No main device | Config sync | The controller | Fleet profiles | Set in Home Assistant |
| **Remote API** | Signed, off by default, can't touch privacy settings | Not listed | Not listed | Yes (Remote API) | Not listed |
| **Updates** | Signed, A/B with fallback, from Home Assistant's update card; stable or dev channel; automatic between 2 and 5 AM if you want | From Home Assistant, stable or development channel | Over Wi-Fi from its dashboard, with automatic rollback | From GitHub, with a Home Assistant update entity | Integration through HACS |
| **License** | MIT | MIT | MIT | CC BY-NC-ND 4.0 (free for personal, non-commercial use) | View Assist: CC BY-NC 4.0. Integration repo: no license file found. VACA: Apache 2.0 |
| **Latest release** | v1.2.23, October 18, 2026 | 0.0.9, October 15, 2026 | v2.18.1, October 15, 2026 | 2026.10.29, October 18, 2026 | VACA v0.13.5, October 10, 2026; integration 2026.7.0, July 8, 2026 |

A few related projects, for context:

| | What it is |
|---|---|
| [Home Assistant Voice Preview Edition](https://github.com/esphome/home-assistant-voice-pe) | Home Assistant's own voice satellite hardware, on ESPHome. No screen. Latest firmware 26.9.0, September 17, 2026 |
| [Linux Voice Assistant](https://github.com/OHF-Voice/linux-voice-assistant) | A voice satellite for any Linux machine over the ESPHome API. Apache 2.0. v1.1.15, August 2, 2026 |
| [Wyoming Satellite](https://github.com/rhasspy/wyoming-satellite) | The older Wyoming voice satellite. Archived; its README points to Linux Voice Assistant instead |

## Where others do better or differently

Being honest about this matters more to me than the table.

- **Any Android device.** Kiosk Satellite runs on anything with Android 7, and View Assist works with
  any satellite, ESPHome ones like Voice PE included. TECHO5 only runs on Echos, and needs the unlock
  first. The apps on an Echo Show need the same unlock plus LineageOS.
- **Any dashboard, as is.** The Android apps run a real browser, so custom cards just work.
  TECHO5 draws dashboards itself, which is fast but covers the common cards, or streams them
  exactly through dashcast, which needs a small server next to Home Assistant.
- **Kiosk Satellite** and TECHO5 now cover nearly the same ground. What Kiosk Satellite still has:
  a plugin manager (TECHO5 leaves third-party code out on purpose), finger-count gestures, short
  Immich videos in the slideshow and twelve camera feeds at once (TECHO5 shows six). As an Android
  app it also has Android things like a lockdown mode. And it moves fast: a new release most days.
- **View Assist** has a big library of ready-made voice blueprints: lists, sports scores, travel
  times, Wikipedia, where a person is, and more. TECHO5 has a handful of ready-made automations in
  [docs/actions.md](https://github.com/HuskerMinion/techo5/blob/main/docs/actions.md).
- **EchoLocal**, where TECHO5 started, has a Chromecast receiver (YouTube, Prime Video and more on
  the Shows), openWakeWord on the device as well as microWakeWord (TECHO5 can only hand the room to
  Home Assistant's openWakeWord), GPU audio visuals and a turn history in its own integration
  (TECHO5 has recordings and per-turn events, not a history page). TECHO5 has no Chromecast
  receiver.
- **EchoMuse** goes deep on the Dot: a controller with a web dashboard and voice history, the
  choice to detect the wake word on the controller (TECHO5 can hand it to Home Assistant's engine
  instead), a trainer for your own wake words (TECHO5 has a guide and keeps clips from your room,
  not a tool), a ten-second way back to the original boot image, and Bluetooth connections for Home
  Assistant, where TECHO5's proxy only scans for now.
- **License.** TECHO5, EchoLocal and EchoMuse are MIT. Kiosk Satellite (CC BY-NC-ND) and View
  Assist (CC BY-NC) are free to use at home but limit commercial use or modified builds. That only
  matters if you want to change and share their code. TECHO5 copies nothing from either.

What TECHO5 does that I didn't find elsewhere:

- The Echo Show 8 2nd gen, on its own mainline kernel, and the Echo Spot. No Android on any of
  them, just one program on a small Linux.
- Voice with no Home Assistant at all, if you want it: your own speech servers and a local chat
  model.
- Phone calls through your own SIP provider.
- TECHO5 Deck, a stream deck for OBS and your computer on a Show.
- Talking through a doorbell's own speaker, and a Reolink doorbell chime, with no Home Assistant.
- Spotify Connect on an Echo.
- A rain radar and U.S. weather alerts on the screen.

## Choosing

- **You have an Android tablet, a Portal or a Lenovo display**: Kiosk Satellite, or View Assist
  with VACA. No unlock. Both keep your dashboards exactly as they are.
- **You want lots of voice skills on any satellite you own**: View Assist's blueprints.
- **You have Echo Dot 2nd gens and want a voice satellite with a controller**: EchoMuse, or
  EchoLocal, or TECHO5 Dot. All three are worth a look.
- **You have an Echo Show or Spot and are fine with the unlock**: TECHO5 or EchoLocal turn it into
  a Linux device with no Android left. Want Chromecast? EchoLocal. Want a calendar, radio, AirPlay
  and Spotify Connect, phone calls, an OBS deck, voice without Home Assistant, or the Show 8 2nd gen?
  TECHO5. Want to keep Android apps on it? LineageOS with Kiosk Satellite or VACA.
- **You want voice without Home Assistant**: TECHO5, with your own speech servers and a local chat
  model.
- **You want something that just works out of the box**: Home Assistant Voice Preview Edition.

## Sources

All checked October 18, 2026.

- TECHO5: [README](https://github.com/HuskerMinion/techo5),
  [docs/overview.md](https://github.com/HuskerMinion/techo5/blob/main/docs/overview.md),
  [docs/setup.md](https://github.com/HuskerMinion/techo5/blob/main/docs/setup.md),
  [docs/actions.md](https://github.com/HuskerMinion/techo5/blob/main/docs/actions.md),
  [docs/dashboards.md](https://github.com/HuskerMinion/techo5/blob/main/docs/dashboards.md),
  [docs/phone.md](https://github.com/HuskerMinion/techo5/blob/main/docs/phone.md),
  [docs/deck.md](https://github.com/HuskerMinion/techo5/blob/main/docs/deck.md),
  [docs/video.md](https://github.com/HuskerMinion/techo5/blob/main/docs/video.md),
  [docs/install-athena.md](https://github.com/HuskerMinion/techo5/blob/main/docs/install-athena.md),
  [releases](https://github.com/HuskerMinion/techo5/releases),
  [TECHO5 Spot](https://github.com/HuskerMinion/techo5-spot),
  [TECHO5 Dot](https://github.com/HuskerMinion/techo5-dot)
- EchoLocal: [README](https://github.com/ygelfand/echolocal),
  [release 0.0.9](https://github.com/ygelfand/echolocal/releases/tag/0.0.9),
  [EchoLocal integration](https://github.com/ygelfand/echolocal-hacs)
- EchoMuse: [README](https://github.com/wilbowes/EchoMuse),
  [release v2.18.0](https://github.com/wilbowes/EchoMuse/releases/tag/v2.18.0),
  [releases](https://github.com/wilbowes/EchoMuse/releases)
- Kiosk Satellite: [README](https://github.com/jxlarrea/kiosk-satellite),
  [docs](https://kiosksatellite.com/docs/),
  [Voice Satellite](https://kiosksatellite.com/docs/voice-satellite/),
  [Intercom](https://kiosksatellite.com/docs/intercom/),
  [Camera streams](https://kiosksatellite.com/docs/cameras/),
  [Updates](https://kiosksatellite.com/docs/updates/),
  [Echo Dot guide](https://kiosksatellite.com/docs/echo-dot/),
  [releases](https://github.com/jxlarrea/kiosk-satellite/releases)
- View Assist: [README](https://github.com/dinki/View-Assist),
  [docs](https://dinki.github.io/View-Assist/),
  [Views gallery](https://dinki.github.io/View-Assist/docs/extend-functionality/views),
  [Automations gallery](https://dinki.github.io/View-Assist/docs/extend-functionality/sentences),
  [integration](https://github.com/dinki/view_assist_integration)
- View Assist Companion App (VACA): [README](https://github.com/msp1974/ViewAssist_Companion_App),
  [wiki](https://github.com/msp1974/ViewAssist_Companion_App/wiki),
  [manifest (Wyoming)](https://github.com/msp1974/ViewAssist_Companion_App/blob/main/custom_components/vaca/manifest.json)
- Context: [Home Assistant Voice PE](https://github.com/esphome/home-assistant-voice-pe),
  [Linux Voice Assistant](https://github.com/OHF-Voice/linux-voice-assistant),
  [Wyoming Satellite](https://github.com/rhasspy/wyoming-satellite)
