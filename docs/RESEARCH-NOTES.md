# Invoke Hack – Forschungsnotizen

> Arbeitsnotizen aus der Entstehung des Projekts (deutsch), bereinigt um gerätespezifische und persönliche
> Angaben (Kennungen, Adressen, Schlüssel). Sie halten fest, *wie* die Erkenntnisse zustande kamen.
> Der **aktuelle Stand** und die Anleitung stehen in der [README](../README.de.md) und [INSTALL.de.md](INSTALL.de.md);
> spätere Änderungen (BlueZ statt Bluedroid, Cast-Empfänger, Lautstärke-Abgleich) in [ARCHITECTURE.md](ARCHITECTURE.md).
> Der Abschnitt „Plan“ ist historisch.



## Hardware

- Harman Kardon Invoke (Codename „Podium“, Build-Produkt „barracuda“), Rechenplatine
  Libre Wireless LS9ADAC11DBT: Marvell **BG2CD** (88DE3005, 2x Cortex-A9, armv7 hf),
  WLAN/BT **Marvell SD8887** (SDIO, Treiber `wlan_sd8887`, `bt8xxx.ko`), NAND 256 MiB
  (Seite 2048 B, 64 Seiten/Block = 128 KiB, OOB 128 B), DDR3.
- Software: Yocto-Linux mit Android-Teilen (Android-`init`, `adbd`, `logcat`), Kernel
  3.8.13, glibc 2.23, ALSA, GStreamer 1.10, Bluedroid (`/bin/bluetooth`).

## Firmware-Stände (alle drei im Projekt vorhanden, `scripts/fetch.sh`)

| Abbild | Version | Eigenschaften |
|---|---|---|
| `flashing/.../83_IMAGE` (Harman) | Barracuda_libre-11.1842.0 | letzte mit Cortana/WLAN; Port 22 per iptables zu, adbd aus, root gesperrt |
| `firmware/83_IMAGE` (coggy9 „StockRoot“) | Barracuda_rooted_libre-11.1842.0 | = Harman 11.1842 + `root::` (leeres Passwort), Port 22 offen (dropbear `-B`), adbd an, OTA-Server in `/etc/hosts` gesperrt |
| `ota/OTA2/83_IMAGE` (Harman, 2021) | Barracuda_libre-12.2134.0 | **vermutlich auf dem Gerät**: Cortana/Spotify entfernt, neuer Dienst `wifi-blocker` (sperrt WLAN ab 2021-09-11, `/etc/podium/wifi-blocker.conf`), adbd aus, Port 22 zu |

Unterschiede Harman 11 -> StockRoot nur in `init.rc` (adbd), `etc/passwd`, `usr/sbin/firewall.sh`,
`etc/hosts`. Die Binärdateien sind gleich gebaut.

### NAND-Layout (aus `cmdline.txt` in `bootimgs`)

```
mtdparts=mv_nand:128K(block0),1M(pre-bootloader),2M(post-bootloader),2M(postbootloaderB),
5M(factory_setting),5M(tz_en),1M(tz_en-B),10M(bootimgs_B),5M(bsl),10M(bootimgs),90M(rootfs),
123M(app),1M(fw_stat)          # Rest 896K: BBT-Bereich
```

- mtd4 `factory_setting` (yaffs2) -> `/factory_setting`: gerätespezifisch (MAC, Zertifikate,
  Kalibrierung). **Steht in keinem Abbild – nur in der eigenen Sicherung.**
- mtd10 `rootfs` squashfs (read-only).
- mtd11 `app` (yaffs2, 123 MiB) -> `/lsync`, **beschreibbar und dauerhaft**:
  `/data -> /lsync/data1`, `/config -> /lsync/misc/config`.
- Das Flash-Abbild (`83_IMAGE`) hat einen Kopf (Magic `f1a3add2`, Einträge à 64 Byte:
  Name, Größe, CRC, Startblock, Blockzahl) und enthält block0, pre-/post-bootloader, tz_en,
  bootimgs(_B), rootfs, app (leeres yaffs2 = Werksreset von `/lsync`), bsl – **nicht**
  factory_setting.

### Mögliche Autostart-Haken aus `/data` (ohne rootfs zu ändern)

- `/etc/dnsmasq.conf -> /data/dnsmasq.conf`; dnsmasq läuft als root, sobald wlan0 oben ist
  (`run-dnscacher.sh`). Mit `dhcp-script=` + `leasefile-ro` + einer `dhcp-range` für ein
  nicht vorhandenes Netz ruft dnsmasq das Skript beim Start auf („init“).
