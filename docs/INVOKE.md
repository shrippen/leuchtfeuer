# Leuchtfeuer on the Harman Kardon Invoke

The **Harman Kardon Invoke** (the Cortana speaker Harman abandoned) is one target of Leuchtfeuer, and the one it was
first built for. This guide covers everything that only concerns the Invoke: the hardware access, the installer and what
is different on this speaker. Leuchtfeuer itself (receivers, web interface, alarms, Home Assistant) is described in the
[README](../README.md); other devices are installed with the [general guide](INSTALL.md). [Deutsch](INVOKE.de.md)

## What Leuchtfeuer does on the Invoke

After the install the speaker shows up as **"HK Invoke"** (change it in the web interface) and plays everything through its
own DSP and amplifier. On top of what every Leuchtfeuer device has, the Invoke target uses the hardware of the speaker:

- the **volume knob** controls all sources and stays in sync with the phone volume over Bluetooth in both directions;
- the **Bluetooth button** opens a 2-minute pairing window (the light ring reacts); otherwise the speaker is invisible,
  and it pairs without a prompt;
- the **mic button** and the other buttons can be mapped to actions (Buttons tab);
- the **light ring** works as a music visualizer, a lamp, a sunrise light for alarms and a timer display, and as a light in
  Home Assistant;
- the speaker's own **start, error and pairing sounds** can be replaced in the web interface;
- its **microphones** feed the optional voice satellite for Home Assistant (`leuchtfeuer_mic`).

It also closes the speaker: SSH with key only (own dropbear), the open root shell on port 5555 (adb) is closed, a firewall
only lets the needed ports in, and the Harman cloud services (Cortana, OTA updates, crash upload, the dead Harman Spotify)
and the vendor Bluetooth stack (replaced by BlueZ) are switched off. **Not a goal:** restoring Cortana.

**How it works (short).** The Invoke runs Linux 3.8 (Yocto + some Android parts) on a Marvell BG2CD. Its writable `/data`
partition survives reboots, so nothing in the read-only system image is changed after the initial flash:

