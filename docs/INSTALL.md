# Installation guide

Leuchtfeuer runs on any small computer with a speaker: a Raspberry Pi with a sound HAT or a USB DAC, a mini PC on the
hi-fi amplifier, an old laptop. This guide installs it on **any Linux with systemd and ALSA** (target `generic`).
[Deutsch](INSTALL.de.md)

For the **Harman Kardon Invoke** there is a separate guide, because it needs a hardware step first:
**[INVOKE.md](INVOKE.md)**.

## What you need

- **The device:** Linux with systemd and ALSA (Raspberry Pi OS, Debian, Ubuntu, Fedora, Arch, …) on `amd64`, `arm64` or
  `armv7`, with a speaker or amplifier on a sound card (3.5 mm, HDMI, USB DAC, I²S HAT). Network, preferably wired or good
  Wi-Fi.
- **To build the package:** `git`, Go ≥ 1.22 and a C compiler for the device (`cc` on the device itself, or a cross compiler:
  `aarch64-linux-gnu-gcc` for `arm64`, `arm-linux-gnueabihf-gcc` for `armv7`). There are no ready-made release packages yet.
- **Receivers** come from your distribution (see step 3); Leuchtfeuer brings its own web interface, Cast receiver and
  Bluetooth agent.

## 1. Build the package

On your computer or directly on the device:

```sh
git clone https://git.arianw.de/shrippen/leuchtfeuer.git && cd leuchtfeuer
tools/build-generic.sh arm64                         # amd64, arm64 or armv7; without an argument: this computer's architecture
tools/make-release.sh --target generic --arch arm64  # -> dist/leuchtfeuer-<version>-generic-arm64.tar.gz
```

## 2. Install

Copy the package to the device and run `setup.sh` from it:

```sh
mkdir lf && tar -xzf leuchtfeuer-*-generic-arm64.tar.gz -C lf
sudo sh lf/setup.sh --name Kitchen --password 'your web password'
```

`setup.sh` installs to `/opt/leuchtfeuer` (`--dir` changes it), writes the settings file `config` (web interface on port
8080, `--port` changes it; time zone from the system) and enables `leuchtfeuer.service`. Without `--password` it creates a
password and writes it to `/opt/leuchtfeuer/log/leuchtfeuerd.log`.

## 3. Receivers

Leuchtfeuer starts every receiver whose program is installed; the others show up as "not installed". On Debian and
Raspberry Pi OS:

```sh
sudo apt install alsa-utils librespot shairport-sync gmediarender snapclient
```

| Receiver | Program | Notes |
|---|---|---|
| Spotify Connect | `librespot` | needs Spotify Premium |
| AirPlay 1 | `shairport-sync` | |
| UPnP / DLNA | `gmediarender` (gmrender-resurrect) | |
| Snapcast | `snapclient` | off by default; set `SNAPCAST_SERVER` |
| Sendspin (Music Assistant) | `sendspin-player` from [sendspin-go](https://github.com/Sendspin/sendspin-go/releases) | put it into `/opt/leuchtfeuer/bin/` |
| Cast (for Music Assistant, Home Assistant, VLC) | `castrecv`, part of the package | |
| Bluetooth A2DP | the system's `bluetoothd` and bluez-alsa (`sudo apt install bluez bluez-alsa-utils`) | switch on in Settings > Services; Leuchtfeuer adds its agent |

### Tidal Connect (optional module, ARM only)

Tidal Connect uses the proprietary `tidal_connect_application` of iFi audio with iFi's device certificate (from
[TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker)). It is **not licensed for your
device**, and Tidal may block it any time. That is why it is never part of a package: you build it yourself as a module
and add it only if you want it. The program exists for 32-bit ARM only, so it runs on a Raspberry Pi and similar boards
(also on 64-bit systems via multiarch), not on x86.