- ALSA: `/etc/asound.conf` lädt `$ALSA_CONFIG` (Vorgabe `/etc/asound-product.conf`);
  `/data/asound-current.conf` wird zwar angelegt, aber nur von `test-service` benutzt –
  als Autostart-Haken wohl nicht brauchbar.
- Bluetooth-Konfiguration liegt beschreibbar in `/data/bluetooth` (Kopie von
  `/etc/bluetooth_orig`) – Ansatzpunkt für die BT-Fehler.

## Service-USB / Flashing-Modus

- Im Normalbetrieb meldet sich der Invoke am USB **nicht** (12.x: kein adbd, kein Gadget;
  am 2026-09-29 kein Gerät in `lsusb`). `/dev/ttyACM0` hier ist der Steam-Controller-Puck,
  nicht der Invoke.
- Flashing-Modus (Boot-ROM, USB `1286:8174`): Strom ab, **Reset-Loch halten**, Strom an,
  innerhalb 5 s **genau 4x Mic-aus**, Ring wird gelb, Reset loslassen, wenn U-Boot da ist.
- `usb_boot` (Marvell) schiebt `bcm_erom`, `drm_erom`, `sysinit`, `bootloader.img` (U-Boot)
  in den RAM; Konsole über Telnet `127.0.0.1:8141`. Das Tool kann nur Host->Gerät laden,
  keine Daten zurücklesen.
- U-Boot-Befehle aus der Harman-Anleitung: `l2nand 83` (flasht `83_IMAGE`),
  `usbload <nr> <addr>`, `bootm`. `bootloader.img` ist verschlüsselt, weitere Befehle unbekannt.

## Werkzeuge im Projekt

- `tools/99-invoke-marvell.rules` – udev-Regel (Boot-ROM + ADB), einmalig mit sudo installieren.
- `tools/usb-ramboot.sh uboot|backup` – RAM-Boot ohne NAND-Zugriff (83_IMAGE wird bewusst
  nicht ins Arbeitsverzeichnis kopiert, 79_IMAGE wird auf Schreibbefehle geprüft).
  - `backup`: Marvell-SDK-Kernel `81_IMAGE` + Ramdisk `82_IMAGE`, deren `rcS` durch
    `tools/mkramdisk.py` ersetzt ist (hängt nichts ein, startet nur adbd). Kernel bekommt
    das Invoke-Layout, **alle Partitionen `ro`**, plus `nand` = ganzer Chip.
- `scripts/nand-backup.sh` – liest per ADB jede Partition zweimal (nanddump, 16-MiB-Stücke),
  vergleicht Prüfsummen Gerät/Host, liest den ganzen Chip roh mit OOB, dann
  `scripts/verify-backup.py` (Partitionen == Rohabbild ohne OOB, rootfs = SquashFS,
  factory_setting nicht leer). Ergebnis in `backup/<zeit>/` (nicht im Git).

Am 2026-09-30 erfolgreich benutzt (siehe unten).

## Sicherung 2026-09-30 (erledigt)

- `backup/<zeit>/` (nur lokal, Rechte 700; enthält Geräteschlüssel aus
  factory_setting und WLAN-Zugangsdaten aus `app`). Alle 14 Partitionen + ganzer Chip roh
  (2048+64 B je Seite), jede Partition zweimal gelesen, Prüfsummen Gerät = Host,
  Partitionen == Rohabbild ohne OOB. `SHA256SUMS` im Ordner.
- **Installierte Firmware: Barracuda_libre-12.2050.3** (nicht 12.2134): `wifi-blocker` mit
  Datum 2021-08-01, adbd aus, `start sshd` (dropbear `-B`) aktiv, aber Port 22 per iptables
  gesperrt (außer bei gesetztem Debug-Flag im Gerätezertifikat), root `x` (gesperrt).
  `/etc/dnsmasq.conf -> /data/dnsmasq.conf` auch hier.
- rootfs-SquashFS besteht `7z t` fehlerfrei -> ECC des SDK-Kernels passt zum Invoke.
- NAND: Toshiba 0x98/0xda, 256 MiB, Seite 2048, OOB 64 (nicht 128), Block 128 KiB.
  BBT gefunden (Seiten 131008/130944, v1) -> Kernel musste nichts schreiben.
  Schlechte Blöcke: 0x0c000000, 0x0c020000 (in `app`) + 4 BBT-Blöcke in `tail`.
