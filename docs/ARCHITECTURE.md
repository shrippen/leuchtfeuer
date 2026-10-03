# Architecture

How the installed system fits together. File names are relative to `device/leuchtfeuer/` (shared part) or
`targets/<target>/` (device-specific part) in this repo and to the installation directory on the device:
`/data/leuchtfeuer/` on the Invoke (`/data` → `/lsync/data1`, a writable, persistent YAFFS2 partition),
`/opt/leuchtfeuer/` on any other Linux (target `generic`).

## Targets

Leuchtfeuer is device-independent; what one device needs on top lives in one place per layer
(how to add a device: [TARGETS.md](TARGETS.md)):

| Layer | Shared | Device-specific |
|---|---|---|
| leuchtfeuerd | everything; volume is the only thing a target must provide | `src/leuchtfeuerd/target_<id>.go`: volume driver, extensions **buttons** and **ring** (light ring), vendor sounds, mixer card, temperature, Wi-Fi interface. The status reports `device.capabilities`; the web interface, MQTT and the API show only what the device has. |
| Hook | `device/leuchtfeuer/hook.sh`: services, firewall, NTP, logs, updates, watchdog | `targets/<id>/target.sh`: `target_init`, `target_tick`, `target_fw_rules`, `target_net` and defaults (name, firewall, interface) |
| Audio | `device/leuchtfeuer/asound-music.conf`: sources, tap, EQ, volume | `targets/<id>/asound-target.conf` + `target.env` (`@OUT@` output, `@CARD@`, `@LEUCHTFEUER_DIR@`) |
| Services | `device/leuchtfeuer/services/*.sh` (`# requires:` names the programs) | `targets/<id>/services/*.sh` (Invoke: BlueZ build, Tidal) |
| Package | `scripts/assemble.sh --target <id>` | `targets/<id>/assemble.sh` (programs, system files) |

