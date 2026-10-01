# HK Invoke Hack

Turn a **Harman Kardon Invoke** (the Cortana smart speaker Harman abandoned) into a plain network
speaker. After the install the speaker shows up as **"HK Invoke"** in:

| What | How | Notes |
|---|---|---|
| **Spotify Connect** | [librespot](https://github.com/librespot-org/librespot) | needs a Spotify Premium account |
| **UPnP / DLNA renderer** | [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect) | works with e.g. Symfonium, BubbleUPnP, foobar2000 |
| **Sendspin** (Music Assistant) | [sendspin-go](https://github.com/Sendspin/sendspin-go) | "legacy" (unencrypted) protocol dialect, see below |
| **AirPlay** (AirPlay 1) | [shairport-sync](https://github.com/mikebrady/shairport-sync) 3.3.9 | iPhone, iPad, Mac; AirPlay 2 and multi-room are not supported |
| **Google Cast** (audio) | own receiver emulation (`src/castrecv`) | only for senders that do not verify the device with Google: Music Assistant, Home Assistant, VLC, pychromecast. **Not** YouTube / Spotify-Cast / Chrome tab |
| **Tidal Connect** (optional) | proprietary iFi binary | **not licensed for this device**, can be revoked – opt-in, see [Legal](#legal-and-risks) |
| **Bluetooth A2DP sink** | BlueZ 5.50 + bluez-alsa + own agent (`src/btagent`) | invisible until you press the speaker's **Bluetooth button** (2-minute pairing window, ring feedback); pairs without a prompt, remembers pairings, phone volume and the speaker's volume knob stay in sync |

**Web interface** (port 80, [Kante](https://github.com/shrippen/shrippen.github.io) design, English/German):
status, **web radio**, **alarms** (fade-in, snooze, radio or beeps) and **timers**, **button mapping**, the **Wi-Fi guard**
(measures the real link quality to your router and switches to a better access point of the same network when it stays
poor), **Home Assistant** (MQTT discovery: volume, mute, web radio, Bluetooth pairing, timers, alarm buttons, temperature,
Wi-Fi, button events), a **light ring visualizer** (spectrum, level or pulse, colour and brightness adjustable) and
settings. Alarms and timers use your time zone and the light ring's own animations.

Everything plays through the speaker's own DSP/amplifier chain; the **volume knob** controls all sources
and stays in sync with the phone volume over Bluetooth in both directions.

Additionally: SSH with key only (own dropbear), the insecure root shell on port 5555 (adb) is closed, a firewall
only lets the needed ports in, and the Harman cloud services (Cortana, OTA updates, crash upload, dead Harman
Spotify) are switched off.

**Not a goal:** restoring Cortana, the wake word or any voice assistant.

## How it works (short)

The Invoke runs Linux 3.8 (Yocto + some Android parts) on a Marvell BG2CD. Its writable `/data` partition
survives reboots, so nothing in the read-only system image is changed after the initial flash:

1. A **rooted stock image** ("StockRoot" from [coggy9/HKHacking](https://github.com/coggy9/HKHacking)) gives
   first access. It is flashed over the service USB port with the vendor tool, **without erasing the
   device-specific `factory_setting` partition**.
2. `dnsmasq` (which runs as root at boot) is used as an **autostart hook** by a few lines in
   `/data/dnsmasq.conf`; it starts `/data/invoke/hook.sh`, a small supervisor that sets up SSH and the firewall,
   trims the Harman service list and keeps the audio services running.
3. All programs are cross-built for the device (glibc 2.23 / musl, ARMv7) with Docker and Go, see `build.sh`.

Details: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Research notes (German): [docs/RESEARCH-NOTES.md](docs/RESEARCH-NOTES.md).

## Quick start

Requirements: an Invoke, a Linux PC (developed on Arch), USB cable for the service port, `adb`, `docker`,
`go` ≥ 1.22, `curl`, `git`, `unzip`, `python3`, `socat`, `telnet`. The Invoke must be reachable in your Wi-Fi.

```sh
git clone https://git.arianw.de/shrippen/invoke-hack.git && cd invoke-hack

# 1. FIRST read docs/INSTALL.md parts 1-3: back up the NAND, flash StockRoot, put the speaker in your Wi-Fi.
# 2. install: the installer is interactive, it asks and explains every step (and builds the programs if needed:
#    Docker, 20-60 minutes once)
./install.sh
# without questions (scripts, CI): ./install.sh --non-interactive --ip <speaker-ip> --key ~/.ssh/id_ed25519.pub
```

Then pair your phone with "HK Invoke", or pick it in Spotify / your UPnP app / Music Assistant.
The complete, step-by-step guide including the hardware part is in **[docs/INSTALL.md](docs/INSTALL.md)**
([Deutsch](docs/INSTALL.de.md), [README auf Deutsch](docs/README.de.md)).

## Configuration

`/data/invoke/config` on the speaker (template: `device/invoke/config.example`): device name, the address of
your Music Assistant server for Sendspin, the DHCP host name. Re-running `./install.sh` updates the speaker and
keeps this file unless you pass `--config`.

## Emergency brake / uninstall

- `ssh root@<ip> 'touch /data/invoke/disable-hook'` and reboot: the original behaviour is back (then SSH is
  the vendor's locked sshd and adb is on again).
- `./uninstall.sh --ip <ip> --key <pub>` removes the autostart hook (`--purge` also deletes `/data/invoke`).

## Legal and risks

- **You can brick the speaker.** Flashing NAND over USB is at your own risk. Take the backup in
  [docs/INSTALL.md](docs/INSTALL.md) part 1 first and keep it; the `factory_setting` partition (certificates,
  MAC, calibration) exists only in your speaker and in that backup. Never run `l2nand 83` without `-m`.
- This project **does not contain Harman or Marvell firmware**; `scripts/fetch.sh` downloads it from the public
  releases of coggy9/HKHacking. Do not redistribute your NAND backup (it contains your device keys).
- **Tidal Connect** uses the proprietary `tidal_connect_application` of iFi audio with iFi's device certificate
  (from [TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker)). That is not licensed
  for this speaker and Tidal may block it any time. It is opt-in: `./build.sh --no-tidal` and
  `./install.sh --no-tidal` leave it out.
- The Cast receiver is an **independent emulation** of the Cast protocol, no Google software.
- Sendspin currently connects with the legacy (unencrypted) dialect; Music Assistant shows a notice.
- The programs installed on the speaker (librespot, gmrender-resurrect, sendspin-go, BlueZ, bluez-alsa, dropbear,
  FFmpeg/avahi for the Tidal bundle, …) keep their own licences. `build.sh` fetches the exact sources by tag/commit.

## License

MIT, see [LICENSE](LICENSE). This covers the code and documentation of this repository; the programs it builds
and installs (librespot, gmrender-resurrect, BlueZ, …) and the vendor firmware keep their own licences and terms.

## Credits

[coggy9/HKHacking](https://github.com/coggy9/HKHacking) (StockRoot image, flashing procedure),
[librespot](https://github.com/librespot-org/librespot), [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect),
[Sendspin](https://github.com/Sendspin/sendspin-go), [BlueZ](https://www.bluez.org/),
[bluez-alsa](https://github.com/arkq/bluez-alsa), [dropbear](https://matt.ucc.asn.au/dropbear/dropbear.html),
[TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker).
