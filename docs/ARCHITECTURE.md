# Architecture

How the installed system fits together. File names are relative to `device/invoke/` in this repo and
`/data/invoke/` on the speaker (`/data` → `/lsync/data1`, a writable, persistent YAFFS2 partition).

## Boot and supervision

```
power on → Harman init (Android init + Yocto) → dnsmasq (root, when wlan0 is up)
        → dhcp-script /data/invoke/boot.sh init          (lines appended to /data/dnsmasq.conf)
        → /data/invoke/hook.sh   (supervisor loop, every 30 s)
```

`dnsmasq.conf` gets `dhcp-script=…`, `leasefile-ro` and a `dhcp-range` in a network that does not exist, so dnsmasq
calls the script once with `init` at start and never hands out leases. `boot.sh` returns immediately and detaches
`hook.sh`. The hook:

- mounts a tmpfs on `/home/root` and places `authorized_keys` there (dropbear checks the permissions of all parent
  directories; `/data` is 777),
- stops the vendor `sshd` and starts its own **dropbear 2026.94** (ed25519/ecdsa host keys generated per device,
  password login compiled out),
- builds an **iptables chain `INVOKE`** (SSH, mDNS, DHCP replies, ICMP, established, the web port, the ports of the
  **switched-on** services from their `# ports:` header line, and own extra ports from `ports.local`). Each loop computes
  the wanted rules; when they differ (a service switched off, `ports.local` edited) it builds a new chain and swaps it in,
  so closed ports really close and there is never a moment without the final `DROP`. Switches IPv6 off, stops `adbd`
  (`disable-adb` flag),