Targets today: **invoke** (Harman Kardon Invoke; WAMP link to the vendor software, buttons, light ring, vendor sounds)
and **generic** (any Linux with systemd and ALSA; no extensions, leuchtfeuerd owns the volume, music services come from
the distribution's packages, installed by `setup.sh` as `leuchtfeuer.service`).

Own programs (btagent, castrecv) talk to leuchtfeuerd over the **local bus** (`src/lfbus`; HTTP + Server-Sent Events on
the Unix socket `$LEUCHTFEUER_RUN/leuchtfeuer-bus.sock`, root only), never to vendor software, so they run unchanged
on every target. Events: `volume`, `claim`, `bt-pairing`, `bt-control`, `button`; commands: `POST /volume`, `/source`,
`/ring`. See [API.md](API.md#local-bus).

## Boot and supervision

On `generic`, systemd starts `hook.sh` (`leuchtfeuer.service`, environment `LEUCHTFEUER_DIR`, `LEUCHTFEUER_RUN=/run/leuchtfeuer`);
everything below that is not marked as Invoke applies there too. On the Invoke:

```
power on → Harman init (Android init + Yocto) → dnsmasq (root, when wlan0 is up)
        → dhcp-script /data/leuchtfeuer/boot.sh init          (lines appended to /data/dnsmasq.conf)
        → /data/leuchtfeuer/hook.sh   (supervisor loop, every 30 s)
```

`dnsmasq.conf` gets `dhcp-script=…`, `leasefile-ro` and a `dhcp-range` in a network that does not exist, so dnsmasq
calls the script once with `init` at start and never hands out leases. `boot.sh` returns immediately and detaches
`hook.sh`. The hook (Invoke parts from `targets/invoke/target.sh`):

- mounts a tmpfs on `/home/root` and places `authorized_keys` there (dropbear checks the permissions of all parent
  directories; `/data` is 777),
- stops the vendor `sshd` and starts its own **dropbear 2026.94** (ed25519/ecdsa host keys generated per device,
  password login compiled out),
- builds an **iptables chain `LEUCHTFEUER`** (`FIREWALL`, on by default on the Invoke, off on `generic`; SSH, mDNS, DHCP replies, ICMP, established, the web port, the ports of the
  **switched-on** services from their `# ports:` header line, and own extra ports from `ports.local`). Each loop computes
  the wanted rules; when they differ (a service switched off, `ports.local` edited) it builds a new chain and swaps it in,
  so closed ports really close and there is never a moment without the final `DROP`. Switches IPv6 off, stops `adbd`
  (`disable-adb` flag),
- **bind-mounts `podium.conf`** over `/etc/podium/podium.conf` and restarts the vendor service manager, which
  drops Cortana, the dead Harman Spotify, OTA and crash upload **and the vendor Bluetooth stack**;
  **bind-mounts `ca-certificates.crt`** (the speaker ships a 2018 CA bundle without Let's Encrypt),
- announces a DHCP host name (`DHCP_HOSTNAME`) so the router shows the speaker as `<name>.lan`,
- starts every executable `services/*.sh` (they end in `exec`) whose **group is switched on** and whose programs
  (`# requires:`, looked up in `$LEUCHTFEUER_DIR/bin` and `PATH`) exist, and restarts dead ones. A missing program
  makes the service `missing` ("not installed" in the web interface) and keeps its ports closed.
  A service that dies within 2 minutes of its start counts as a failure; from the second failure in a row the hook waits
  30 s, 60 s, ... up to 30 min before the next try, and from the third the web interface shows it as failing. The state
  per service is in `/run/leuchtfeuer-svc-<name>.state` (`failures next-start started restarts`). A deliberate stop (restart
  button, backup restore, update) announces itself with `/run/leuchtfeuer-svc-<name>.expected` and does not count,
- trims the logs every 5 minutes (`log/*.log` and `hook.log`, from 1 MiB: the last 256 KiB go to `.1`, the file is
  truncated in place, so running services keep writing),
- sets the clock by NTP after the start and every 6 h (`busybox ntpd -q`, `NTP_SERVER`) unless the firmware runs its own
  ntpd,
- watches the services for 10 minutes after an update (`update-pending`) and rolls the update back when one keeps
  failing (see *Updates*).

**Service header lines.** Each `services/<name>.sh` starts with `# title:`, `# group:` (spotify, upnp, cast, airplay,
sendspin, bluetooth, tidal, snapcast, core), `# process:`, `# ports:` (`tcp 8009, udp 6001:6011`), `# requires:`
(programs) and `# default: on|off`. Services inherit `LEUCHTFEUER_DIR`, `LEUCHTFEUER_RUN`, `LEUCHTFEUER_TARGET`,
`LEUCHTFEUER_NAME` (default device name), `WIFI_IFACE`, `ALSA_CONFIG` and, on `generic`, `ALSA_CONFIG_PATH`.
They are the one source for the hook (start, firewall), leuchtfeuerd (service list, switches, logs, source detection) and
`scripts/verify-install.sh`. A group is switched with `SERVICE_<GROUP>="on|off"` in `config` (the web interface writes it;
the old `AIRPLAY="off"` still works).

Emergency brake: a file `/data/leuchtfeuer/disable-hook` makes the hook do nothing.

## Audio path

```
service -> ALSA "leuchtfeuer_<source>" (plug -> softvol "Quelle <source>")
        -> "leuchtfeuer_music" (plug to 48 kHz float -> LADSPA: leuchtfeuer_viz_tap, leuchtfeuer_eq -> softvol "Leuchtfeuer Music")
        -> @OUT@  (Invoke: dmix "volmix_music" -> DSP, card 1, wm8904, 48 kHz; generic: dmix on ALSA_CARD or ALSA_OUTPUT)
alarm/timer tones -> "leuchtfeuer_music" directly         announcements -> "leuchtfeuer_announce" (softvol "Leuchtfeuer Announce") -> dmix
```

`asound-music.conf` (on the Invoke loaded through the `ALSA_CONFIG` hook of `/etc/asound.conf`; on `generic` appended to
the alsa-lib base configuration with `ALSA_CONFIG_PATH`) defines these PCMs. Every source has its
own PCM with its own softvol control `Quelle <source>` (spotify, upnp, cast, airplay, bluetooth, sendspin, tidal, snapcast,
radio): normally at 255 (0 dB), so it does not change the volume; leuchtfeuerd uses it to mute one source (source rule) or to
lower all of them during an announcement. Programs that only use the ALSA default device (Sendspin/miniaudio,
Tidal/PortAudio) get their source PCM through `LEUCHTFEUER_SRC` (`default` = `leuchtfeuer_${LEUCHTFEUER_SRC:-music}`, ALSA `@func getenv`),
with a `hint` block, otherwise miniaudio picks the ALSA `null` device.

Invoke: the vendor service `audio-ui` ducks the ALSA control `music` to 0 at every click of the volume knob, which made
music drop out. The services therefore use their **own** control `Leuchtfeuer Music`, which `audio-ui` does not know.
leuchtfeuerd copies the knob's ALSA control `system` into `Leuchtfeuer Music` whenever `audio-ui` reports `com.harman.volumeChanged`
and every 5 s as a safety net (this replaced `volume-sync.sh`, which forked `amixer` five times a second). On `generic`
leuchtfeuerd owns the volume itself (`volume.json`) and sets `Leuchtfeuer Music` directly.

**Sound** (`device/src/leuchtfeuer-eq.c`, LADSPA `leuchtfeuer_eq`): low shelf (bass, 120 Hz), high shelf (treble, 6 kHz), preamp
against clipping, a feed-forward compressor (night mode) and a soft limiter. leuchtfeuerd writes the effective values
(including loudness, which depends on the volume) into `/dev/shm/leuchtfeuer-eq` (64 bytes, sequence counter, odd while
writing); the plugin maps the file read-only and picks up changes at once. Without the file it passes the audio through.

### Output

`leuchtfeuer_music` and `leuchtfeuer_announce` end in `leuchtfeuer_sink`, an alias for the target's output (`@OUT@`). The web
interface (Settings > Output, `output.go`) can point it elsewhere by writing `$LEUCHTFEUER_DIR/output.conf`, which the last
line of `asound-music.conf` includes and which overrides the alias (`pcm.!leuchtfeuer_sink`):

- `card:<n>`: dmix on `hw:<n>,0`, 48 kHz (other sound cards of the hardware, HDMI, USB),
- `bt:<MAC>`: ALSA plugin `bluealsa` (`device`, `profile a2dp`), i.e. bluez-alsa as A2DP **source**.

ALSA reads the configuration when a program starts, so after a switch leuchtfeuerd stops the speaker's own playback and
terminates the audio services (`.expected`, not counted as failures); the hook is woken with SIGUSR1 and starts them
right away. The file must exist (ALSA drops the whole configuration otherwise): the hook creates it empty, `leuchtfeuerd`
rewrites it from the saved choice at start. The hook also rewrites the install path baked into `asound-music.conf`
(first line `# leuchtfeuer-dir:`) when the installation is not where the package was built for.

Bluetooth speakers: `btagent` (`sink.go`) lists devices with an A2DP sink or class Audio/Video from BlueZ, searches for
30 s on request, pairs (agent `NoInputNoOutput`, Just Works), trusts and connects, and reconnects the chosen speaker every
20 s while it is away. It reports to leuchtfeuerd with `POST /bt` and receives commands as bus event `bt-sink`. Needs
bluealsa with `-p a2dp-source` (the Invoke service passes it; on `generic` the system's bluez-alsa usually does) and, on the
Invoke, `libasound_module_pcm_bluealsa.so` (built by `tools/build-bluez.sh`, loaded through `pcm_type.bluealsa` in
`targets/invoke/asound-target.conf`). While the speaker is not connected the sources cannot open the output; the web
interface shows "not connected" and switching back to the built-in output restores sound.

## Services (`services/*.sh`)

| Script | Program | Ports | Notes |
|---|---|---|---|
| `librespot.sh` | librespot 0.8.0 (static musl, patched: `--volume-ctrl fixed` does not attenuate) | 57500/tcp (zeroconf) | output via `aplay -D leuchtfeuer_spotify`; `--onevent` reports playing, track and volume to leuchtfeuerd |
| `gmrender.sh` | gmrender-resurrect + libupnp (static), GStreamer of the device | 49494/tcp, 1900/udp | UUID derived from the Wi-Fi MAC |
| `sendspin.sh` | sendspin-go 1.8.2 | outgoing 8927 | `Alsa.NoMMap = 1` patch (mmap on dmix/softvol spun a core); `SENDSPIN_SERVER` |
| `castrecv.sh` | `src/castrecv` (Go) | 8009/tcp (TLS), 8008, 8443 | Cast receiver emulation, plays with `gst-launch-1.0` |
| `tidal-1/2/3-*.sh` | iFi `tidal_connect_application` + bundled libs, avahi 0.6.32 (LD_PRELOAD shim for the missing `avahi` user), own `dbus-daemon` config | 2019/tcp | optional |
| `shairport.sh` | shairport-sync 3.3.9 (AirPlay 1, Apple ALAC decoder), tinysvcmdns | 5000/tcp, 6001-6011/udp | does not attenuate; volume, play begin/end and metadata pipe go to leuchtfeuerd |
| `snapclient.sh` | snapclient 0.35 (static musl, file player) + `aplay -D leuchtfeuer_snapcast` | outgoing 1704 | off by default; `SNAPCAST_SERVER` |
| `leuchtfeuerd.sh` | `src/leuchtfeuerd` | 80/tcp (443 with `WEB_TLS`, `on` or `acme`) | see below |
| `bluetooth-1..4-*.sh` | bluetoothd, `btagent`, bluealsa, bluealsa-aplay | – | see below |

## Bluetooth

Invoke: the vendor stack (Bluedroid) forgot pairings and was unreliable. It is replaced by **BlueZ 5.87**
(`tools/build-bluez.sh`; state under `/data/leuchtfeuer/bluez/var` so pairings persist):

1. `bluetooth-1`: loads the kernel module `bt8xxx` (Marvell SD8887) with its firmware, brings `hci0` up (the chip
   needs longer on a cold start than bluetoothd waits), starts `bluetoothd -n`. Kernel 3.8 has HCI/L2CAP/SCO/RFCOMM;
   the chip supplies its own BD address. `main.conf`: class *speaker*, BR/EDR only, no discoverable/pairable timeout.
2. `bluetooth-2`: **`btagent`** (`src/btagent`): BlueZ agent `NoInputNoOutput`, accepts every pairing and service
   authorization, marks paired devices *Trusted*, keeps the adapter powered. By default (`BLUETOOTH_PAIRING="button"`)
   the adapter is neither discoverable nor pairable; a short press on the Bluetooth button (leuchtfeuerd forwards it as
   bus event `button`) or the web interface / Home Assistant (`bt-pairing`) opens a 2-minute pairing window, a second
   press or a successful new pairing closes it, the light ring answers (`POST /ring` `bt_open`/`bt_closed`, which the
   Invoke target maps to stock animations). Trusted, already paired devices reconnect
   at any time. `always` keeps the old behaviour.
3. `bluetooth-3`: **bluez-alsa** `-p a2dp-sink --a2dp-volume` (SBC). 
4. `bluetooth-4`: `bluealsa-aplay -D leuchtfeuer_bluetooth`.

On `generic`, bluetoothd and bluealsa come from the system; `targets/generic/services` only adds btagent and
bluealsa-aplay (off by default).

### Volume: knob ↔ phone

leuchtfeuerd owns the volume (on the Invoke it delegates to `audio-ui`, which sets the ALSA controls and the LEDs).
`btagent` follows it over the local bus:

- knob / web / other source → bus event `volume` → btagent sets the phone's AVRCP absolute volume
  (bluez-alsa `org.bluealsa.PCM1.Volume`, 0-127),
- phone → PCM `Volume` property changes → btagent sends `POST /volume`.

The speaker is master: a newly connected phone receives the speaker's current volume.

`btagent` also reads the phone's AVRCP player (`org.bluez.MediaPlayer1`: status, title, artist, album) and reports it with
`POST /source`; it pauses the phone on the bus event `claim` from another source and takes `bt-control` (play, pause,
next, previous) from the web interface.

## Cast receiver (`src/castrecv`)

The firmware image contains no Cast receiver (it was a cloud download for which the speaker lacks the keys). `castrecv`
implements the CASTV2 protocol (TLS on 8009, protobuf framing by hand, namespaces connection/heartbeat/receiver/media),
announces `_googlecast._tcp` (model *Chromecast Audio*) via mDNS and plays `LOAD`ed URLs with GStreamer
(pause/resume by SIGSTOP/SIGCONT). Volume and mute go to leuchtfeuerd over the local bus (`POST /volume`), and every
change (knob, web) updates the sender's volume slider at once (`RECEIVER_STATUS`). It reports state and title with
`POST /source` and pauses on the bus event `claim`. Senders that verify the device certificate
(YouTube, Chrome, Google Home) refuse it; Music Assistant, Home Assistant, VLC, pychromecast accept it.

## leuchtfeuerd (web interface and extras)

`src/leuchtfeuerd` (Go, one static binary, service `leuchtfeuerd.sh`) provides everything that is not an audio receiver:

| Part | How |
|---|---|
| Volume, mute | `audioCtl` of the target. generic: own state (`volume.json`) on `Leuchtfeuer Music`. Invoke: through the vendor `audio-ui` over its WAMP router (`com.harman.volumeGet`, `volumeAdjust`, `musicMuteSet`, `musicMuteToggle`; events `volumeChanged`, `musicMuteChanged`), so the ALSA controls and LEDs stay consistent |
| Buttons | extension `buttons`; Invoke: subscribes to `com.harman.test.inputEvent [name, value]` (mic, volumeup/down, bluetooth); maps them to actions (settings) and forwards them to Home Assistant |
| Web radio, alarm and timer tones | `gst-launch-1.0` (streams) or generated beeps piped to `aplay`, always on ALSA `leuchtfeuer_music`. Web radio reconnects after an error or the end of a live stream (2, 4, 8, 16, 30 s; gives up after 8 tries in a row, state `reconnecting`). An alarm whose stream does not play within 15 s or ends while it rings switches to the built-in alarm tone. |
| Station search | `radiosearch.go`: radio-browser.info through leuchtfeuerd (server list from `all.api.radio-browser.info`, failover); `POST /api/radio/url` plays any stream without saving it |
| Briefing | `briefing.go`, `ical.go`. Sources: Open-Meteo (weather, place search), Bright Sky (DWD warnings), DWD pollen JSON, ICS calendars (own parser: RRULE, EXDATE, RECURRENCE-ID, TZID incl. Windows names), podcast RSS (newest enclosure), Home Assistant `/api/template`. All are fetched in parallel, at most 8 s each. Consecutive texts are merged and spoken through HA `/api/tts_get_url` or an own TTS address, then played one after the other as player kind `briefing`. As an alarm sound: chime, briefing, then a station (with the alarm fallback). It is prepared 4 min ahead. |
| Voice assistant | `voice.go`, `wyoming.go`: Wyoming satellite on TCP 10700, announced as `_wyoming._tcp`. Microphone `arecord` 16 kHz/16 bit/mono, 1024 samples per chunk. Mode `wake`: streams continuously, wake word on HA. Mode `button`: pipeline from `asr`. The answer goes to `leuchtfeuer_announce` while the music is lowered; the microphone sends silence meanwhile (no echo cancellation). Ring scene in Cortana blue. The port goes to `/data/leuchtfeuer/ports.leuchtfeuerd` for the hook's firewall. |
| Other speakers | `peers.go`: mDNS `_leuchtfeuer._tcp` (announce and browse, grandcat/zeroconf as in castrecv). Peers are added with their API key; https peers get their certificate fingerprint pinned (TOFU). leuchtfeuerd itself fetches status, copies settings sections (`PUT /api/settings/<section>` on the peer), starts updates and runs actions. |
| Access, logs, metrics | `tokens.go` (API keys `lf_…`, SHA-256 stored, scopes `read`/`full`, access management session-only), `sshkeys.go` (`authorized_keys` list/add/delete, never the last key), `auth.go` (origin check for cookie requests, CSP and other headers), `logs.go` (tails all service logs every second, copytruncate-aware; SSE live log; syslog RFC 5424 over UDP/TCP; counts `underrun` lines), `metrics.go` (Prometheus text format). See [API.md](API.md). |
| Sounds | `sounds.go`: finds the WAV sounds of the vendor software (start, error, pairing …) below `hw.SoundDirs`. An uploaded file (WAV parsed in Go, other formats decoded by GStreamer) is converted to the original's rate, channels and bits and bind-mounted over it. `sounds/vendor.map` lists the replacements; the hook mounts them at boot before it restarts the vendor services. Leuchtfeuer's own tones (alarm, timer, chime, bell, beep) play `sounds/tone-<id>.wav` instead of the generated sequence when one exists. Part of the backup. |
| Target | `target.go` (interface, capabilities), `target_invoke.go`, `target_generic.go`. `LEUCHTFEUER_TARGET` (set by the hook) or `TARGET` in config selects it; see *Targets* and [TARGETS.md](TARGETS.md). |
| Local bus | `bus.go`: HTTP + SSE on `$LEUCHTFEUER_RUN/leuchtfeuer-bus.sock` for btagent and castrecv (see *Targets*) |
| Room measurement | `measure.go`: pink noise (Kellet filter) through `leuchtfeuer_music` while the EQ is neutral and other sources paused. Analysis in the browser (`web/roomeq.js`, tested with node): 1/6-octave smoothing, reference 200 Hz–2 kHz, up to 4 cut-only peaking filters for peaks of 3 dB or more between 35 and 350 Hz. |
| Alarms, timers | scheduler in the configured IANA time zone; fade-in through the volume driver; ring animations `alarm`, `timer` (Invoke: `L_111_c_alarm`, `L_112_c_timer` via `com.harman.ledAnimate`) |
| Wi-Fi guard | `wpa_cli` (`status`, `signal_poll`, `scan_results`, `roam`) + `ping` to the default gateway; per-access-point penalty list |
| Home Assistant | MQTT 3.1.1 (paho), discovery topics under `homeassistant/`, state under `leuchtfeuer/<mac>/…`, availability via last will |
| Sources | `sources.go`: who plays what. Spotify and AirPlay report through `leuchtfeuerd -source-event ...` (librespot `--onevent`, shairport-sync session commands) over the Unix socket `/run/leuchtfeuer-events.sock`; AirPlay titles from the metadata pipe; Bluetooth and Cast over the local bus (`POST /source`); UPnP, Sendspin, Tidal and Snapcast count as playing while one of their processes has an ALSA playback device open (`/proc/*/fd`, matched to the service by its parent processes). **Source rule** `last`: when a source starts, the bus event `claim` makes Bluetooth and Cast pause, web radio stops, the others are muted through their source control; 5 s after the newest one ends they come back. Volume limits (overall, per source) and a start volume per source |
| Sound | `eq.go` -> `/dev/shm/leuchtfeuer-eq` (layout `IEQ2`, 128 bytes: bass/treble shelves, compressor, up to 6 peaking filters for room correction) for the LADSPA plugin (see *Audio path*). The plugin checks the file size before mapping it; an old `IEQ1` file means pass-through. |
| Announcements | `announce.go`: URL (TTS, door bell) or built-in tones on `leuchtfeuer_announce`, sources lowered by `DuckDB` meanwhile, one after the other |
| Sleep timer | fades out over 30 s, then stops every source (radio stops, Bluetooth/Cast pause, the rest muted until they start again) |
| Light ring scenes | before the visualizer: sunrise light (an alarm's `sunriseMin`), remaining time of the next timer; lamp mode (`static`) and own colour; Home Assistant light |
| Clock | SNTP query to `NTP_SERVER` every 30 min, deviation shown in the web interface |
| Backup, diagnostics | `backup.go`: tar.gz of `config`, `leuchtfeuerd.json`, keys, Bluetooth pairings, Spotify login (restore writes only these, then restarts the services); diagnostics with secrets removed |
| Web interface | embedded static files + JSON API behind a login page; live state over Server-Sent Events (`/api/events`, status on every change and every 10 s); sessions survive restarts (`sessions.json`, only SHA-256 of the cookie); optional HTTPS with an own certificate (`WEB_TLS`) (session cookie, HttpOnly/SameSite=Strict; password stored as salted PBKDF2-HMAC-SHA256 hash in `WEB_PASSWORD_HASH`, set with `leuchtfeuerd -set-password` reading stdin; 5 wrong tries lock an address for a minute); design from Kante (`web/kante/`, vendored with `tools/sync-kante.sh`, never edited by hand) |

The core works against small interfaces (volume, player, LED) and an injectable clock, so the logic runs without a speaker.

**Demo mode and release check.** `tools/build-leuchtfeuerd.sh` builds the release binary and aborts if the demo marker
(`LEUCHTFEUER-DEMO-BUILD`) is inside. The demo (`-tags demo`, `demo/start.sh`) is a separate build for screenshots with the shared
"Studio Weber" data; it has no command line switch or data in the normal build.

### Light ring

The ring controller (MCU, `mcu-interface` talks to it) sits on `/dev/i2c-0` at address `0x36`. `ledAnimate(name, {repeat})`
uploads a pattern file `/usr/share/lights/*.bin` (39 bytes per frame: 13 × R,G,B; the stock patterns use only the first 12
LEDs) as `0e 01 <frames>` and the MCU plays it; `ledSet("front", …)` drives only the front status LED. A call takes about
250 ms, too slow for live frames, so the **visualizer** in leuchtfeuerd (`viz.go`) writes single frames `0e 01 <39 bytes>` itself,
25 per second (about 5 ms bus time each). `mcu-interface` opens the bus only per transfer, so both coexist. The visualizer
stops writing while the stock firmware uses the ring: after `volumeChanged`/`musicMuteChanged` and button events (2.5-3 s),
while muted, and while leuchtfeuerd plays its own alarm/timer animation. Killing `mcu-interface` is not a good idea: podium
then may not restart it, and it also handles the DAC/amplifier mute.

The audio comes from the LADSPA plugin `leuchtfeuer-viz-tap` (`device/src/leuchtfeuer-viz-tap.c`, `tools/build-viztap.sh`) in
`asound-music.conf`: `leuchtfeuer_music` = `plug` (to 48 kHz float) → `ladspa` tap → `softvol "Leuchtfeuer Music"` → `dmix`. It passes
the audio through unchanged and writes a mono copy into a ring buffer `/dev/shm/leuchtfeuer-viz` (header: magic, rate, write
position; 8192 floats) without ever blocking. leuchtfeuerd reads the last 2048 samples, computes 12 log-spaced bands (50 Hz -
14 kHz, FFT with Hann window) or the RMS level, applies automatic gain and decay, and renders spectrum, level (symmetric
from the start LED) or pulse (bass). The rate conversion must happen before the tap: dmix fixes the period time at
5333.33 µs, and with a free rate in front of the LADSPA stage the hw-params intervals become empty for 44.1 kHz clients
(PortAudio/Tidal aborts with `snd_interval_empty`). Earlier attempts (ALSA `multi` tee to the Loopback card, `meter`
plugin) failed, see RESEARCH-NOTES.md.

### Fades and level trim

A source that yields fades out, and a released source fades back in. `mixer.go` moves the softvol control `Quelle <src>`
in 8 steps of 60 ms, linear in dB. Softvol has 256 steps over −51…0 dB, and the value 0 is silence. A per-source trim
(`trimDB`, 0–20 dB) lowers loud sources permanently and adds to the announcement ducking.

### Watchdog

`WATCHDOG="on"` in config: the hook starts `leuchtfeuerd -watchdog <alive file>` once.

- It sets WDIOC_SETTIMEOUT 60 s and sends WDIOC_KEEPALIVE every 5 s, but only while the hook has written its uptime
  within the last 150 s. On SIGTERM it writes `V` (magic close).
- Boot-loop guard: `/data/leuchtfeuer/watchdog-unstable` counts boots with the watchdog armed. It is reset after 30 minutes
  of uptime. At 3 the watchdog stays off.

## Updates and rollback

`device/leuchtfeuer/apply-update.sh apply <stage>` puts a staged update in place: files that are replaced or removed (`.remove`)
are first copied to `/data/leuchtfeuer/.prev` (new files are listed there), `podium.conf` and the CA bundle are overwritten in
place (they are bind-mounted), then `update-pending` is written. The hook watches the services for 10 minutes: when one
fails three times in a row, `apply-update.sh rollback` restores `.prev`, deletes the new files and restarts everything;
`update-rolledback` tells the web interface why. Once the 10 minutes pass, the hook discards `.prev` when less than 60 MB
are free (on the Invoke it holds about 40 MB of old binaries); only the manual rollback is lost then. `install.sh` (over SSH) and the web interface use the same script.

The web interface can update itself from the release page (Gitea API, `Settings.Update.URL`) or from an uploaded package:
`leuchtfeuer-<version>-<package>.tar.gz` (`invoke` or `generic-<arch>`; layout of the installation directory, from
`tools/make-release.sh --target …`) with an Ed25519 signature over
`"leuchtfeuer-release:" + sha256(package)` (`src/relsign`). leuchtfeuerd accepts it only with a valid signature for `UPDATE_PUBKEY`
and only files below `bin/`, `lib/`, `services/`, `bluez/{bin,lib}/` and the system scripts, never settings or keys.
`.gitea/workflows/release.yml` builds, signs and publishes on a tag `v*`; `install.sh --prebuilt` installs such a package
without building.

## Shared Go modules

`src/lfbus` (client of the local bus) is used by btagent and castrecv, `src/wamp` (Rawsocket/MessagePack client and a
reconnecting `Hub`) only by leuchtfeuerd's Invoke target; both through `replace` lines in the `go.mod` files.

## Build system

`build.sh` runs `tools/build-*.sh`. Toolchains are Docker images: `tools/docker/xenial-armhf*.Dockerfile` (Ubuntu
16.04 cross, glibc 2.23 like the speaker; GStreamer/GLib headers), `armv7-musl.Dockerfile` (static dropbear and
librespot), `rust-armv7.Dockerfile`. The Go programs are pure Go (`CGO_ENABLED=0`) except sendspin-go (cgo, miniaudio).
Outputs go to `build/` (not in git). `tools/build-generic.sh <arch>` builds the own programs and the LADSPA plugins for
`generic` (amd64, arm64, armv7) into `build/generic-<arch>/`.

## Security notes

- Web: strict CSP (scripts only from own files), origin check for cookie requests, API keys for programs (never for
  access management), `X-Frame-Options: DENY`.
- SSH: public-key only. adb (root shell without login on 5555) is stopped by the hook and filtered.
- The firewall allows only the service ports; the vendor's WAMP router (9998/9999) is internal only.
- The vendor cloud endpoints for OTA are blocked in `/etc/hosts` (StockRoot); Cortana/OTA/crash upload are not started.
- The web interface can use HTTPS: own certificate (`WEB_TLS="on"`, `tls.go`) or Let's Encrypt over the DNS challenge
  (`WEB_TLS="acme"`, `https.go`: certmagic, the certificate core of Caddy, with libdns providers for Cloudflare, deSEC,
  Gandi, Porkbun, Namecheap and ACME-DNS (together about 110 KB); netcup in `dns_netcup.go` (replaces leftover challenge
  records in the same update); `acme_wait.go` waits until every authoritative nameserver returns the challenge value,
  asked over public resolvers, because netcup publishes changes to its servers minutes apart; Hetzner Console in `dns_hetzner.go`, because libdns/hetzner/v2 would add about 4.5 MB). Certificates and the ACME
  account live in `/data/leuchtfeuer/acme`; names other than `ACME_DOMAIN` (IP address, `.lan`) and the time before the
  first certificate get the own certificate. The provider's token and password are stored encrypted in the config
  (`secrets.go`: AES-256-GCM, key `secret.key` outside the backup, the config key as additional data); optional CNAME
  delegation of `_acme-challenge` (`ACME_DNS_ALIAS`, certmagic `OverrideDomain`). MQTT can use TLS. Sessions are stored only as
  SHA-256 of the cookie value; expired sessions and old failed logins are cleared every 10 minutes.
- Updates from the web interface need a valid Ed25519 signature (`UPDATE_PUBKEY`); without a key the feature is off.
- Bluetooth pairing needs no PIN, but is only possible during the 2-minute window after a press on the speaker's
  Bluetooth button (default). `BLUETOOTH_PAIRING="always"` makes it permanently open to anyone in range.
- mDNS announcements come from avahi (Tidal), librespot (libmdns) and castrecv.
