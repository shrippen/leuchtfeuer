# Architecture

How this repository is meant to be built. Where the code differs today is listed under
"Known deviations". Change this file in the PR that changes the structure. How the
installed system runs (boot, audio path, services, updates) is described in
`docs/ARCHITECTURE.md`.

## What it does

Leuchtfeuer turns a small computer with a speaker into a smart speaker: audio receivers
(Spotify, AirPlay, Cast, UPnP, Bluetooth and others), web radio, alarms, a morning
briefing, a web interface and Home Assistant. One shared platform runs on every device;
a device adds only what it has (target `invoke`: Harman Kardon Invoke; `generic`: any
Linux).

## Building blocks

| Path | Its one task |
|---|---|
| `src/leuchtfeuerd/` | The service on the speaker, one Go program, one file per topic: alarms and timers (`sched`), sources, briefing with calendar, announcements, sound, light ring, voice satellite, other speakers, updates, backup, HTTPS, access. |
| `src/leuchtfeuerd/web.go`, `mqtt.go`, `bus.go`, `events.go` | The ways in: the HTTP API with the web interface, Home Assistant over MQTT, the local bus for the own receivers, the events of foreign receivers. |
| `src/leuchtfeuerd/app.go` | The core's state, the small interfaces it works against (volume, player, LED) and the one place that runs a named action (`Do`). |
| `src/leuchtfeuerd/store.go`, `cfg.go` | The settings: the service's own (JSON) and the shell configuration the hook shares. |
| `src/leuchtfeuerd/target.go`, `target_<id>.go` | What a device provides: volume (required), and the extensions buttons, light ring, vendor sounds. The only Go files that know a device. |
| `src/leuchtfeuerd/web/` | The web interface in the browser: `app.js` (the pages), `roomeq.js` (analysis of the room measurement), login. `kante/` is a synced copy of the design system, never edited here. |
| `src/castrecv/`, `src/btagent/` | The own receivers: Cast, and the Bluetooth agent (pairing, volume, media keys). |
| `src/lfbus/`, `src/wamp/`, `src/relsign/` | The client of the local bus; the client for the Invoke's vendor software; signing and checking a release. |
| `device/leuchtfeuer/` | The shared part installed on the device: the hook (services, firewall, logs, updates, watchdog), the audio chain, one start script per receiver in `services/`, update and migration. |
| `device/src/` | The audio plug-ins in C: equalizer, the tap for the visualizer, two shims. |
| `targets/<id>/` | The device's part: its hook functions (`target.sh`), audio output, own services, packaging, optional modules (Tidal). |
| `custom_components/leuchtfeuer/` | The Home Assistant integration (installed through HACS). |
| `tools/`, `build.sh`, `scripts/` | Building each program (`build-*.sh`, Docker toolchains), the release package, Kante sync, the structure check (synced copy); assembling and checking an installation. The Invoke's tools for flashing and backup lie here too, beside the shared ones. |
| `install.sh`, `uninstall.sh` | Installing on and removing from the Invoke over USB or network. An exception at the top level, where the guides point; `generic` has `targets/generic/setup.sh`. |
| `tests/` | Everything that runs without a speaker (`run.sh`); Go tests lie beside their file. |
| `demo/`, `docs/` | The demo world for screenshots; guides, the API, the landing page. |

## Layers

```
 browser (web/)   Home Assistant            castrecv, btagent   librespot, shairport
      │           (integration)   (MQTT)         │ src/lfbus          │
      ▼                │            │            ▼                    ▼
   web.go: HTTP API ◄──┘         mqtt.go      bus.go             events.go
      └───────────────────┬─────────┴────────────┴───────────────────┘
                          ▼
   the core: app.go (state, named actions) and one file per topic
                          │                         │
                          ▼                         ▼
   volume, player, LED, clock (interfaces)    store.go, cfg.go (settings)
                          │
                          ▼
   target_<id>.go ─► the device (Invoke: vendor software over src/wamp; generic: ALSA)

 hook.sh ─► services/*.sh (shared) + targets/<id>/ (target.sh, services) ─► the programs
```

