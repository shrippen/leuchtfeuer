<p align="center"><img src="docs/logo.svg" alt="Leuchtfeuer" width="96" height="96"></p>

# Leuchtfeuer

_Turns a small computer with a speaker into a smart speaker_

Leuchtfeuer makes a **network and smart speaker** out of any small Linux computer with a speaker: a Raspberry Pi with a
sound HAT, a mini PC on the hi-fi amplifier, or a smart speaker whose vendor gave it up, like the **Harman Kardon Invoke**.
The shared part (receivers, web interface, alarms, Home Assistant) is the same everywhere; what a device has on top
(buttons, a light ring, its own amplifier) comes from its **target**. [Deutsch](docs/README.de.md)

| Target | Device | Guide |
|---|---|---|
| `generic` | any Linux with systemd and ALSA: Raspberry Pi, mini PC, old laptop | [docs/INSTALL.md](docs/INSTALL.md) |
| `invoke` | Harman Kardon Invoke (the abandoned Cortana speaker): volume knob, buttons, light ring, microphones | [docs/INVOKE.md](docs/INVOKE.md) |

Another device with its own hardware: [docs/TARGETS.md](docs/TARGETS.md).

## Receivers

After the install the device shows up under its name in:

| What | How | Notes |
|---|---|---|
| **Spotify Connect** | [librespot](https://github.com/librespot-org/librespot) | needs a Spotify Premium account |
| **UPnP / DLNA renderer** | [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect) | works with e.g. Symfonium, BubbleUPnP, foobar2000 |
| **Sendspin** (Music Assistant) | [sendspin-go](https://github.com/Sendspin/sendspin-go) | "legacy" (unencrypted) protocol dialect |
| **AirPlay** (AirPlay 1) | [shairport-sync](https://github.com/mikebrady/shairport-sync) | iPhone, iPad, Mac; AirPlay 2 is not supported (multiroom: Snapcast or Sendspin) |
| **Google Cast** (audio) | own receiver emulation (`src/castrecv`) | only for senders that do not verify the device with Google: Music Assistant, Home Assistant, VLC, pychromecast. **Not** YouTube / Spotify-Cast / Chrome tab |
| **Snapcast** (optional) | [snapclient](https://github.com/badaix/snapcast) | multiroom in sync with other Snapcast speakers; off by default, needs your snapserver |
| **Bluetooth A2DP sink** | BlueZ + bluez-alsa + own agent (`src/btagent`) | pairing window from the web interface, Home Assistant or a button; remembers pairings, phone volume stays in sync |
| **Tidal Connect** (optional, ARM only) | proprietary iFi binary | **not licensed for your device**, can be revoked – opt-in, never in a package: Invoke see [INVOKE.md](docs/INVOKE.md#risks), others as a module, see [INSTALL.md](docs/INSTALL.md) |

The sound can also go to another sound card (HDMI, USB) or a **Bluetooth speaker** (Settings > Output).

## Web interface

Port 80 on the Invoke, 8080 elsewhere; optional HTTPS, [Kante](https://github.com/shrippen/Kante) design, English/German,
live updates. A **setup assistant** asks for name, time zone, place, stations and Home Assistant on the first start.

- **Now playing** (title and artist from Spotify, AirPlay, Bluetooth, Cast, web radio) and volume
- **Web radio** with a **station search** (radio-browser.info) and favourites on buttons
- **Alarms** (fade-in, snooze, radio, beeps or any stream address, not on public holidays, skip once, fade out), **timers**
  and a **sleep timer**
- a **morning briefing**: greeting, weather, severe weather warnings, pollen, calendars (ICS, also waste collection), news
  such as *tagesschau in 100 Sekunden*, Home Assistant templates; spoken via Home Assistant TTS; also as an alarm sound
- **Sound** (bass, treble, loudness, night mode, applied at once) and **room correction** measured with your phone
- **Source rules** (the newest source plays, the others fade out and pause), **volume limits** and a **level trim** per source
- **Announcements** over the music (text-to-speech, door bell)
- **Home Assistant**: MQTT discovery (volume, mute, web radio, Bluetooth pairing, timers, alarm buttons, announcements, now
  playing, sensors), a [custom integration](docs/HOMEASSISTANT.md) with a real media player, and a **voice satellite** for
  Assist (Wyoming, optional, with a microphone)
- the **Wi-Fi guard** (measures the real link quality and switches to a better access point of the same network)
- **services on/off**, **backup and restore**, a **diagnostics package**, **signed updates with automatic rollback**
- an overview of **other Leuchtfeuer speakers** (copy settings, start updates)
- **API keys**, **SSH keys**, **Prometheus metrics**, a **live log**, **syslog** forwarding
- **sounds**: replace Leuchtfeuer's tones with your own files

On devices with these extensions also: **button mapping** (any button, learned by pressing it), the **light ring**
(visualizer, lamp, sunrise light, timer progress, a light in Home Assistant) and the device's **own sounds**.

The web interface sends strict security headers and checks the origin of every request. Streams reconnect by themselves;
an alarm whose station fails rings with the built-in tone. An optional **hardware watchdog** restarts a hung device.
API: [docs/API.md](docs/API.md).

**Not a goal:** a cloud voice assistant. The optional voice satellite only passes the microphone to your own Home Assistant.

## How it works (short)

A small supervisor (`hook.sh`, started by systemd or by the device's own boot) starts and watches the receivers, sets up
the firewall and SSH where the target wants it, and keeps time. **leuchtfeuerd** serves the web interface and the API and
runs alarms, timers, the briefing, Home Assistant and the volume. All sources play into **one ALSA chain** with per-source
controls, the sound plugin and the visualizer tap, which ends at the device's output. Details:
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Quick start

**Any Linux** (Raspberry Pi, mini PC), see [docs/INSTALL.md](docs/INSTALL.md):

```sh
git clone https://git.arianw.de/shrippen/leuchtfeuer.git && cd leuchtfeuer
tools/build-generic.sh arm64 && tools/make-release.sh --target generic --arch arm64
# on the device, with the package from dist/:
mkdir lf && tar -xzf leuchtfeuer-*-generic-arm64.tar.gz -C lf && sudo sh lf/setup.sh --name Kitchen
sudo apt install alsa-utils librespot shairport-sync gmediarender     # the receivers you want
```

Then open `http://<device>:8080/`.

**Harman Kardon Invoke:** back up and flash the speaker first, then `./install.sh` (it asks and explains every step).
Everything is in **[docs/INVOKE.md](docs/INVOKE.md)**.

## Development

- Tests without a device: `tests/run.sh` (Go with the race detector, shell, the sound plugin, the web interface, the Home
  Assistant integration; also run by the CI). On a device: `scripts/smoke.sh`.
- Demo mode for screenshots (no device, demo data "Studio Weber"): `demo/start.sh`.
- Releases: one package per target, signed (`tools/make-release.sh`, `.gitea/workflows/release.yml`).

## Legal

- The receivers (librespot, gmrender-resurrect, sendspin-go, shairport-sync, snapclient, BlueZ, bluez-alsa, …) keep their own
  licences; on the Invoke `build.sh` fetches the exact sources by tag or commit, elsewhere they come from your distribution.
- The Cast receiver is an **independent emulation** of the Cast protocol, no Google software.
- Sendspin currently connects with the legacy (unencrypted) dialect; Music Assistant shows a notice.
- Release packages contain no vendor firmware and no Tidal; updates from the web interface are accepted only with a valid
  Ed25519 signature (`UPDATE_PUBKEY`), and a failing update is rolled back.
- Invoke: flashing can brick the speaker, and Tidal Connect is not licensed for it – see [INVOKE.md](docs/INVOKE.md#risks).

## License

MIT, see [LICENSE](LICENSE). This covers the code and documentation of this repository; the programs it builds and installs
and any vendor firmware keep their own licences and terms.

## Credits

[coggy9/HKHacking](https://github.com/coggy9/HKHacking) (StockRoot image and flashing procedure for the Invoke),
[librespot](https://github.com/librespot-org/librespot), [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect),
[Sendspin](https://github.com/Sendspin/sendspin-go), [shairport-sync](https://github.com/mikebrady/shairport-sync),
[Snapcast](https://github.com/badaix/snapcast), [BlueZ](https://www.bluez.org/), [bluez-alsa](https://github.com/arkq/bluez-alsa),
[dropbear](https://matt.ucc.asn.au/dropbear/dropbear.html), [TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker).
