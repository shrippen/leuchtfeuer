# Installation guide

Complete path from a factory Invoke to the state described in the [README](../README.md). The hardware parts
(1-3) were done once on one device; the software part (4) is automated by `build.sh` and `install.sh`.

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
   includes the LADSPA tap plugin for the light ring visualizer),
8. shows a summary and asks before it changes anything,
9. on a **fresh StockRoot device**: connects over adb (port 5555), checks root, firmware and free space, installs the own
   dropbear with a per-device host key and your key, adds the autostart hook to `/data/dnsmasq.conf` (original saved as
   `dnsmasq.conf.orig`), waits for SSH (checking the host key that it generated),
10. copies all files over SSH (only changed ones), closes adb, offers a reboot, and finally runs
    `scripts/verify-install.sh`.

Messages are in English or German depending on `$LANG` (force with `INVOKE_LANG=en|de`). Options skip questions:
`--ip`, `--key`, `--config`, `--tidal`/`--no-tidal`, `--no-reboot`, `--dry-run`. **Non-interactive** (scripts):
`./install.sh --non-interactive --ip <ip> --key <pub> [--config FILE] [--no-tidal]`. Running it again later updates the
speaker; only changed files are transferred, and your settings on the speaker are kept unless you choose new ones.
Build separately with `./build.sh [--no-tidal] [--force]`.

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

The volume knob sets all sources. Logs are in `/data/invoke/log/` on the speaker
(`ssh root@<ip> 'tail -f /data/invoke/log/*.log'`).

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
  colour, brightness, start LED; off by default). Volume knob, mute, alarm, timer and buttons keep their own animations.

### Discovery across Wi-Fi ↔ LAN

Many routers (e.g. FRITZ!Box) do not forward multicast between Wi-Fi and LAN. A PC on the LAN then does not see
the speaker via mDNS although phones on the Wi-Fi do. Workarounds: use the IP (Cast known hosts,
`SENDSPIN_SERVER`), or put the client on the Wi-Fi.

## 6. Maintenance

- **Update:** `git pull && ./build.sh && ./install.sh --ip <ip> --key <pub>`.
- **Verify:** `scripts/verify-install.sh --ip <ip> --key <pub>` (also checks the visualizer plugin and the vendor `audio-ui`).
- **Do not kill `mcu-interface`** (vendor ring/amplifier controller): the vendor supervisor then restarts its stack in recovery
  mode, `audio-ui` drops off the router and the amplifier stays muted. A reboot of the speaker fixes it.
- **Emergency brake:** `ssh root@<ip> 'touch /data/invoke/disable-hook'`, reboot → original behaviour.
- **Uninstall:** `./uninstall.sh --ip <ip> --key <pub> [--purge]` (removes the autostart hook, restores
  `dnsmasq.conf`, reboots; `--purge` also deletes `/data/invoke`).
- **Factory state:** the vendor firmware can be flashed again with the vendor tool (`l2nand -m 83` with the vendor
  image); restore `factory_setting` from your backup if it was damaged.

## 7. Troubleshooting

| Symptom | Check |
|---|---|
| `adb shell id` is not root | Not StockRoot (firmware 12.x has adbd off and port 22 closed) – part 2 |
| `install.sh` slow or timing out | Wi-Fi link (ping loss); move the speaker, re-run – unchanged files are skipped |
| SSH refuses | `--key` must be the public key matching your private key / agent; too many agent keys can exhaust dropbear's 10 tries → `-o IdentitiesOnly=yes` |
| Service missing | `ssh root@<ip> 'ps; tail /data/invoke/log/<service>.log'`; the hook restarts dead services every 30 s |
| Bluetooth not visible | `scripts/verify-install.sh`; `/data/invoke/log/bluetooth-*.log`; `hciconfig hci0` must say `UP RUNNING PSCAN ISCAN` |
| Music Assistant says "legacy mode" for Sendspin | Expected: sendspin-go 1.8.x speaks the unencrypted dialect; accepted while "Allow legacy clients" is on |
| No sound, "audio-ui not reachable" | `scripts/verify-install.sh` (visualizer plugin, `audio-ui`); a reboot of the speaker usually fixes it. If `/data/invoke/lib/ladspa/invoke-viz-tap.so` is missing, run `install.sh` again (the audio chain needs it) |
| Tidal login fails | iFi certificate may have been revoked; not fixable here |