1. A **rooted stock image** ("StockRoot" from [coggy9/HKHacking](https://github.com/coggy9/HKHacking)) gives first access.
   It is flashed over the service USB port with the vendor tool, **without erasing the device-specific `factory_setting`
   partition** (parts 1-3 below).
2. `dnsmasq` (which runs as root at boot) is used as an **autostart hook** by a few lines in `/data/dnsmasq.conf`; it starts
   `/data/leuchtfeuer/hook.sh`, the supervisor that sets up SSH and the firewall, trims the Harman service list and keeps
   the services running.
3. All programs are cross-built for the device (glibc 2.23 / musl, ARMv7) with Docker and Go (`build.sh`).

Details: [ARCHITECTURE.md](ARCHITECTURE.md); research notes (German): [RESEARCH-NOTES.md](RESEARCH-NOTES.md).

## Risks

- **You can brick the speaker.** Flashing the NAND over USB is at your own risk. Take the backup in part 1 first and keep it
  in two places; the `factory_setting` partition (certificates, MAC, calibration) exists only in your speaker and in that
  backup. Never run `l2nand 83` without `-m`.
- This project **does not contain Harman or Marvell firmware**; `scripts/fetch.sh` downloads it from the public releases of
  coggy9/HKHacking. Do not share your NAND backup (it contains your device keys).
- **Tidal Connect** (also available as a module for other ARM devices, see [INSTALL.md](INSTALL.md)) runs the
  proprietary `tidal_connect_application` of iFi audio with iFi's device certificate (from
  [TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker)). That is not licensed for this speaker, and Tidal may block it any time. It is opt-in: `./build.sh --no-tidal` and
  `./install.sh --no-tidal` leave it out, and release packages never contain it.

### The device's old system software (important)

Leuchtfeuer ships its own software up to date, but **the Invoke's operating system cannot be renewed**.
Harman's 2018 image stays, and it contains programs that have had no security fixes for years yet sit on
your Wi-Fi:

| Part | On the device | Known vulnerable |
|---|---|---|
| Kernel | 3.8.13 (2013) | many, not fixable |
| glibc | 2.23 | – (everything else builds on it) |
| wpa_supplicant | 2.5 (2015) | KRACK (CVE-2017-13077 ff.) among others |
| dnsmasq | 2.75 (2015) | CVE-2017-14491 ff., DNSpooq among others |
| OpenSSL (system) | 1.0.2h (2016) | EOL since 2019 |
| GnuTLS / libcurl / libxml2 | 3.4.9 / 7.47.1 / 2.9.4 | all EOL |
| GStreamer | 1.8.3 | EOL |
| BusyBox / dhcpcd | 1.24.1 / 5.5.6 | EOL |

Leuchtfeuer uses these parts as little as it can and brings current libraries for everything of its own
(its own curl with mbedTLS, its own mbedTLS for AirPlay, its own OpenSSL 3.5 for Snapcast, a current CA
bundle). What remains is contained by the setup: the firewall ends in `DROP` and lets in only the ports
that are needed, adbd is off, IPv6 is off, SSH takes keys only, and the vendor's update servers are
blocked through `/etc/hosts`.

**What that means for running it:** only put the speaker on a network you trust. Do not expose it to the
internet (no port forwarding, no DMZ), and prefer a guest or IoT network. This does not apply to targets
like the Raspberry Pi: there these parts come from the distribution and are updated with it.

## Installation

Complete path from a factory Invoke to a running Leuchtfeuer. The hardware parts (1-3) were done once on one device; the
software part (4) is automated by `build.sh` and `install.sh`.

> **Test status.** The update path of `install.sh` (SSH, checksum-based transfer, reboot, `verify-install.sh`) was run
> against the finished device and reproduced the state exactly. The **first-install path over adb** (part 4, "fresh
> StockRoot device") is implemented and reviewed but has **not yet been run on a factory-fresh device**; the same is
> true for `wifi-setup.sh` and `uninstall.sh` (the form fields of the Wi-Fi setup are taken from the vendor library).
> Report problems as issues. **Read everything once before you start.**

> ⚠️ Parts 1-3 write to the speaker's NAND flash. A mistake can brick it. Part 1 (backup) is mandatory.
> Never run the U-Boot command `l2nand 83` **without `-m`**: it erases the whole flash including
> `factory_setting` (your device's certificates, MAC, calibration), which exists in no firmware image.

## 0. Preparation (PC)

Linux PC. Needed: `adb`, `docker`, `go` (≥ 1.22), `curl`, `git`, `unzip`, `python3`, `socat`, `telnet`, `gh`
(GitHub CLI, only for `scripts/fetch.sh`; alternatively download the three files by hand, see below).

```sh
git clone <this repo> leuchtfeuer && cd leuchtfeuer
./scripts/fetch.sh                      # downloads the vendor flashing tool, the rooted image "StockRoot" and the OTA
                                        # from github.com/coggy9/HKHacking releases into firmware/ (not in git)
sudo cp tools/99-invoke-marvell.rules /etc/udev/rules.d/ && sudo udevadm control --reload && sudo udevadm trigger
```

Without `gh`: download `Harman.Kardon.INVOKE.Flashing.zip` (release *HarmanFlash*), `83_IMAGE` (release *StockRoot*)
and `Harman.Kardon.INVOKE.Driver.OTA2.zip` (release *FinalOTA*) into `firmware/`, then
`unzip firmware/Harman.Kardon.INVOKE.Flashing.zip -d firmware/extracted/flashing`. The StockRoot image must have
the SHA-256 `f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7`.

### Flashing mode (used in parts 1 and 2)

The Invoke does not show up on USB in normal operation. To enter the Marvell boot ROM (USB id `1286:8174`):

1. Unplug the power, connect the **service USB** port to the PC.
2. Hold the **reset pinhole** (paper clip), plug the power in, keep holding.
3. Within 5 s press the **mic-off button exactly 4 times**. The light ring turns yellow.
4. Release reset when the console/U-Boot appears in the script output.

It can take several attempts (the boot ROM sometimes resets 2-3 times before the chain runs through).

## 1. Back up the NAND (mandatory)

```sh
tools/usb-ramboot.sh backup         # start, then trigger flashing mode; boots a RAM-only kernel + ramdisk,
                                    # all partitions read-only, nothing mounted, NAND untouched
scripts/nand-backup.sh              # (second terminal) reads every partition twice over adb, compares checksums,
                                    # then the whole chip raw incl. OOB -> backup/<time>/
python3 scripts/verify-backup.py backup/<time>
```

Keep `backup/<time>/` **safe and in two places**. It contains your device keys (`factory_setting`) and any Wi-Fi
credentials in `app`; never publish it. Restoring a damaged `factory_setting` is only possible from this backup.

## 2. Flash the rooted image

```sh
tools/usb-flash.sh                  # start; then trigger flashing mode. Loads U-Boot into RAM, console in recon/
tools/uboot-send.sh "l2nand -m 83"  # flashes firmware/83_IMAGE (StockRoot 11.1842). -m = erase only the blocks
                                    # that are written. (uboot-send.sh refuses l2nand without -m)
```

Wait for `Congratulations! u2nand succeed!` in the log. Optional but recommended check that `factory_setting`,
the B partitions and the bad-block table are still identical to your backup:

```sh
tools/uboot-send.sh ramdisk         # boots the read-only ramdisk again, no reboot in between
scripts/verify-after-flash.sh backup/<time>
```

Power-cycle the speaker. The StockRoot image is the vendor's 11.1842 plus: root login, adbd on (port 5555, no
authentication), OTA servers blocked in `/etc/hosts`.

## 3. Put the speaker into your Wi-Fi

After the flash the speaker opens an unencrypted access point `HK Invoke_XXXXXX` (address 192.168.43.1). Connect
the PC to it, then:

```sh
scripts/wifi-setup.sh "<your SSID>" "<your Wi-Fi passphrase>"    # WPA2-PSK, 2.4 GHz
```

The answer `continue` means it joins your network. Put the PC back into your network and look up the
speaker's IP in the router. Check: `adb connect <ip>:5555 && adb shell id` must print `uid=0(root)`.

> The speaker is quite sensitive to Wi-Fi quality. If it sits at the edge of coverage, pings show loss and high
> latency; put it closer or bind it to a good access point (`wpa_cli` / router). Everything below works over
> the Wi-Fi, a bad link makes `install.sh` slow but it resumes (it only transfers changed files).

## 4. Install (builds the software if needed)

```sh
./install.sh
```

The installer is **interactive**: it explains each step and asks for what it needs. It
1. checks your computer (`ssh`, `tar`, `adb`),
2. asks for the speaker's IP address (find it in your router) and checks it answers,
3. lets you pick (or create) an SSH key; the **public** key is copied to the speaker, the private key stays with you,
4. finds out whether this is a first installation (over adb) or an update (over SSH),
5. asks for the settings: speaker name, address of your Music Assistant for Sendspin, DHCP host name,
6. asks whether to install **Tidal Connect** (default: no, because it is not licensed for this speaker),
7. builds the programs if they are missing (`./build.sh`: Docker + Go, 20-60 minutes the first time, results in `build/`;
   includes the two LADSPA plugins of the audio chain: visualizer tap and sound), or with `--prebuilt` downloads the
   release package instead,
8. shows a summary and asks before it changes anything,
9. on a **fresh StockRoot device**: connects over adb (port 5555), checks root, firmware and free space, installs the own
   dropbear with a per-device host key and your key, adds the autostart hook to `/data/dnsmasq.conf` (original saved as
   `dnsmasq.conf.orig`), waits for SSH (checking the host key that it generated),
10. copies all files over SSH (only changed ones), closes adb, offers a reboot, and finally runs
    `scripts/verify-install.sh`.

Messages are in English or German depending on `$LANG` (force with `LEUCHTFEUER_LANG=en|de`). Options skip questions:
`--ip`, `--key`, `--config`, `--tidal`/`--no-tidal`, `--no-reboot`, `--dry-run`. **Non-interactive** (scripts):
`./install.sh --non-interactive --ip <ip> --key <pub> [--config FILE] [--no-tidal]`. Running it again later updates the
speaker; only changed files are transferred, and your settings on the speaker are kept unless you choose new ones.
Build separately with `./build.sh [--no-tidal] [--force]`.

**Without building:** `./install.sh --prebuilt [TAG]` takes the release package from GitHub (latest release or the given
tag) instead of building: no Docker, a few minutes. It is checked against its `.sha256` and, when
`docs/release-key.pub` holds the release key and Go is installed, against its signature. The package does not contain
Tidal Connect (proprietary, not ours to hand out); build it yourself if you want it.

After the reboot the services come up within about 90 s.

## 5. Use it

| Source | How |
|---|---|
| Bluetooth | press the speaker's **Bluetooth button briefly** (the light ring reacts; pressing again closes the window), then pair "HK Invoke" on the phone within 2 minutes (no PIN). It stays paired and reconnects by itself, also while the speaker is not visible. Setting `BLUETOOTH_PAIRING="always"` keeps it permanently visible |
| Spotify | "HK Invoke" appears in the Spotify app's device list (same Wi-Fi; Premium) |
| UPnP/DLNA | pick "HK Invoke" as renderer in your UPnP app; https:// streams work (current CA bundle) |
| Music Assistant | the player appears via Sendspin (set `SENDSPIN_SERVER`) and, if the Cast provider finds it, as a Chromecast; add it by IP in the Google Cast provider's known hosts if mDNS does not cross your Wi-Fi/LAN |
| AirPlay | "HK Invoke" appears in the AirPlay menu of iPhone, iPad and Mac (AirPlay 1, audio only) |
| Web interface | `http://<speaker-ip>/` (port 80): login page, password set by `install.sh` (or a random one shown once at the end of a first install). The speaker stores only a salted PBKDF2 hash. Change it in Settings, or later with `scripts/set-web-password.sh` |
| Tidal | "HK Invoke" appears in the Tidal app's Tidal Connect list |
| Snapcast | off by default. Settings > Services > Snapcast on, and `SNAPCAST_SERVER="host"` in `/data/leuchtfeuer/config`; the speaker then joins your snapserver as a client (multiroom, in sync with the other rooms). The server should send 48000:16:2 (FLAC or PCM); set about 100 ms latency for this client in snapweb |

The volume knob sets all sources; the volume slider in Spotify, AirPlay, Cast and Bluetooth moves the same volume (UPnP
keeps its own software volume on top). Logs are in `/data/leuchtfeuer/log/` on the speaker
(`ssh root@<ip> 'tail -f /data/leuchtfeuer/log/*.log'`).

### Web interface, alarms, Home Assistant

- **Alarms/timers:** set the time zone first (Alarms & timers tab). An alarm can fade in over N seconds to a target volume, play
  beeps or a web radio station, snooze and stop itself after a limit. The timer beeps use the light ring's timer animation.
  The mic button is mapped to *smart* (snooze an alarm / end a timer / mute) by default; see the Buttons tab, which also logs
  the name and value of every button the speaker reports so you can map them.
- **Wi-Fi guard:** Network tab. It pings the router every 20 s; after two poor measurements in a row (default: 20 % loss or
  150 ms) it scans for other access points of your network, switches to the best one that is not marked poor and checks again.
  "Find access points" lists them and lets you switch by hand; *Dry run* only logs.
- **Home Assistant:** Home Assistant tab: enter your MQTT broker. The speaker registers itself via MQTT discovery (device with
  volume, mute, web radio, pairing, timers, alarm buttons, sensors, button events).
- **Light ring:** can follow the music of all receivers as a visualizer (Settings > Light ring: spectrum, level or pulse,
  colour, brightness, start LED; off by default) or glow as a lamp in any colour. A running timer shows its remaining time
  as a filling ring. Volume knob, mute, alarm, timer and buttons keep their own animations. In Home Assistant the ring is a
  light with colour, brightness and effects.
- **Alarms, more:** *sunrise light* (the ring brightens from deep red to warm white over N minutes before the alarm),
  *not on public holidays* (pick the German state under Alarms & timers), *skip next* (one time), *fade out when stopped*,
  and any stream or file address as sound (e.g. a file on your music server).
- **Sleep timer:** Overview, 15-90 minutes: the music fades out over 30 s and every source stops.
- **Now playing:** title and artist of Spotify, AirPlay, Bluetooth, Cast and web radio; UPnP, Sendspin, Tidal and
  Snapcast show as playing while they use the speaker. Bluetooth can be paused and skipped from the web interface.
- **Sources and volume** (Settings): when a second source starts, the newest plays and the others pause (Bluetooth and
  Cast really pause, web radio stops, the rest are muted until they start again, and come back 5 s after the newest one
  ends). *All play together* restores the old behaviour. A highest volume overall and per source, and a start volume per
  source (e.g. Bluetooth always starts at 25 %).
- **Sound** (Settings): bass and treble (±12 dB), *loudness* (more bass and treble the quieter it plays) and *night mode*
  (evens out loud and quiet passages). Works for every source, changes apply at once.
- **Announcements:** Home Assistant sends an audio address (text-to-speech, door bell) or `chime` / `bell` / `beep` to the
  *Announcement* text entity; the music is lowered meanwhile (Settings > Sources: by how many dB). The Overview has test
  buttons, a button can be mapped to the chime.
- **Services** (Settings): switch Spotify, UPnP, Cast, AirPlay, Sendspin, Bluetooth, Tidal and Snapcast on or off. A
  switched-off service is not started and its ports stay closed. A service that keeps crashing is restarted with growing
  pauses (30 s ... 30 min) and marked as failing.
- **Back up and restore** (Settings): one file with settings, pairings, Spotify login and SSH keys (contains secrets);
  restoring it brings everything back, e.g. after a factory reset, except the access data for Let's Encrypt (stored
  encrypted with a device key that stays on the speaker; enter it again after a reset or on another device). The *diagnostics package* (status, settings without
  secrets, logs) is meant for bug reports.
- **HTTPS:** System > HTTPS (or `WEB_TLS` in `/data/leuchtfeuer/config`) switches the web interface to HTTPS; HTTP then
  redirects. *Own certificate* (`WEB_TLS="on"`) works at once, the browser warns once. *Let's Encrypt* (`WEB_TLS="acme"`)
  fetches a trusted certificate for a name in your own domain over the DNS challenge (Cloudflare, Hetzner Console, deSEC,
  netcup, Gandi, Porkbun, Namecheap or ACME-DNS): the speaker does not have to be reachable from the internet, but the name must point to it in your own network
  (router, Pi-hole, local DNS). Until the certificate is there, and when opened by IP address, the own certificate is used;
  it renews itself about 30 days before it expires. The access data of the DNS provider is stored encrypted in the config
  (AES-256-GCM with a device key `secret.key` that is not part of the backup; after restoring on another device, enter it
  again) and is never shown or put into the diagnostics package. Against someone with root on the speaker this does not
  help, so give the token the smallest rights the provider offers (only DNS, only this zone). With a *challenge domain*
  (`ACME_DNS_ALIAS`) `_acme-challenge.<name>` points by CNAME to a name in another zone (e.g. a free deSEC zone), and the
  speaker only needs a token for that zone; recommended with netcup, whose API key may change every zone of the account. MQTT can use TLS too (Home Assistant tab).
- **Clock:** the speaker sets its clock by NTP after start and every 6 h (`NTP_SERVER`); the Overview warns if it is off by
  more than 2 s.

### Discovery across Wi-Fi ↔ LAN

Many routers (e.g. FRITZ!Box) do not forward multicast between Wi-Fi and LAN. A PC on the LAN then does not see
the speaker via mDNS although phones on the Wi-Fi do. Workarounds: use the IP (Cast known hosts,
`SENDSPIN_SERVER`), or put the client on the Wi-Fi.

## 6. Maintenance

- **Update:** `git pull && ./build.sh && ./install.sh --ip <ip> --key <pub>` (or `./install.sh --prebuilt`). Before new files
  are put in place the old ones are saved to `/data/leuchtfeuer/.prev`; if a service then keeps failing within 10 minutes, the
  speaker goes back to the previous version by itself (Settings > Update shows it, and has a button to roll back by hand). Once the 10 minutes pass, the backup is discarded when less than 60 MB are
  free (the normal case on the Invoke, where it takes about 40 MB); the button has nothing to restore then.
- **Update from the web interface:** Settings > Update checks the release page and installs a newer release. It needs the
  release signing key in `/data/leuchtfeuer/config` (`UPDATE_PUBKEY`, set by `install.sh` from `docs/release-key.pub`): only
  packages signed with that key are accepted. Without internet on the speaker, upload the package and its `.sig` there.
- **Releases (maintainer):** create a key once with `(cd src/relsign && go run . keygen ~/.config/leuchtfeuer/release.key)`,
  put the printed public key into `docs/release-key.pub`, store the private key as secret `LEUCHTFEUER_SIGNING_KEY` in the
  GitHub repository for `.github/workflows/release.yml`. A tag `v*` then builds, signs and publishes the
  package; locally: `./build.sh --no-tidal && tools/make-release.sh --key <file>`.
- **Tests without a speaker:** `tests/run.sh` (also run by the CI).
- **Verify:** `scripts/verify-install.sh --ip <ip> --key <pub>` (also checks the audio chain plugins, the source controls and the vendor `audio-ui`).
- **Device test:** `scripts/smoke.sh --ip <ip> --key <pub> [--token lf_…] [--listen]`. It goes deeper than verify and
  writes a Markdown report:
  - each source PCM opens
  - controls and the sound plugin file are there
  - CPU of the audio chain
  - clock, NTP and watchdog
  - every capture device is recorded with its level, to find the microphone for the voice assistant
  - with a key: API, security headers and origin check
  - with `--listen`: chime, radio, ducking, cross-fade and briefing, asked one by one
- **Hardware watchdog:** `WATCHDOG="on"` in `/data/leuchtfeuer/config` (only if `smoke.sh` found `/dev/watchdog` and nothing else
  holds it). `leuchtfeuerd -watchdog` sets a 60 s timeout and feeds it only while the hook is alive. After 3 boots without
  30 minutes of stable uptime it stays off; to re-arm, delete `/data/leuchtfeuer/watchdog-unstable`. The emergency brake
  closes it cleanly.
- **Do not kill `mcu-interface`** (vendor ring/amplifier controller): the vendor supervisor then restarts its stack in recovery
  mode, `audio-ui` drops off the router and the amplifier stays muted. A reboot of the speaker fixes it.
- **Emergency brake:** `ssh root@<ip> 'touch /data/leuchtfeuer/disable-hook'`, reboot → original behaviour.
- **Uninstall:** `./uninstall.sh --ip <ip> --key <pub> [--purge]` (removes the autostart hook, restores
  `dnsmasq.conf`, reboots; `--purge` also deletes `/data/leuchtfeuer`).
- **Factory state:** the vendor firmware can be flashed again with the vendor tool (`l2nand -m 83` with the vendor
  image); restore `factory_setting` from your backup if it was damaged.

## 7. Troubleshooting

| Symptom | Check |
|---|---|
| `adb shell id` is not root | Not StockRoot (firmware 12.x has adbd off and port 22 closed) – part 2 |
| `install.sh` slow or timing out | Wi-Fi link (ping loss); move the speaker, re-run – unchanged files are skipped |
| SSH refuses | `--key` must be the public key matching your private key / agent; too many agent keys can exhaust dropbear's 10 tries → `-o IdentitiesOnly=yes` |
| Service missing | `ssh root@<ip> 'ps; tail /data/leuchtfeuer/log/<service>.log'`; the hook restarts dead services every 30 s |
| Bluetooth not visible | `scripts/verify-install.sh`; `/data/leuchtfeuer/log/bluetooth-*.log`; `hciconfig hci0` must say `UP RUNNING PSCAN ISCAN` |
| Music Assistant says "legacy mode" for Sendspin | Expected: sendspin-go 1.8.x speaks the unencrypted dialect; accepted while "Allow legacy clients" is on |
| No sound, "audio-ui not reachable" | `scripts/verify-install.sh` (plugins, `audio-ui`, source controls); a reboot of the speaker usually fixes it. If `/data/leuchtfeuer/lib/ladspa/leuchtfeuer-viz-tap.so` or `leuchtfeuer-eq.so` is missing, run `install.sh` again (the audio chain needs both) |
| One source is silent | Settings > Sources: is it marked *paused (other source)*? It comes back 5 s after the other source stops, or switch to *All play together*. `amixer -c 0 sget "Quelle spotify"` should be 255 |
| A service keeps failing | Overview shows it; Settings > Services > Log. After an update the speaker rolls back by itself; otherwise switch the service off |
| Alarm at the wrong time | Overview warns if the clock is off; check `NTP_SERVER` and that the speaker reaches it (`/data/leuchtfeuer/hook.log`) |
| Tidal login fails | iFi certificate may have been revoked; not fixable here |
