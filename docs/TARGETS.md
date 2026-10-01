# Targets: running Leuchtfeuer on another device

Leuchtfeuer is device-independent. A **target** describes one kind of device: what it must provide (volume) and which
**extensions** it has (buttons, light ring, vendor sounds). Everything else, from receivers and web radio to alarms,
the briefing, Home Assistant, the voice satellite and updates, is shared and works on every target.

| Target | Device | Extensions |
|---|---|---|
| `invoke` | Harman Kardon Invoke (Marvell BG2CD, vendor software "podium") | buttons, ring, vendorSounds, vendorVolume |
| `generic` | any Linux with systemd and ALSA (Raspberry Pi, mini PC, old laptop) | none |

The web interface, MQTT discovery and the API follow `device.capabilities` from `GET /api/status`: a device without a
ring has no ring card, no sunrise light and no ring entity in Home Assistant; a device without buttons has no
"Buttons" tab.

## Installing on any Linux (`generic`)

```sh
tools/build-generic.sh arm64                       # or amd64, armv7; needs Go and a C (cross) compiler
tools/make-release.sh --target generic --arch arm64
# on the device:
mkdir lf && tar -xzf leuchtfeuer-*-generic-arm64.tar.gz -C lf
sudo sh lf/setup.sh --password 'secret' --name Küche
sudo apt install alsa-utils librespot shairport-sync gmediarender snapclient   # whatever receivers you want
```

`setup.sh` installs to `/opt/leuchtfeuer` (`--dir`), writes `config` (port 8080, `ALSA_CARD="0"`), and enables
`leuchtfeuer.service`. Receivers whose program is missing show up as "not installed". If PipeWire or PulseAudio holds
the sound card, set `ALSA_OUTPUT="pipewire"` (or `"pulse"`) in `config`. Bluetooth uses the system's bluetoothd and
bluealsa; switch on `SERVICE_BLUETOOTH` and Leuchtfeuer adds its agent and player. Updates: run `setup.sh` from a newer
package, or from the web interface with a signed package (`UPDATE_PUBKEY`). `setup.sh --uninstall` removes it again
(settings stay; `--purge` deletes them too).

## Adding a new device

Four places, all optional except the first. Take `generic` as the starting point and add only what differs.

### 1. leuchtfeuerd: `src/leuchtfeuerd/target_<id>.go`

```go
func init() {
	registerTarget(target{
		ID: "mybox", Manufacturer: "Acme", Model: "Box 1", DefaultName: "Box",
		Package:   "mybox",                     // release package leuchtfeuer-<version>-mybox.tar.gz
		MixerCard: "0",                         // card of the softvol controls in asound-music.conf
		MirrorCtl: "",                          // vendor ALSA control copied into "Leuchtfeuer Music" ("" = none)
		TempPath:  "/sys/class/thermal/thermal_zone0/temp", TempDiv: 1000,
		WifiIface: "wlan0",
		newVolume: func(a *app) audioCtl { return newSoftVolume(a, filepath.Join(dataDir, "volume.json")) },
		start:     func(a *app) { a.vol.(*softVolume).apply() },
		// extensions:
		buttons:     true,                      // the driver calls a.onButton(name, value)
		ButtonNames: []string{"play", "plus", "minus"},
		ring:        &ringSpec{writer: newMyRing, led: newMyLED},
		SoundDirs:   nil,                       // vendor WAV sounds that can be replaced (vendorSounds)
		Link:        "", linkOK: nil,           // vendor software to show as connected in the status
	})
}
```

| Part | Contract |
|---|---|
| `audioCtl` (required) | `Get`, `SetVolume`, `Adjust`, `SetMute`, `ToggleMute`, `OnChange`. Call the `OnChange` callback after **every** change, also changes made on the device itself (knob). `softVolume` is a complete implementation that owns the state and drives `Leuchtfeuer Music`; use it unless the device has its own volume authority (like `audio-ui` on the Invoke). |
| `start` | starts the driver: subscribe to buttons and vendor events, call `a.onButton(name, value)` for a button (the actions, Home Assistant and the bus event `button` follow from that). |
| `ringSpec.writer` | `ringWriter.Write(*ringFrame)`: shows one frame (12 RGB LEDs on the Invoke) for the visualizer, sunrise light, timer and voice scenes. |
| `ringSpec.led` | `ledAPI.Animate(name, repeat)`, `Off()`: logical animations `alarm`, `timer`, `success`, `bt_open`, `bt_closed`; map them to whatever the device can do and ignore unknown names. |

Add a case to `TestTargetsAndCapabilities` in `target_test.go`.

### 2. Hook: `targets/<id>/target.sh`

Loaded by the shared `device/leuchtfeuer/hook.sh`. Sets defaults and may define four functions:

```sh
TARGET_ID=mybox
TARGET_NAME="Box"              # device name while DEVICE_NAME is empty (LEUCHTFEUER_NAME for the services)
TARGET_FIREWALL=off            # default of FIREWALL (chain LEUCHTFEUER)
TARGET_IFACE=wlan0             # default of WIFI_IFACE ("" = interface of the default route)
TARGET_ALSA_BASE=/usr/share/alsa/alsa.conf   # set when no vendor asound.conf includes $ALSA_CONFIG
target_init(){ :; }            # once at start (bind mounts, restart vendor services)
target_tick(){ :; }            # every 30 s (keep SSH up, switch things off again)
target_fw_rules(){ :; }        # extra iptables rules, one per line, e.g. "-i p2p0 -p tcp --dport 443 -j RETURN"
target_net(){ :; }             # network ready? Called until it succeeds, then every 6 h; the clock is set after it
```

Who starts the hook is up to the device: the Invoke uses a dnsmasq hook (`targets/invoke/boot.sh`), `generic` a systemd
unit (`targets/generic/leuchtfeuer.service`).

### 3. Audio: `targets/<id>/asound-target.conf` and `target.env`

The shared chain (`device/leuchtfeuer/asound-music.conf`: source PCMs, tap, EQ, `Leuchtfeuer Music`,
`Leuchtfeuer Announce`) ends at `@OUT@`. `scripts/assemble.sh` puts `asound-target.conf` in front and fills in the
placeholders from `target.env`:

```sh
DIR=/opt/leuchtfeuer          # installation directory (@LEUCHTFEUER_DIR@, LADSPA path)
OUT=leuchtfeuer_out           # PCM the chain plays into; define it in asound-target.conf unless the system has one
CARD='{ @func getenv vars [ ALSA_CARD ] default "0" }'   # card of the softvol controls
```

The output must accept 48 kHz; softvol needs a real card for its controls.

### 4. Services and package: `targets/<id>/services/`, `targets/<id>/assemble.sh`

Shared services (`device/leuchtfeuer/services`) find their programs via `PATH` (`$LEUCHTFEUER_DIR/bin` first) and
declare them with `# requires:`. Device-only services (the Invoke's BlueZ build and Tidal) go to
`targets/<id>/services/`. `targets/<id>/assemble.sh` is sourced by `scripts/assemble.sh` with `S` (staging directory),
`t`, `DIR`, `ARCH`, `TIDAL` and copies the programs and system files of the device.

### Checks

- `sh tests/hook_test.sh`, `cd src/leuchtfeuerd && go test ./...`
- `sh tests/generic_e2e.sh` shows how a target is tested end to end without the hardware.
- On the device: `scripts/smoke.sh`.