- The shared part knows no device. What one device needs stands in `target_<id>.go`
  and `targets/<id>/`; the rest asks the target what it has and shows only that.
  The installer and the flash tools of the Invoke are the exception named above.
- The own receivers talk to the service over the local bus only, so they run on every
  device unchanged. They set the device's volume through the bus, never at a mixer
  or the vendor software.
- The web interface in the browser and the Home Assistant integration know the
  service through the HTTP API in `docs/API.md` only.
- A way in translates and decides nothing: a named action runs through `Do`, whether
  it comes from a button, the web interface, MQTT or Home Assistant.
- What a setting may be is checked beside the setting, one check per section; every
  way that changes it calls that check. A failed save is logged or reported.
- A list both sides need (action names, the fields of a DNS provider) stands once in
  the service; the browser gets it from the API, and a test holds the copy in the
  Home Assistant integration equal.
- The access to an outside service (Home Assistant, a calendar, a DNS provider) is
  one block each, used by everyone who needs it.
- The core works against the small interfaces and an injected clock, so its rules
  run in tests without a speaker.
- One file per topic. The web interface has one file per page; the page brings its
  own structure, controls and live update.
- Optional things (a receiver, Tidal) are a service or a module of their own that can
  be left out; the web interface is always there.
- Demo code is in the build only with the tag `demo`; the release build stops if it
  finds the demo marker.

## Where new things go

| Change | Touch |
|---|---|
| A new device | `src/leuchtfeuerd/target_<id>.go`, `targets/<id>/`, `docs/TARGETS.md` |
| A new receiver | Its start script in `device/leuchtfeuer/services/` (or `targets/<id>/services/`), `tools/build-<name>.sh` and `build.sh`, the packaging of each target (`targets/<id>/assemble.sh`, `targets/generic/setup.sh`, `install.sh`), its name in `mixer.go`, the audio chain; until they are derived, its labels in `web/app.js` and `custom_components/leuchtfeuer/` |
| A new named action | `Do` and the list of names in `app.go`; `docs/API.md`; until they are derived, the labels in `web/app.js` and the lists in `custom_components/leuchtfeuer/` |
| A new setting | `store.go` (field and default), its check, the page in `web/`, `docs/API.md` |
| A new address of the API | `web.go`, `docs/API.md`; for Home Assistant also `custom_components/leuchtfeuer/api.py` |
| A new source for the briefing | `briefing.go`, with a test; its editor in `web/app.js`, `docs/API.md` |
| A new DNS provider for HTTPS | `https.go` (a file `dns_<name>.go` where no library has it), with a test |
| Something only the Invoke has | `target_invoke.go`, `targets/invoke/` |
| A design element that is missing | Kante, then `tools/sync-kante.sh`; never `web/kante/` here |
| A release | The steps in `RELEASE.md` |

## Known deviations

- The action names stand nine times by hand, with no test; MQTT runs four actions
  itself instead of through `Do` (`AUDIT.md`, finding 1). The web interface does the
  same for alarm stop and snooze (not in `AUDIT.md`).
- The names of the sources stand in the service, twice in `web/app.js` and in the
  Home Assistant integration, with no test (not in `AUDIT.md`).
- Shared code knows the Invoke in three places: `update.go` lists its files,
  `apply-update.sh` restarts its vendor service, and `wampx.go` with a field in
  `app.go` holds the link to its vendor software (not in `AUDIT.md`).
- A failed save of the settings goes unnoticed in seven places (`AUDIT.md`, finding 2).
- The access to Home Assistant and the calendar fetch live inside the briefing; the
  setup help reaches into it or rebuilds it (`AUDIT.md`, finding 3).
- The Cast receiver sets the volume at the Invoke's mixer itself when the bus is down
  (`AUDIT.md`, finding 4).
- The checks of all settings are one function in `web.go`; other ways repeat single
  limits (`AUDIT.md`, finding 5).
- What a DNS provider needs stands in three places (`AUDIT.md`, finding 6).
- `web/app.js` holds all pages in one file of 1756 lines (`AUDIT.md`, finding 7).
- `web.go` lists all addresses in one function; `briefing.go` fetches and plays in
  one file (`AUDIT.md`, finding 8).
