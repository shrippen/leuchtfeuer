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
| `volume-sync.sh` | shell | – | see above |
| `bluetooth-1..4-*.sh` | bluetoothd, `btagent`, bluealsa, bluealsa-aplay | – | see below |

## Bluetooth

The vendor stack (Bluedroid) forgot pairings and was unreliable. It is replaced by **BlueZ 5.50**
(`tools/build-bluez.sh`; state under `/data/invoke/bluez/var` so pairings persist):

1. `bluetooth-1`: loads the kernel module `bt8xxx` (Marvell SD8887) with its firmware, brings `hci0` up (the chip
   needs longer on a cold start than bluetoothd waits), starts `bluetoothd -n`. Kernel 3.8 has HCI/L2CAP/SCO/RFCOMM;
   the chip supplies its own BD address. `main.conf`: class *speaker*, BR/EDR only, no discoverable/pairable timeout.
2. `bluetooth-2`: **`btagent`** (`src/btagent`): BlueZ agent `NoInputNoOutput`, accepts every pairing and service
   authorization, marks paired devices *Trusted*, keeps the adapter powered, discoverable and pairable.
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

## Build system

`build.sh` runs `tools/build-*.sh`. Toolchains are Docker images: `tools/docker/xenial-armhf*.Dockerfile` (Ubuntu
16.04 cross, glibc 2.23 like the speaker; GStreamer/GLib headers), `armv7-musl.Dockerfile` (static dropbear and
librespot), `rust-armv7.Dockerfile`. The Go programs are pure Go (`CGO_ENABLED=0`) except sendspin-go (cgo, miniaudio).
Outputs go to `build/` (not in git).

## Security notes

- SSH: public-key only. adb (root shell without login on 5555) is stopped by the hook and filtered.
- The firewall allows only the service ports; the vendor's WAMP router (9998/9999) is internal only.
- The vendor cloud endpoints for OTA are blocked in `/etc/hosts` (StockRoot); Cortana/OTA/crash upload are not started.
- Bluetooth pairing is deliberately open (no PIN) – anyone in range can pair while the speaker is discoverable
  (always). Turn discoverability down in `btagent` if that is not acceptable for your environment.
- mDNS announcements come from avahi (Tidal), librespot (libmdns) and castrecv.