- ECC-Fehler (2 Bit, beide Lesevorgänge gleich) nur in `fw_stat` Offsets 0x47000-0x4e800
  (16 Seiten); OOB liegt im Rohabbild vor.
- factory_setting: 14 belegte Seiten ab 0x1e0000 (yaffs2): Libre-Gerätezertifikat
  (`lsc-certchain`, CN = Geräte-ID) und privater Schlüssel.
- U-Boot per USB: „U-Boot 2013.04 (Apr 11 2016) Marvell … MV88DE3006“, DRAM 512 MiB,
  „Flash: 16 MiB“ (SPI-NOR vorhanden!), „environment in SPI flash is invalid“, OTP ohne
  Kundenschlüssel (RKEK/Sign-Felder 0) – Secure Boot offenbar nicht aktiv.
- Der Flashing-Modus braucht ggf. mehrere Anläufe: das Boot-ROM fordert 08_IMAGE an und
  setzt sich 2-3x zurück, bevor die Kette (09, sysinit, drm_erom, bootloader, 79, 81, 82)
  durchläuft. busybox `nanddump` scheitert an ro-Partitionen (öffnet O_RDWR) -> toolbox
  `nandread` verwenden. Die Ramdisk-.profile gibt bei jedem `adb shell` „/“ aus.

## StockRoot geflasht 2026-09-30 mit `l2nand -m 83`

- `firmware/83_IMAGE` = StockRoot (sha256 f59d0a56…6cc7), Kopf/CRC ok (`tools/check83.py`).
  Nur rootfs anders als Harman 11.1842: 12 Dateien (init.rc, passwd, hosts, firewall.sh,
  build.info, distro_version, version.txt, motd, 4 Sounddateien). `firewall.sh` hat einen
  leeren `if`-Block -> Syntaxfehler -> keine DROP-Regel, alle Ports offen.
- Einträge im Abbild (Startblock+Anzahl, 128 KiB): block0 0+1, pre-bootloader 1+8,
  post-bootloader 9+16, tz_en 81+40, bootimgs_B 129+80, bsl 209+40, bootimgs 249+80,
  rootfs 329+720, app 1049+984 (flags=1). factory_setting (41–80), postbootloaderB,
  tz_en-B, fw_stat, BBT stehen nicht drin.
- **U-Boot `help l2nand`:** „l2nand 83 … load image … to DDR addr 0x4000000, **erase all NAND
  content**, and burn the image to NAND. l2nand -m 84 … burn it to NAND, **without erase all
  NAND content**.“ -> das offizielle `l2nand 83` löscht den ganzen Chip, also auch
  factory_setting (und evtl. BBT). Deshalb stattdessen **`l2nand -m 83`** verwendet:
  löscht nur die Blöcke jedes Eintrags („N blocks erased“), schreibt, liest per CRC zurück
  (pre-bootloader 8 Kopien), app mit OOB (yaffs2), schlechte Blöcke 0x0c000000/0x0c020000
  übersprungen, „Congratulations! u2nand succeed!“. U-Boot meldet „oob size: 32B, ecc
  48bits/2kB“, Chip 98DA90157616, unrandomized. Log: `recon/flash-<zeit>.log` (nicht im Git).
- Danach ohne Neustart Sicherungs-Ramdisk + `scripts/verify-after-flash.sh`: factory_setting
  (Daten und Daten+OOB), postbootloaderB, tz_en-B, fw_stat, tail/BBT == Sicherung; block0,
  pre-/post-bootloader, tz_en, bootimgs(_B), bsl, rootfs == Abbild. **Alles OK.**
- Weitere U-Boot-Befehle: nandinit, nandbad, nanderase, nandrd, nandrdoob, nandwr,
  nandverify, nandmarkbad, usbload, u2nand/usb2nand, tftp2nand, b2nand, spinit.
- Werkzeuge: `tools/usb-flash.sh` (U-Boot-Konsole per FIFO), `tools/uboot-send.sh`,
  `scripts/verify-after-flash.sh` (factory_setting inkl. OOB, B-Partitionen, fw_stat, BBT
  gegen Sicherung; geschriebene Partitionen gegen Abbild) – am ungeflashten Gerät getestet.

## Plan

1. udev-Regel installieren (sudo), Flashing-Modus, `tools/usb-ramboot.sh backup`,
   `scripts/nand-backup.sh` – **erledigt**. Offen: zweite Kopie außerhalb dieses Rechners.
