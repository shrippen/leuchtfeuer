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
- builds an **iptables chain `INVOKE`** (SSH, mDNS, DHCP replies, ICMP, established, plus the ports listed in
  `ports.local`; it re-reads that file every loop), switches IPv6 off, stops `adbd` (`disable-adb` flag),
- **bind-mounts `podium.conf`** over `/etc/podium/podium.conf` and restarts the vendor service manager, which
  drops Cortana, the dead Harman Spotify, OTA and crash upload **and the vendor Bluetooth stack**;
  **bind-mounts `ca-certificates.crt`** (the speaker ships a 2018 CA bundle without Let's Encrypt),
- announces a DHCP host name (`DHCP_HOSTNAME`) so the router shows the speaker as `<name>.lan`,
- starts every executable `services/*.sh` (they end in `exec`), restarts dead ones, rotates their logs
  (`log/<name>.log`, 1 MiB).

Emergency brake: a file `/data/invoke/disable-hook` makes the hook do nothing.

## Audio path

```
service → ALSA "invoke_music" (softvol control "Invoke Music") → dmix "volmix_music" → DSP (card 1, wm8904, 48 kHz)
```

`asound-music.conf` (loaded through the `ALSA_CONFIG` hook of `/etc/asound.conf`) defines `invoke_music` and makes it
the default PCM (with a `hint` block, otherwise miniaudio picks the ALSA `null` device). The vendor services share
the same dmix.

The vendor service `audio-ui` ducks the ALSA control `music` to 0 at every click of the volume knob, which made
music drop out. The services therefore use their **own** control `Invoke Music`, which `audio-ui` does not know.
`services/volume-sync.sh` copies the knob's ALSA control `system` into `Invoke Music` (polls every 0.2 s).

## Services (`services/*.sh`)

| Script | Program | Ports | Notes |
|---|---|---|---|
| `librespot.sh` | librespot 0.8.0 (static musl) | 57500/tcp (zeroconf) | output via `aplay -D invoke_music` subprocess backend |
| `gmrender.sh` | gmrender-resurrect + libupnp (static), GStreamer of the device | 49494/tcp, 1900/udp | UUID derived from the Wi-Fi MAC |
| `sendspin.sh` | sendspin-go 1.8.2 | outgoing 8927 | `Alsa.NoMMap = 1` patch (mmap on dmix/softvol spun a core); `SENDSPIN_SERVER` |
| `castrecv.sh` | `src/castrecv` (Go) | 8009/tcp (TLS), 8008, 8443 | Cast receiver emulation, plays with `gst-launch-1.0` |
| `tidal-1/2/3-*.sh` | iFi `tidal_connect_application` + bundled libs, avahi 0.6.32 (LD_PRELOAD shim for the missing `avahi` user), own `dbus-daemon` config | 2019/tcp | optional |
| `shairport.sh` | shairport-sync 3.3.9 (AirPlay 1, Apple ALAC decoder), tinysvcmdns | 5000/tcp, 6001-6011/udp | `AIRPLAY="off"` disables |
| `invoked.sh` | `src/invoked` | 80/tcp | see below |
| `volume-sync.sh` | shell | – | see above |
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

## Cast receiver (`src/castrecv`)

The firmware image contains no Cast receiver (it was a cloud download for which the speaker lacks the keys). `castrecv`
implements the CASTV2 protocol (TLS on 8009, protobuf framing by hand, namespaces connection/heartbeat/receiver/media),
announces `_googlecast._tcp` (model *Chromecast Audio*) via mDNS and plays `LOAD`ed URLs with GStreamer
(pause/resume by SIGSTOP/SIGCONT; volume sets the knob's control). Senders that verify the device certificate
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
| Web interface | embedded static files + JSON API behind a login page (session cookie, HttpOnly/SameSite=Strict; password stored as salted PBKDF2-HMAC-SHA256 hash in `WEB_PASSWORD_HASH`, set with `invoked -set-password` reading stdin; 5 wrong tries lock an address for a minute); design from Kante (`web/kante/`, vendored with `tools/sync-kante.sh`, never edited by hand) |

The core works against small interfaces (volume, player, LED) and an injectable clock, so the logic runs without a speaker.

**Demo mode and release check.** `tools/build-invoked.sh` builds the release binary and aborts if the demo marker
(`INVOKE-DEMO-BUILD`) is inside. The demo (`-tags demo`, `demo/start.sh`) is a separate build for screenshots with the shared
"Studio Weber" data; it has no command line switch or data in the normal build.

### Light ring

The ring (13 RGB LEDs, 39 bytes per frame, about 25 frames per second) is driven by `mcu-interface` from pattern files
`/usr/share/lights/*.bin`; `ledAnimate(name, {repeat})` plays one, `ledSet("front", …)` drives only the front status LED.
An **audio visualizer was investigated and not built**: it needs a tap of the played audio. The Loopback card can receive a
copy through an ALSA `multi` tee (data arrives), but then GStreamer clients (gmrender/UPnP) fail to open `invoke_music`
(empty hw-params intervals, also with fixed parameters). The ALSA `meter` plugin with a scope is transparent but receives no
payload in the speaker's plug/softvol/dmix chains (its buffer stays zero). Other routes (FIFO tee, reading dmix shared memory)
would block or depend on internals and could stall the main audio path, which was not acceptable.

## Build system

`build.sh` runs `tools/build-*.sh`. Toolchains are Docker images: `tools/docker/xenial-armhf*.Dockerfile` (Ubuntu
16.04 cross, glibc 2.23 like the speaker; GStreamer/GLib headers), `armv7-musl.Dockerfile` (static dropbear and
librespot), `rust-armv7.Dockerfile`. The Go programs are pure Go (`CGO_ENABLED=0`) except sendspin-go (cgo, miniaudio).
Outputs go to `build/` (not in git).

## Security notes

- SSH: public-key only. adb (root shell without login on 5555) is stopped by the hook and filtered.
- The firewall allows only the service ports; the vendor's WAMP router (9998/9999) is internal only.
- The vendor cloud endpoints for OTA are blocked in `/etc/hosts` (StockRoot); Cortana/OTA/crash upload are not started.
- Bluetooth pairing needs no PIN, but is only possible during the 2-minute window after a press on the speaker's
  Bluetooth button (default). `BLUETOOTH_PAIRING="always"` makes it permanently open to anyone in range.
- mDNS announcements come from avahi (Tidal), librespot (libmdns) and castrecv.