- **bind-mounts `podium.conf`** over `/etc/podium/podium.conf` and restarts the vendor service manager, which
  drops Cortana, the dead Harman Spotify, OTA and crash upload **and the vendor Bluetooth stack**;
  **bind-mounts `ca-certificates.crt`** (the speaker ships a 2018 CA bundle without Let's Encrypt),
- announces a DHCP host name (`DHCP_HOSTNAME`) so the router shows the speaker as `<name>.lan`,
- starts every executable `services/*.sh` (they end in `exec`) whose **group is switched on** and restarts dead ones.
  A service that dies within 2 minutes of its start counts as a failure; from the second failure in a row the hook waits
  30 s, 60 s, ... up to 30 min before the next try, and from the third the web interface shows it as failing. The state
  per service is in `/run/invoke-svc-<name>.state` (`failures next-start started restarts`). A deliberate stop (restart
  button, backup restore, update) announces itself with `/run/invoke-svc-<name>.expected` and does not count,
- trims the logs every 5 minutes (`log/*.log` and `hook.log`, from 1 MiB: the last 256 KiB go to `.1`, the file is
  truncated in place, so running services keep writing),
- sets the clock by NTP after the start and every 6 h (`busybox ntpd -q`, `NTP_SERVER`) unless the firmware runs its own
  ntpd,
- watches the services for 10 minutes after an update (`update-pending`) and rolls the update back when one keeps
  failing (see *Updates*).

**Service header lines.** Each `services/<name>.sh` starts with `# title:`, `# group:` (spotify, upnp, cast, airplay,
sendspin, bluetooth, tidal, snapcast, core), `# process:`, `# ports:` (`tcp 8009, udp 6001:6011`) and `# default: on|off`.
They are the one source for the hook (start, firewall), invoked (service list, switches, logs, source detection) and
`scripts/verify-install.sh`. A group is switched with `SERVICE_<GROUP>="on|off"` in `config` (the web interface writes it;
the old `AIRPLAY="off"` still works).

Emergency brake: a file `/data/invoke/disable-hook` makes the hook do nothing.

## Audio path

```
service -> ALSA "invoke_<source>" (plug -> softvol "Quelle <source>")
        -> "invoke_music" (plug to 48 kHz float -> LADSPA: invoke_viz_tap, invoke_eq -> softvol "Invoke Music")
        -> dmix "volmix_music" -> DSP (card 1, wm8904, 48 kHz)
alarm/timer tones -> "invoke_music" directly         announcements -> "invoke_announce" (softvol "Invoke Announce") -> dmix
```

`asound-music.conf` (loaded through the `ALSA_CONFIG` hook of `/etc/asound.conf`) defines these PCMs. Every source has its
own PCM with its own softvol control `Quelle <source>` (spotify, upnp, cast, airplay, bluetooth, sendspin, tidal, snapcast,
radio): normally at 255 (0 dB), so it does not change the volume; invoked uses it to mute one source (source rule) or to
lower all of them during an announcement. Programs that only use the ALSA default device (Sendspin/miniaudio,
Tidal/PortAudio) get their source PCM through `INVOKE_SRC` (`default` = `invoke_${INVOKE_SRC:-music}`, ALSA `@func getenv`),
with a `hint` block, otherwise miniaudio picks the ALSA `null` device.

The vendor service `audio-ui` ducks the ALSA control `music` to 0 at every click of the volume knob, which made
music drop out. The services therefore use their **own** control `Invoke Music`, which `audio-ui` does not know.
invoked copies the knob's ALSA control `system` into `Invoke Music` whenever `audio-ui` reports `com.harman.volumeChanged`
and every 5 s as a safety net (this replaced `volume-sync.sh`, which forked `amixer` five times a second).

**Sound** (`device/src/invoke-eq.c`, LADSPA `invoke_eq`): low shelf (bass, 120 Hz), high shelf (treble, 6 kHz), preamp
against clipping, a feed-forward compressor (night mode) and a soft limiter. invoked writes the effective values
(including loudness, which depends on the volume) into `/dev/shm/invoke-eq` (64 bytes, sequence counter, odd while
writing); the plugin maps the file read-only and picks up changes at once. Without the file it passes the audio through.

## Services (`services/*.sh`)

| Script | Program | Ports | Notes |
|---|---|---|---|
| `librespot.sh` | librespot 0.8.0 (static musl, patched: `--volume-ctrl fixed` does not attenuate) | 57500/tcp (zeroconf) | output via `aplay -D invoke_spotify`; `--onevent` reports playing, track and volume to invoked |
| `gmrender.sh` | gmrender-resurrect + libupnp (static), GStreamer of the device | 49494/tcp, 1900/udp | UUID derived from the Wi-Fi MAC |
| `sendspin.sh` | sendspin-go 1.8.2 | outgoing 8927 | `Alsa.NoMMap = 1` patch (mmap on dmix/softvol spun a core); `SENDSPIN_SERVER` |
| `castrecv.sh` | `src/castrecv` (Go) | 8009/tcp (TLS), 8008, 8443 | Cast receiver emulation, plays with `gst-launch-1.0` |
| `tidal-1/2/3-*.sh` | iFi `tidal_connect_application` + bundled libs, avahi 0.6.32 (LD_PRELOAD shim for the missing `avahi` user), own `dbus-daemon` config | 2019/tcp | optional |
| `shairport.sh` | shairport-sync 3.3.9 (AirPlay 1, Apple ALAC decoder), tinysvcmdns | 5000/tcp, 6001-6011/udp | does not attenuate; volume, play begin/end and metadata pipe go to invoked |
| `snapclient.sh` | snapclient 0.31 (static musl, file player) + `aplay -D invoke_snapcast` | outgoing 1704 | off by default; `SNAPCAST_SERVER` |
| `invoked.sh` | `src/invoked` | 80/tcp (443 with `WEB_TLS`) | see below |
| `bluetooth-1..4-*.sh` | bluetoothd, `btagent`, bluealsa, bluealsa-aplay | – | see below |

## Bluetooth

The vendor stack (Bluedroid) forgot pairings and was unreliable. It is replaced by **BlueZ 5.50**
(`tools/build-bluez.sh`; state under `/data/invoke/bluez/var` so pairings persist):

1. `bluetooth-1`: loads the kernel module `bt8xxx` (Marvell SD8887) with its firmware, brings `hci0` up (the chip
   needs longer on a cold start than bluetoothd waits), starts `bluetoothd -n`. Kernel 3.8 has HCI/L2CAP/SCO/RFCOMM;
   the chip supplies its own BD address. `main.conf`: class *speaker*, BR/EDR only, no discoverable/pairable timeout.
2. `bluetooth-2`: **`btagent`** (`src/btagent`): BlueZ agent `NoInputNoOutput`, accepts every pairing and service
   authorization, marks paired devices *Trusted*, keeps the adapter powered. By default (`BLUETOOTH_PAIRING="button"`)
   the adapter is neither discoverable nor pairable; a short press on the Bluetooth button (`mcu-interface` publishes
   `com.harman.test.inputEvent ["bluetooth", "0"]` on the WAMP router; a long press produces no event) opens a 2-minute
   pairing window, a second press or a successful new pairing closes it, the light ring answers with a stock animation
   (`com.harman.ledAnimate`; the firmware has no dedicated pairing animation). Trusted, already paired devices reconnect
   at any time. `always` keeps the old behaviour.
3. `bluetooth-3`: **bluez-alsa** `-p a2dp-sink --a2dp-volume` (SBC). 
4. `bluetooth-4`: `bluealsa-aplay -D invoke_music`.

### Volume: knob ↔ phone

`audio-ui` is the owner of the volume state (group `music`, 0-100 %); it sets the ALSA controls and the LEDs. Writing
the ALSA control directly leaves `audio-ui` unaware, so the next knob step starts from its stale value. `btagent`
therefore talks to `audio-ui` through the vendor **WAMP router** (`bonefish`, rawsocket/MessagePack on
`127.0.0.1:9999`, realm `default`):

- knob → `audio-ui` publishes `com.harman.volumeChanged ["music", n]` → btagent sets the phone's AVRCP absolute volume
  (bluez-alsa `org.bluealsa.PCM1.Volume`, 0-127),
- phone → PCM `Volume` property changes → btagent calls `com.harman.volumeAdjust([delta])` (there is no working
  `volumeSet`).

The speaker is master: a newly connected phone receives the speaker's current volume.

`btagent` also reads the phone's AVRCP player (`org.bluez.MediaPlayer1`: status, title, artist, album) and publishes it as
`invoke.source.state ["bluetooth", {...}]`; it pauses the phone on `invoke.source.claim` from another source and takes
`invoke.bt.control` (play, pause, next, previous) from the web interface.

## Cast receiver (`src/castrecv`)

The firmware image contains no Cast receiver (it was a cloud download for which the speaker lacks the keys). `castrecv`
implements the CASTV2 protocol (TLS on 8009, protobuf framing by hand, namespaces connection/heartbeat/receiver/media),
announces `_googlecast._tcp` (model *Chromecast Audio*) via mDNS and plays `LOAD`ed URLs with GStreamer
(pause/resume by SIGSTOP/SIGCONT). Volume and mute go through `audio-ui` (`volumeAdjust`, `musicMuteSet`) like the knob, and a
turn of the knob updates the sender's volume slider at once (`RECEIVER_STATUS`). It reports state and title to invoked and
pauses on `invoke.source.claim`. Senders that verify the device certificate
(YouTube, Chrome, Google Home) refuse it; Music Assistant, Home Assistant, VLC, pychromecast accept it.

## invoked (web interface and extras)

`src/invoked` (Go, one static binary, service `invoked.sh`) provides everything that is not an audio receiver:

| Part | How |
|---|---|
| Volume, mute | through the vendor `audio-ui` over its WAMP router (`com.harman.volumeGet`, `volumeAdjust`, `musicMuteSet`, `musicMuteToggle`; events `volumeChanged`, `musicMuteChanged`), so the ALSA controls and LEDs stay consistent |
| Buttons | subscribes to `com.harman.test.inputEvent [name, value]` (mic, volumeup/down, bluetooth); maps them to actions (settings) and forwards them to Home Assistant |
| Web radio, alarm and timer tones | `gst-launch-1.0` (streams) or generated beeps piped to `aplay`, always on ALSA `invoke_music` |
| Alarms, timers | scheduler in the configured IANA time zone; fade-in through `volumeAdjust`; light ring animations `L_111_c_alarm`, `L_112_c_timer` via `com.harman.ledAnimate` |
| Wi-Fi guard | `wpa_cli` (`status`, `signal_poll`, `scan_results`, `roam`) + `ping` to the default gateway; per-access-point penalty list |
| Home Assistant | MQTT 3.1.1 (paho), discovery topics under `homeassistant/`, state under `invoke/<mac>/…`, availability via last will |
| Sources | `sources.go`: who plays what. Spotify and AirPlay report through `invoked -source-event ...` (librespot `--onevent`, shairport-sync session commands) over the Unix socket `/run/invoke-events.sock`; AirPlay titles from the metadata pipe; Bluetooth and Cast over WAMP `invoke.source.state`; UPnP, Sendspin, Tidal and Snapcast count as playing while one of their processes has an ALSA playback device open (`/proc/*/fd`, matched to the service by its parent processes). **Source rule** `last`: when a source starts, `invoke.source.claim` makes Bluetooth and Cast pause, web radio stops, the others are muted through their source control; 5 s after the newest one ends they come back. Volume limits (overall, per source) and a start volume per source |
| Sound | `eq.go` -> `/dev/shm/invoke-eq` for the LADSPA plugin (see *Audio path*) |
| Announcements | `announce.go`: URL (TTS, door bell) or built-in tones on `invoke_announce`, sources lowered by `DuckDB` meanwhile, one after the other |
| Sleep timer | fades out over 30 s, then stops every source (radio stops, Bluetooth/Cast pause, the rest muted until they start again) |
| Light ring scenes | before the visualizer: sunrise light (an alarm's `sunriseMin`), remaining time of the next timer; lamp mode (`static`) and own colour; Home Assistant light |
| Clock | SNTP query to `NTP_SERVER` every 30 min, deviation shown in the web interface |
| Backup, diagnostics | `backup.go`: tar.gz of `config`, `invoked.json`, keys, Bluetooth pairings, Spotify login (restore writes only these, then restarts the services); diagnostics with secrets removed |
| Web interface | embedded static files + JSON API behind a login page; live state over Server-Sent Events (`/api/events`, status on every change and every 10 s); sessions survive restarts (`sessions.json`, only SHA-256 of the cookie); optional HTTPS with an own certificate (`WEB_TLS`) (session cookie, HttpOnly/SameSite=Strict; password stored as salted PBKDF2-HMAC-SHA256 hash in `WEB_PASSWORD_HASH`, set with `invoked -set-password` reading stdin; 5 wrong tries lock an address for a minute); design from Kante (`web/kante/`, vendored with `tools/sync-kante.sh`, never edited by hand) |

The core works against small interfaces (volume, player, LED) and an injectable clock, so the logic runs without a speaker.

**Demo mode and release check.** `tools/build-invoked.sh` builds the release binary and aborts if the demo marker
(`INVOKE-DEMO-BUILD`) is inside. The demo (`-tags demo`, `demo/start.sh`) is a separate build for screenshots with the shared
"Studio Weber" data; it has no command line switch or data in the normal build.

### Light ring

The ring controller (MCU, `mcu-interface` talks to it) sits on `/dev/i2c-0` at address `0x36`. `ledAnimate(name, {repeat})`
uploads a pattern file `/usr/share/lights/*.bin` (39 bytes per frame: 13 × R,G,B; the stock patterns use only the first 12
LEDs) as `0e 01 <frames>` and the MCU plays it; `ledSet("front", …)` drives only the front status LED. A call takes about
250 ms, too slow for live frames, so the **visualizer** in invoked (`viz.go`) writes single frames `0e 01 <39 bytes>` itself,
25 per second (about 5 ms bus time each). `mcu-interface` opens the bus only per transfer, so both coexist. The visualizer
stops writing while the stock firmware uses the ring: after `volumeChanged`/`musicMuteChanged` and button events (2.5-3 s),
while muted, and while invoked plays its own alarm/timer animation. Killing `mcu-interface` is not a good idea: podium
then may not restart it, and it also handles the DAC/amplifier mute.

The audio comes from the LADSPA plugin `invoke-viz-tap` (`device/src/invoke-viz-tap.c`, `tools/build-viztap.sh`) in
`asound-music.conf`: `invoke_music` = `plug` (to 48 kHz float) → `ladspa` tap → `softvol "Invoke Music"` → `dmix`. It passes
the audio through unchanged and writes a mono copy into a ring buffer `/dev/shm/invoke-viz` (header: magic, rate, write
position; 8192 floats) without ever blocking. invoked reads the last 2048 samples, computes 12 log-spaced bands (50 Hz -
14 kHz, FFT with Hann window) or the RMS level, applies automatic gain and decay, and renders spectrum, level (symmetric
from the start LED) or pulse (bass). The rate conversion must happen before the tap: dmix fixes the period time at
5333.33 µs, and with a free rate in front of the LADSPA stage the hw-params intervals become empty for 44.1 kHz clients
(PortAudio/Tidal aborts with `snd_interval_empty`). Earlier attempts (ALSA `multi` tee to the Loopback card, `meter`
plugin) failed, see RESEARCH-NOTES.md.

## Updates and rollback

`device/invoke/apply-update.sh apply <stage>` puts a staged update in place: files that are replaced or removed (`.remove`)
are first copied to `/data/invoke/.prev` (new files are listed there), `podium.conf` and the CA bundle are overwritten in
place (they are bind-mounted), then `update-pending` is written. The hook watches the services for 10 minutes: when one
fails three times in a row, `apply-update.sh rollback` restores `.prev`, deletes the new files and restarts everything;
`update-rolledback` tells the web interface why. `install.sh` (over SSH) and the web interface use the same script.

The web interface can update itself from the release page (Gitea API, `Settings.Update.URL`) or from an uploaded package:
`leuchtfeuer-<version>-invoke.tar.gz` (layout of `/data/invoke`, from `tools/make-release.sh`) with an Ed25519 signature over
`"leuchtfeuer-release:" + sha256(package)` (`src/relsign`). invoked accepts it only with a valid signature for `UPDATE_PUBKEY`
and only files below `bin/`, `lib/`, `services/`, `bluez/{bin,lib}/` and the system scripts, never settings or keys.
`.gitea/workflows/release.yml` builds, signs and publishes on a tag `v*`; `install.sh --prebuilt` installs such a package
without building.

## Shared WAMP module

`src/wamp` (Rawsocket/MessagePack client and a reconnecting `Hub`) is used by invoked, btagent and castrecv through a
`replace leuchtfeuer/wamp => ../wamp` in their `go.mod`.

## Build system

`build.sh` runs `tools/build-*.sh`. Toolchains are Docker images: `tools/docker/xenial-armhf*.Dockerfile` (Ubuntu
16.04 cross, glibc 2.23 like the speaker; GStreamer/GLib headers), `armv7-musl.Dockerfile` (static dropbear and
librespot), `rust-armv7.Dockerfile`. The Go programs are pure Go (`CGO_ENABLED=0`) except sendspin-go (cgo, miniaudio).
Outputs go to `build/` (not in git).

## Security notes

- SSH: public-key only. adb (root shell without login on 5555) is stopped by the hook and filtered.
- The firewall allows only the service ports; the vendor's WAMP router (9998/9999) is internal only.
- The vendor cloud endpoints for OTA are blocked in `/etc/hosts` (StockRoot); Cortana/OTA/crash upload are not started.
- The web interface can use HTTPS (`WEB_TLS="on"`, own certificate) and MQTT can use TLS. Sessions are stored only as
  SHA-256 of the cookie value; expired sessions and old failed logins are cleared every 10 minutes.
- Updates from the web interface need a valid Ed25519 signature (`UPDATE_PUBKEY`); without a key the feature is off.
- Bluetooth pairing needs no PIN, but is only possible during the 2-minute window after a press on the speaker's
  Bluetooth button (default). `BLUETOOTH_PAIRING="always"` makes it permanently open to anyone in range.
- mDNS announcements come from avahi (Tidal), librespot (libmdns) and castrecv.