2. Firmware: 12.2050.3 – **erledigt**. StockRoot 11.1842 per `l2nand -m 83` geflasht – **erledigt**.
3. Weg zu WLAN + SSH (jeder Weg braucht einen Schreibzugriff auf den NAND):
   - a) **StockRoot 11.1842 mit dem Harman-Verfahren flashen** (`l2nand 83`): WLAN-Einrichtung
     über den Setup-AP (192.168.43.1), danach `ssh root@<ip>` (leeres Passwort) und adbd.
     Sofort eigenen Schlüssel + eigenes dropbear ohne Passwort-Login einrichten.
     Offen: ob `l2nand` factory_setting unangetastet lässt (steht nicht im Abbild) – vorher
     Sicherung, im Zweifel factory_setting danach aus der Sicherung prüfen.
   - b) 12.x behalten und nur `/data` beschreiben (Haken oben, `wifi-blocker` beenden):
     bräuchte Schreibzugriff auf yaffs2 aus der RAM-Ramdisk mit fremdem Kernel – riskanter
     als a).
4. Dienste (statisch/armhf, Start über Haken aus `/data`, Dateien unter `/lsync`):
   librespot (Spotify Connect), gmrender-resurrect (UPnP/DLNA, GStreamer 1.10 ist da),
   Sendspin-Client, Tidal Connect (nur als proprietäres Binary für Raspberry-Pi-Systeme
   verfügbar – Lauffähigkeit mit glibc 2.23 / Kernel 3.8 offen). Ausgabe über ALSA
   (`dmix` „volmix_music“ bzw. `pcm.dsp`, card 1, 48 kHz S32_LE).
5. Bluetooth: Logs von `/bin/bluetooth`, `bt_stack.conf` in `/data/bluetooth`, ggf.
   Firmware `sd8887_bt_a2_new.bin` prüfen.

## Im Heimnetz 2026-09-30

- WLAN über Setup-AP „HK Invoke_XXXXXX“ (offen, 192.168.43.1): POST an
  `/goform/HandleSACConfiguration` (`SSID=…&Security=WPA-PSK`, Passphrase per
  `--data-urlencode`) -> Antwort „continue“, verbindet sofort (2,4 GHz, WPA2-CCMP), DHCP.
  `https://192.168.43.1/getlogcat.asp` liefert das ganze logcat. Der Ring leuchtete vorher rot.
- Invoke: WLAN-MAC (Geräteabhängig), im Heimnetz eine DHCP-Adresse <ip>.
- Offen: 22 (dropbear, nur `ssh-rsa`-Hostkey -> `-o HostKeyAlgorithms=+ssh-rsa`), **5555 adbd
  ohne Anmeldung = root-Shell für jeden im LAN**, 9998/9999 (WAMP), 443, 12345.
- SSH-Passwortlogin geht nicht: `/etc/shadow` hat `root:*` (passwd `root::` wirkt nicht).
  Zugang derzeit nur über `adb connect <ip>:5555`.
- OTA: `ota_rbua_install.sh` läuft („fota-check“), Server (Neptune/saf1/harman-podium.redbend.com)
  zeigen per /etc/hosts auf 127.0.0.1.
- Normaler Kernel: mtdparts endet mit fw_stat 128K, cenv 128K, senv 128K; factory_setting ist
  rw eingehängt (Original-Verhalten). Service-USB meldet sich im Normalbetrieb nicht.

## SSH nur mit Schlüssel, adb aus (2026-09-30, erledigt)

- Das mitgelieferte dropbear 2016.72 kann kein ed25519 -> eigenes statisches dropbear
  2026.94 (armv7 musl, Passwort-Login einkompiliert aus): `tools/build-dropbear.sh`
  (Toolchain `tools/docker/armv7-musl.Dockerfile`), Ergebnis `build/dropbear/dropbearmulti`.
- Auf dem Gerät `/data/invoke/` (= /lsync/data1/invoke): `dropbearmulti`, `host_ed25519`,
  `host_ecdsa` (eigene Host-Schlüssel), `authorized_keys` (öffentlicher Schlüssel des Nutzers), `boot.sh`, `hook.sh` (Quelle: `device/invoke/`), `disable-adb`,
  `hook.log`, `dnsmasq.conf.orig`.