```sh
tools/build-tidal-bundle.sh generic && tools/make-tidal-module.sh      # -> dist/leuchtfeuer-tidal-<version>-armhf.tar.gz
# on the device:
sudo sh /opt/leuchtfeuer/setup.sh --module leuchtfeuer-tidal-*-armhf.tar.gz
sudo sh /opt/leuchtfeuer/setup.sh --remove-module tidal                # remove it again
```

The module (about 3.4 MB packed) brings only what current systems lack; the rest comes from the system. If something is
missing, `setup.sh` prints the `apt install` line (on 64-bit with `:armhf`): `libstdc++6 libasound2 libportaudio2
libavahi-client3 zlib1g libogg0`, plus `alsa-utils` and `avahi-daemon`. The speaker then appears in the Tidal app under
its name; Tidal plays into Leuchtfeuer's sound chain like every other source.

The program is from 2019 and expects old interfaces. To keep it as safe as possible, the module replaces what it can:
HTTPS goes through a current curl with mbedTLS, its own TLS uses OpenSSL 1.0.2u from Debian's security updates (SSLv3
stays off), FFmpeg 3.4.13 is built without network and TLS. The program itself cannot be updated; it listens on port
2019/tcp in your network.

## 4. First start

Open `http://<device>:8080/` and sign in. A fresh installation starts with the **setup assistant**: name, time zone and
public holidays, the place for the weather, a few web radio stations and whether you use Home Assistant. After that the
device appears under its name in Spotify, AirPlay, UPnP apps and Music Assistant.

## Settings

Most things can be set in the web interface. The file `/opt/leuchtfeuer/config` holds what is needed before the web
interface runs:

| Setting | Meaning |
|---|---|
| `ALSA_CARD` | sound card for output and volume (number or name from `aplay -l`) |
| `ALSA_OUTPUT` | set to `"pipewire"` or `"pulse"` when a sound server holds the card (desktop systems) |
| `WEB_PORT`, `WEB_TLS` | port of the web interface; HTTPS: `on` (own certificate) or `acme` (Let's Encrypt, `ACME_*`, see `config.example`; also System > HTTPS) |
| `WIFI_IFACE` | network interface (empty = the one of the default route) |
| `SERVICE_<NAME>` | which receivers run (also in Settings > Services) |
| `SNAPCAST_SERVER`, `SENDSPIN_SERVER` | servers when they are not found automatically |
| `FIREWALL` | `on` builds an iptables chain for the switched-on services (off by default) |
| `UPDATE_PUBKEY` | release signing key for updates from the web interface |

After changing the file: `sudo systemctl restart leuchtfeuer`.

Everything the web interface offers is listed in the [README](../README.md); the HTTP API is in [API.md](API.md), the Home
Assistant integration in [HOMEASSISTANT.md](HOMEASSISTANT.md).

## Update and uninstall

- **Update:** build a newer package and run its `setup.sh` again; settings, keys and pairings stay. With a release signing
  key (`UPDATE_PUBKEY`) Settings > Backup & update can install signed packages and rolls a failing update back by itself.
- **Uninstall:** `sudo sh /opt/leuchtfeuer/setup.sh --uninstall` (settings stay), `--purge` deletes them too.

## Troubleshooting

| Symptom | Check |
|---|---|
| Web interface does not open | `systemctl status leuchtfeuer`; is the port (`WEB_PORT`) taken or blocked by a firewall? |
| A receiver says "not installed" | its program is missing (table in step 3); install it, it starts within 30 s |
| No sound | `aplay -l` shows the card; `ALSA_CARD` set to it? A sound server holds the card: `ALSA_OUTPUT="pipewire"` |
| Logs | `/opt/leuchtfeuer/log/` and `/opt/leuchtfeuer/hook.log`, or Settings > Services > Log, or `journalctl -u leuchtfeuer` |
| Not found in apps | mDNS between Wi-Fi and LAN (see [INVOKE.md](INVOKE.md#discovery-across-wi-fi--lan), same for every device) |

## Another device with its own hardware

Buttons, a light ring or vendor software are **extensions** of a target. How to add a device (driver in leuchtfeuerd,
hook, audio and package): [TARGETS.md](TARGETS.md).
