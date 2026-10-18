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

| | TECHO5 | EchoLocal | EchoMuse | echo-dot-assist | Kiosk Satellite | View Assist (with VACA) |
|---|---|---|---|---|---|---|
| **What it is** | Replacement firmware for Echos | Replacement firmware for Echos | Replacement firmware for the Echo Dot, plus a controller | Keeps the Echo's own Fire OS and swaps only Alexa for its own small program (`hassmic`) | Android kiosk app for Home Assistant displays, with a built-in voice satellite. Also runs with no screen on an Echo Dot | Home Assistant integration, views and blueprints, plus an Android companion app |
| **Devices** | Echo Show 5 1st and 2nd gen, Echo Show 8 1st and 2nd gen, Echo Spot 1st gen, Echo Dot 2nd gen, Echo 2nd gen | Echo Dot 2nd gen, Echo Show 5 1st and 2nd gen, Echo Show 8 1st gen | Echo Dot 2nd gen. Echo Dot 3, Echo 2 and Echo Show 8 ports "in progress" | Echo Dot 3rd gen (2018, D9N29T), Echo Dot 2nd gen, Echo 2nd gen. Not the 2019–2020 Dot 3 or the Dot 3 with a clock | Anything with Android 7.0 or newer. Guides for Fire tablets, Meta Portals, the Raspberry Pi 4 and the Echo Dot 2nd gen (on its own LineageOS image). Its docs also name the Echo Show 5 and Show 8 on LineageOS, and tablets like the Galaxy Tab and Lenovo Tab. No Echo Spot or Echo 2nd gen listed | Android 8 or newer for VACA. Tested on Lenovo ThinkSmart View and smart displays; community reports Fire tablets and Echo Show 5 and 8 on LineageOS. Views also work with ESPHome satellites |
| **What runs on the device** | Alpine Linux and one Go daemon (`echod`). No Android | One Go daemon (`echod`) in place of Amazon's software | emOS (its own small Linux userspace on the Dot's kernel), or rooted Fire OS 5 | Stock Fire OS with Amazon's audio front end and wake word engine, plus `hassmic`, a C daemon. A firewall keeps Amazon out | Android app | Android app (VACA) and a browser showing the views |
| **Install route** | Unlock (amonet, or saiyaku-athena for the Show 8 2nd gen), then one Python install script over USB, on Windows, Linux or macOS. Nothing to build | Unlock to TWRP, then the `echolocal` installer over USB | Unlock with firebreak (amonet-biscuit), then a setup wizard in Chrome or Edge over USB | Unlock and root (micro-USB on the Dot 2, test pads on the Dot 3, soldering on the Echo 2), then one guided setup script on a Linux PC (not a VM or WSL). Each model needs one exact firmware | Install the APK and follow its setup wizard, on the device or from a browser. On an Echo it needs an unlocked device running LineageOS | Install the APK and the HACS integration. On an Echo Show it needs LineageOS |
| **Wake word on the device** | Yes, microWakeWord: twelve words plus your own models; two wake words at once, each with its own assistant. A "stop" word with no wake word. Can also hand the room to Home Assistant's wake word engine (openWakeWord, say), or ask it to confirm | Yes, microWakeWord and openWakeWord, two assistants with their own wake word | Yes by default, or on the controller per Dot. Custom words trained with oww_forge | Yes, Amazon's own (Alexa, and Echo, Computer and others downloaded from Amazon), with "Alexa, stop". Or microWakeWord (experimental), or Home Assistant's openWakeWord. Two wake words, each with its own assistant | Yes, vsWakeWord, microWakeWord and openWakeWord, with your own models for all three. Two wake words, each with its own assistant. A "stop" word. Works with the screen off or another app in front | Yes in VACA, six built-in words plus your own |
| **Voice without Home Assistant** | Yes, if you want it: speech to text and text to speech on your own Wyoming servers, and any OpenAI-style chat model (llama.cpp, Ollama), with timers, alarms, the radio and web search | Not listed | No (it needs Home Assistant) | No (it needs Home Assistant) | Partly: a realtime conversation goes straight to OpenAI, xAI or Gemini (or a relay on your network). The app itself expects Home Assistant | No (it's a Home Assistant integration) |
| **Home Assistant link** | ESPHome API, encrypted | ESPHome API, optional HACS integration | ESPHome API (through the controller) | ESPHome API, encrypted. Or Wyoming, with fewer features | ESPHome API, encrypted, plus a long-lived token | Custom integration; VACA uses the Wyoming protocol |
| **Radio** | Yes, Radio Browser lists and favorites, with song and cover | Not listed (media player and Sendspin) | Yes, as a Home Assistant media player | Not listed | Through Music Assistant | Yes, a blueprint plays TuneIn through Music Assistant |
| **Music Assistant** | Yes, Sendspin, in sync as a group | Yes, Sendspin | Yes, Sendspin (v2.18.0) | Yes, Sendspin | Yes, Sendspin, in sync, with group volume, queue and speaker groups | Yes, through blueprints |
| **AirPlay / Spotify Connect** | Both, on the Show and the Dot (not the Spot). Off until you turn them on | Not listed. Has a Chromecast receiver (audio only on the Dot) | Not listed | Not listed | AirPlay yes (since 2026.10.29). Spotify Connect not built in, but it shows and controls a Spotify app playing on the device | Not listed |
| **Bluetooth** | Audio out to earbuds or a speaker, a phone playing in (Show, Spot and Dot), Home Assistant proxy that scans (no active connections yet), iBeacon on the Dot | Bluetooth speaker on the Shows, proxy with iBeacon | Proxy, and up to three Bluetooth connections for Home Assistant | A phone playing in (SBC, AAC, aptX, aptX HD), audio out to a speaker, proxy with up to three connections | Proxy with active connections (locks, buttons, blinds), two or three at once | Not listed |
| **Other media** | DLNA speaker, video from `play_video` and DLNA, follow another player, lyrics, sleep sounds made on the device (white, pink and brown noise, rain, ocean, fire and more, in two layers), bass and treble, a sleep timer | Chromecast receiver for YouTube, Prime Video and more on the Shows, white noise | Ducks music under replies | Separate volumes for music, replies, sounds, timers and calls; Amazon's equalizer (bass, mid, treble); extra speaker gain | DLNA for pictures, video and audio; follow a Home Assistant, Music Assistant or Sonos player, or another app on the device; lyrics; audio quality | Sound machine blueprint |
| **Clock and weather** | Yes, 12 clock styles, 13 themes, in seven languages, forecast, rain radar, U.S. weather alerts | Yes, eight clock faces, weather | No screen | No screen | Digital, flip and roller clocks with a night mode, an animated weather screensaver, weather and entity widgets | Clock and weather views |
| **Dashboards** | Drawn on the device (with a Rooms dashboard from your areas), or streamed exactly by dashcast, a server next to Home Assistant, which also shows web pages | Native dashboard, Tessera dashboards | No screen | No screen | Any Home Assistant dashboard, in its browser, with view rotation. Also your own HTML pages, and dashboard buttons that open its features or other apps | Any Home Assistant dashboard and View Assist's own views |
| **Screensaver and night** | Night hours (dark, a faint glow or a red night clock), photos behind the clock or as a screensaver, brightness that follows the room's light | Immich screensaver | No screen | No screen | Eleven screensaver modes (clock, weather, photos, cameras, a web page, a dashboard and more), a schedule by time of day, screen off after a while, brightness from its own or a Home Assistant light sensor | Not listed |
| **Cameras** | Home Assistant and go2rtc cameras live, with sound, up to six at once. Talk through a doorbell's speaker. Reolink doorbell chime with no Home Assistant. Its own camera to Home Assistant, or to an NVR over RTSP and ONVIF (not yet on the Show 8 2nd gen) | RTSP camera with ONVIF, snapshot camera | No camera | No camera | Up to twelve live feeds per view, imported from Home Assistant or go2rtc, with sound and pinch zoom. Its own camera to Home Assistant, or over RTSP and ONVIF | Camera view |
| **Photos** | Slideshow from anything Home Assistant's media library can browse (its media folder, network shares, Immich and other media sources), from Immich directly (favorites, dates and a person), or photos kept on the device from the setup page. Behind the clock or as a screensaver, whole photo with blurred sides, two tall photos side by side, date and place captions | Immich screensaver | No screen | No screen | Immich (albums, people, tags, dates, short videos), Home Assistant's media library and photos on the device. Two photos side by side, photo details | Not found in their docs |
| **Calendar** | Yes: day and week on their hours, month, Coming up, icons, countdowns, the next event on the clock, event pop-ups. Home Assistant calendars or a calendar's own link (iCal) | Not listed | No screen | No screen | No calendar screen of its own. Home Assistant's calendar cards work on its dashboards | Yes, "View Calendar" blueprint |
| **Alarms and timers** | Timers from Home Assistant (pause, add time); alarms that ring with Home Assistant down, sunrise light, light-only alarms, reminders by voice that go off on the devices you pick | Timers and alerts | Timers from Home Assistant | Timers from Home Assistant, ringing up to 10 minutes or until stopped. Alarms not listed | Timers on screen (pause, add time, from Home Assistant too); alarms that ring with no network, sunrise light, a spoken line, set by voice through an AI agent | Alarms, reminders and timers blueprint and view |
| **Intercom** | Yes: call one device from another, from the screen or Home Assistant. Works with Home Assistant down. Optional Drop In | Yes, audio on the Dot, audio and video on the Shows | Not listed | Yes, Drop In between Echos, with no cloud or Home Assistant in between | Yes, audio: call one kiosk or announce to all, push to talk or hands free, from the screen, Home Assistant or by voice | Broadcast messages to other satellites |
| **Nearest device answers** | Yes, off by default (v1.2.21) | Not listed | Yes | Yes, the Echos settle it among themselves. Can also work with Kiosk Satellite tablets | Yes, wake word arbitration | Not listed |
| **Announcements** | Yes, from Home Assistant, or device to device with no Home Assistant at all: typed words or your own voice in every room | Yes | Yes | Yes | Yes | Yes |
| **Notifications on the screen** | Yes, from Home Assistant: a card with an icon, a chime, a picture or a live camera, said aloud if you want | Not listed | No screen | No screen | Yes, from Home Assistant: up to four cards at once, with an icon, a chime and a picture | Not listed |
| **Phone calls** | Through your own SIP provider, off until signed in | Not listed | Not listed (no calling) | Not listed | Not listed | Not listed |
| **Realtime voice** | Yes, OpenAI, xAI or Gemini with your own key (v1.2.22) | Not listed | Not listed | Not listed | Yes, OpenAI, xAI or Gemini, with Home Assistant's tools and web search | Not listed |
| **Stream deck** | Yes, TECHO5 Deck on the Shows: OBS scenes, stream, record and mute, plus shortcuts, apps and scripts on your computer | Not listed | No screen | No screen | Not listed | Not listed |
| **Presence** | Presence from the camera (the screen goes out when nobody's near), person detection. Off by default (not yet on the Show 8 2nd gen) | Not listed | Not listed | A motion sensor from the Wi-Fi signal (experimental) | Camera motion and face detection, a proximity sensor, the Portal's person sensor, or a Home Assistant motion sensor in the camera's place. Each can wake the screen or hold off the screensaver | Not listed |
| **Gestures** | A palm over the camera, a double clap, a hold on the clock. Off by default | Not listed | Not listed | Not listed. Button presses and holds go to Home Assistant | Corner taps and holds, two and three finger taps, a knock code, claps, fingers held up to the camera. Each runs an action you pick | Not listed |
| **Sensors for Home Assistant** | Presence, person, room loudness, what's playing, ringing alarms and timers | Not listed | Not listed | Sounds (smoke or CO alarm, breaking glass, a dog, a baby and more) from Amazon's model, a whispered request, Wi-Fi motion, room light, ringing timers | Battery, charging, CPU, memory, storage, light, room loudness, motion, person, GPS, network, the app in front, a screenshot | Not listed |
| **Kiosk lockdown** | A settings lock: a PIN before settings change, while alarms, volume and brightness stay open | Not listed | No screen | No screen | Kiosk mode (exit gesture and PIN, status bar, home and volume keys blocked), a lockdown mode for cleaning or guests, home launcher, device owner mode | Not listed |
| **Settings across devices** | Link any everyday setting with the devices you pick, copy once, or save to a file and restore. No main device | Config sync | The controller | Copy settings and wake word models Echo to Echo, or export to a file and import | Fleet management: a leader sends profiles, plugins and updates to the others. Backup and restore | Set in Home Assistant |
| **Setup in a browser** | A setup page for every setting; a screenshot of the screen when you allow it | Not listed | Setup wizard and dashboard | A settings page on each Echo, let in by a press of its action button | Remote admin for every setting, with live screenshots, logs, a file manager and backups | Not listed |
| **Remote API** | Signed, off by default, can't touch privacy settings | Not listed | Not listed | Not listed | Yes: REST and WebSocket, a JavaScript API for pages, Android broadcasts | Not listed |
| **Plugins** | No | Not listed | Not listed | Not listed | Yes, a plugin manager and a plugin kit | Not listed |
| **Updates** | Signed, A/B with fallback, from Home Assistant's update card; stable or dev channel; automatic between 2 and 5 AM if you want | From Home Assistant, stable or development channel | Over Wi-Fi from its dashboard, with automatic rollback | Signed, from your PC or from Home Assistant (release or beta channel); falls back if a new version fails its self test | From GitHub, with a Home Assistant update entity; for a whole fleet at once | Integration through HACS |
| **License** | MIT | MIT | MIT | MIT | CC BY-NC-ND 4.0 (free for personal, non-commercial use) | View Assist: CC BY-NC 4.0. Integration repo: no license file found. VACA: Apache 2.0 |
| **Latest release** | v1.2.23, October 18, 2026 | 0.0.9, October 15, 2026 | v2.18.1, October 15, 2026 | 2026.10.17.151309, October 17, 2026 (beta builds through October 18, 2026) | 2026.10.29, October 18, 2026 | VACA v0.13.5, October 10, 2026; integration 2026.7.0, July 8, 2026 |

A few related projects, for context:

| | What it is |
|---|---|
| [Home Assistant Voice Preview Edition](https://github.com/esphome/home-assistant-voice-pe) | Home Assistant's own voice satellite hardware, on ESPHome. No screen. Latest firmware 26.9.0, September 17, 2026 |
| [Linux Voice Assistant](https://github.com/OHF-Voice/linux-voice-assistant) | A voice satellite for any Linux machine over the ESPHome API. Apache 2.0. v1.1.15, August 2, 2026 |
| [Wyoming Satellite](https://github.com/rhasspy/wyoming-satellite) | The older Wyoming voice satellite. Archived; its README points to Linux Voice Assistant instead |

## Where others do better or differently

Being honest about this matters more to me than the table.

- **Any Android device.** Kiosk Satellite runs on anything with Android 7: tablets, Fire tablets,
  Meta Portals, a Raspberry Pi 4, and Echo Shows on LineageOS. It even runs with no screen on an
  Echo Dot 2nd gen. View Assist works with any satellite, ESPHome ones like Voice PE included.
  TECHO5 only runs on Echos, and needs the unlock first. The apps on an Echo Show need the same
  unlock plus LineageOS.
- **Any dashboard, as is.** The Android apps run a real browser, so custom cards just work.
  TECHO5 draws dashboards itself, which is fast but covers the common cards, or streams them
  exactly through dashcast, which needs a small server next to Home Assistant.
- **Kiosk Satellite** has a lot TECHO5 doesn't:
  - Fleet management: one kiosk leads and sends profiles, plugins and updates to the rest. TECHO5
    links settings one at a time, with no profiles.
  - A plugin manager with a kit for writing plugins. TECHO5 leaves third-party code out on purpose.
  - Kiosk and lockdown modes, a home launcher and a URL filter. TECHO5 has a settings lock.
  - A remote admin in the browser with live screenshots, logs, a file manager and backups, plus
    REST, WebSocket and JavaScript APIs and Android broadcasts.
  - A Bluetooth proxy that holds connections to locks and buttons. TECHO5's proxy only scans.
  - Many more gestures: corner taps, a knock code, fingers held up to the camera, each running any
    action. And presence from a face, a proximity sensor, or a Home Assistant motion sensor.
  - Eleven screensaver modes on a schedule, including an animated weather sky, a web page and a
    dashboard.
  - Twelve camera feeds at once (TECHO5 shows six), short Immich videos in the slideshow, and
    following a Sonos or another app's music directly.
  - More sensors for Home Assistant: battery, CPU, memory, storage and GPS.
  - Alarms and intercom calls by voice through an AI agent, and fourteen looks for the voice
    overlay.

  TECHO5 also has the nearest device answering, two wake words, realtime voice, AirPlay, Sendspin,
  DLNA, sunrise alarms, the intercom, notifications, camera presence and a room loudness sensor.
  And Kiosk Satellite moves fast: a new release most days.
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
- **echo-dot-assist** takes another road: it keeps Amazon's Fire OS and swaps only Alexa for its
  own program. That buys it a lot:
  - Amazon's own echo canceling, beamforming and wake words stay. They were made for these
    microphones, so it hears you across the room and over its own music. TECHO5 drops Amazon's
    software and uses its own echo canceling and wake word engine.
  - The Echo Dot 3rd gen (2018), which TECHO5 doesn't run on.
  - Sound detection on the device (a smoke or CO alarm, breaking glass, a dog, a baby) from
    Amazon's own model, and whisper detection, so your assistant can answer in a whisper. Its docs
    call sound detection a hint, not an alarm.
  - AAC, aptX and aptX HD from a phone over Bluetooth, and a Bluetooth proxy with connections.
  - A motion sensor from the Wi-Fi signal (experimental).
  - Separate volumes for music, replies, sounds, timers and calls.

  The trade-off, plainly: Amazon's software still runs on the Echo, kept off the internet by a
  firewall, and each model needs one exact firmware (Fire OS 6574.1 on the Dot 3). The install
  needs a Linux PC, and the Dot 3 and Echo 2 need the case opened. In Wyoming mode most of the
  extras go. TECHO5 leaves none of Amazon's software on the device.
- **License.** TECHO5, EchoLocal, EchoMuse and echo-dot-assist are MIT. Kiosk Satellite (CC BY-NC-ND) and View
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

- **You have an Android tablet, a Fire tablet, a Portal, a Lenovo display or a Raspberry Pi**:
  Kiosk Satellite, or View Assist with VACA. No unlock. Both keep your dashboards exactly as they
  are.
- **You run several displays and want them managed as one, or locked down for guests**: Kiosk
  Satellite.
- **You want lots of voice skills on any satellite you own**: View Assist's blueprints.
- **You have Echo Dot 2nd gens**: EchoMuse (with a controller), EchoLocal, TECHO5 Dot or
  echo-dot-assist. All four are worth a look. Want Amazon's own wake word and echo canceling kept?
  echo-dot-assist. Kiosk Satellite runs on the Dot too.
- **You have an Echo Dot 3rd gen from 2018**: echo-dot-assist. TECHO5 doesn't run on it, and
  EchoMuse lists its port as in progress.
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
- echo-dot-assist: [README](https://github.com/Gamer92000/echo-dot-assist),
  [user guide](https://github.com/Gamer92000/echo-dot-assist/blob/main/docs/GUIDE.md),
  [devices](https://github.com/Gamer92000/echo-dot-assist/blob/main/devices/README.md),
  [changelog](https://github.com/Gamer92000/echo-dot-assist/blob/main/CHANGELOG.md),
  [releases](https://github.com/Gamer92000/echo-dot-assist/releases)
- Kiosk Satellite: [README](https://github.com/jxlarrea/kiosk-satellite),
  [docs](https://kiosksatellite.com/docs/),
  [Voice Satellite](https://kiosksatellite.com/docs/voice-satellite/),
  [Custom wake words](https://kiosksatellite.com/docs/custom-wake-words/),
  [ESPHome](https://kiosksatellite.com/docs/esphome/),
  [Events](https://kiosksatellite.com/docs/events/),
  [Android broadcasts](https://kiosksatellite.com/docs/android-broadcasts/),
  [Custom pages](https://kiosksatellite.com/docs/custom-pages/),
  [Dashboard links](https://kiosksatellite.com/docs/dashboard-links/),
  [Screen](https://kiosksatellite.com/docs/screen/),
  [Screensavers](https://kiosksatellite.com/docs/screensavers/),
  [Immich](https://kiosksatellite.com/docs/immich/),
  [Media player](https://kiosksatellite.com/docs/sendspin/),
  [DLNA](https://kiosksatellite.com/docs/dlna/),
  [Intercom](https://kiosksatellite.com/docs/intercom/),
  [Camera streams](https://kiosksatellite.com/docs/cameras/),
  [Device camera](https://kiosksatellite.com/docs/camera/),
  [Kiosk and lockdown](https://kiosksatellite.com/docs/kiosk/),
  [Home launcher](https://kiosksatellite.com/docs/home-launcher/),
  [Gestures](https://kiosksatellite.com/docs/gestures/),
  [Alarms](https://kiosksatellite.com/docs/alarms/),
  [Fleet management](https://kiosksatellite.com/docs/fleet/),
  [Updates](https://kiosksatellite.com/docs/updates/),
  [Remote API](https://kiosksatellite.com/docs/remote-api/),
  [JavaScript API](https://kiosksatellite.com/docs/js-api/),
  [Plugins](https://kiosksatellite.com/docs/plugins/),
  [Fire tablets](https://kiosksatellite.com/docs/fire/),
  [Meta Portal](https://kiosksatellite.com/docs/portal/),
  [Raspberry Pi 4](https://kiosksatellite.com/docs/raspberry-pi/),
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