- Autostart: `/data/dnsmasq.conf` hat am Ende `dhcp-script=/data/invoke/boot.sh`,
  `leasefile-ro`, `dhcp-range=10.254.254.10,10.254.254.20,1h` (in keinem Netz) -> dnsmasq
  ruft beim Start (sobald wlan0 oben ist) `boot.sh init` auf -> `hook.sh` (Schleife 30 s):
  tmpfs über /home/root + authorized_keys, `stop sshd` + eigenes dropbear auf 22,
  `stop adbd`, iptables-Kette INVOKE (22, mDNS, DHCP-Antworten, ICMP, bestehende; auf p2p0
  zusätzlich Setup-Ports; weitere aus `/data/invoke/ports.local`), IPv6 aus (kein ip6tables).
  Startet das eigene dropbear nicht, gehen Original-sshd und adbd wieder an.
  Notbremse: `/data/invoke/disable-hook`.
- dropbear prüft die Rechte aller Elternverzeichnisse von authorized_keys (/data ist 777,
  /run 1777) -> deshalb tmpfs mit Modus 700 über /home/root.
- Geprüft nach echtem Neustart (`/bin/reboot` = toolbox; busybox `/sbin/reboot` tut nichts):
  eigener Host-Schlüssel (pro Gerät), nur „publickey“,
  5555/443/9998/9999/53 gefiltert, adbd aus, IPv6 aus, redbend-Sperre in /etc/hosts aktiv.
- `getprop` geht in SSH-Sitzungen und im Haken nicht (ANDROID_PROPERTY_WORKSPACE fehlt),
  `start`/`stop`/`setprop` gehen.
- Name **invoke.lan** (frei wählbar, `DHCP_HOSTNAME`): Das Libre-dhcpcd sendet `-h LibreSync-279012855` (Name aus dem Libre-NV,
  `getFromNVDeviceName`), der Router trägt das nicht ein. `hook.sh` schickt deshalb nach dem
  Start und alle 6 h `busybox udhcpc -r <ip> -x hostname:invoke -s /bin/true` (Name aus
  `/data/invoke/hostname`) -> Router trägt `invoke.lan` ein. Nach Neustart geprüft.
  Nicht funktioniert: `dhcpcd -n` (hängt), eigener dhcpcd-Neustart (bekommt kein Lease),
  eigener mDNS-Responder für invoke.local (Router leitet mDNS nur per Proxy weiter; verworfen).
  Verbindungen aus dem LAN kommen am Invoke mit der Router-Adresse als Quelle an (Router-NAT zwischen
  LAN und WLAN).
- Anmelden: `ssh -i <schlüssel> -o IdentitiesOnly=yes root@<ip>` (IdentitiesOnly nötig, wenn der Agent >10
  Schlüssel hat; dropbear erlaubt 10 Versuche).

## Musikdienste (2026-09-30)

Alle als Dienste unter `/data/invoke/services/*.sh` (Quelle `device/invoke/services/`), von
`hook.sh` gestartet und bei Absturz neu gestartet, Logs `/data/invoke/log/`. Programme in
`/data/invoke/bin/`. Freigegebene Ports in `/data/invoke/ports.local`. Name überall „HK Invoke“.

- **Audio-Weg:** ALSA-PCM `music` = softvol „music“ (Karte 0) -> dmix `volmix_music` -> `dsp`
  (Karte 1 wm8904, 48 kHz S32_LE). dmix teilt sich das Gerät mit den Harman-Diensten.
  Standardgerät `default` zeigt ins Loopback (stumm) -> für miniaudio-Programme
  `ALSA_CONFIG=/data/invoke/asound-music.conf` (bindet asound-product.conf ein, default=music).
- **Spotify Connect:** librespot 0.8.0 statisch (armv7 musl, rustls, libmdns),
  `tools/build-librespot.sh` (`tools/docker/rust-armv7.Dockerfile`). Ausgabe über
  subprocess-Backend an `aplay -D music` (Geräte-alsa-lib, kein zweites dmix-Layout).
  Zeroconf 57500/tcp. Das Harman-`spotify` (eSDK) läuft weiter, ist aber tot
  (esdk-ffl.spotify.com gibt es nicht mehr). **Nicht anhalten:** SIGSTOP -> fehlender Heartbeat
  -> system-manager startet den ganzen Podium-Stack neu (adbd kam kurz zurück, hook stoppt ihn).
- **UPnP/DLNA:** gmrender-resurrect (Commit 3d87b36) + libupnp 1.14.31 statisch, gegen glibc
  2.23 (Xenial-armhf-Cross, `tools/build-gmrender.sh`, `tools/docker/xenial-armhf.Dockerfile`),
  GStreamer/GLib des Geräts. 49494/tcp, 1900/udp. Getestet: Ton per SetAVTransportURI/Play
  vom Rechner abgespielt (PLAYING -> STOPPED am Ende).
- **Sendspin:** sendspin-go v1.8.2 Player (Go + cgo, glibc 2.23, libopus 1.5.2 statisch,
  miniaudio lädt libasound des Geräts), `tools/build-sendspin.sh`. Der Player verbindet sich
  selbst (ws://…:8927/sendspin); mDNS-Suche geht über den Router nicht -> fest
  `--server <music-assistant>:8927` (Music Assistant 2.10, `SENDSPIN_SERVER` in `/data/invoke/config`).
  Verbunden, client_id = WLAN-MAC. Angebot auf 48 kHz/24 bit begrenzt.
- **Tidal Connect** (auf ausdrücklichen Wunsch des Nutzers; iFi-Binary 1.1.3 mit
  iFi-Zertifikat `IfiAudio_ZenStream.dat`, nicht für das Gerät lizenziert, kann gesperrt
  werden): `tools/build-tidal-bundle.sh` -> `/data/invoke/tidal/` (~11 MB). Läuft mit der
  glibc 2.23 des Geräts (braucht nur ≥ 2.9); mitgeliefert: FFmpeg 3.4 minimal
  (`tools/build-ffmpeg34.sh`, Sonamen 57/57/55/2, alle 35 benötigten Symbole vorhanden),
  libstdc++ 6.0.22 (GLIBCXX_3.4.22), portaudio/FLAC++/avahi aus stretch, libssl 1.0.1t +
  libcurl3 7.38 aus jessie (Debian-Symbolversionen OPENSSL_1.0.x/CURL_OPENSSL_3).
  Dienste `tidal-1-dbus` (dbus-daemon des Geräts, eigene Konfig), `tidal-2-avahi` (avahi
  0.6.32 stretch; LD_PRELOAD-Shim `device/src/avahi-user-shim.c` liefert Benutzer „avahi“),
  `tidal-3-connect` (`--netif-for-deviceid wlan0` nötig, sonst ioctl-Fehler). Websocket
  2019/tcp. avahi-Hostname `hk-invoke` (bei „invoke“ Namenskonflikt, vermutlich meldet der
  Router den DHCP-Namen selbst per mDNS). Auf dem Gerät per avahi-browse geprüft:
  `_tidalconnect._tcp` „HK Invoke“ und `_spotify-connect._tcp` „HK Invoke“ sichtbar
  (vom kabelgebundenen Rechner aus nicht – der Router reicht mDNS nicht von WLAN nach LAN).
- **Harman-Dienste gekürzt:** `hook.sh` legt beim Start `/data/invoke/podium.conf` per
  Bind-Mount über `/etc/podium/podium.conf` und startet den init-Dienst `podium`
  (system-manager) neu. Entfernt: cortana-harness, spotify, ota_rbua_install.sh,
  crash-uploader-HK.sh. Weiter aktiv: mcu-interface, dsp-client, audio-ui,
  connection-manager, music-source-manager, bluetooth, device_auto_recovery.sh.
  Danach offen (intern, von außen gefiltert): 7777 luci_service, 9998/9999 bonefish (WAMP).
- **Sendspin-CPU:** Beim Abspielen hält ein miniaudio-Thread einen Kern bei 100 % (nach
  Ende teils weiter). Vermutung mmap auf dmix/softvol; `tools/build-sendspin.sh` setzt jetzt
  `Alsa.NoMMap = 1` – **noch nicht getestet** (WLAN brach beim Test ein), auf dem Gerät läuft
  noch der alte Build.
- **WLAN-Problem 2026-09-30 01:30:** Invoke hängt an einem schwachen Access Point (Kanal 6,
  −69 dBm, Qualität 3/5), Ping 80 % Verlust / ~950 ms, scp ~40 kB/s; an einem besseren AP desselben Netzes
  problemlos. Power-Management ist aus.
- Last mit allen drei Diensten: Load ~0,3, ~60 MB RAM belegt, SoC 78 °C (Harman-Abschaltung
  bei 95 °C, `device_auto_recovery.sh`). Harman-`cortana` braucht die meiste CPU.
