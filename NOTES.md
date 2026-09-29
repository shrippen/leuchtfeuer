# Invoke Hack – Notizen

Stand: 2026-09-29

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

Ungetestet, weil das Gerät noch nicht im Flashing-Modus war. Offene Risiken beim
`backup`-Boot: Der SDK-Kernel stammt vom Marvell-Referenz-Dongle (2014), nicht vom Invoke;
er kann hängen oder NAND-ECC anders auslesen (dann schlägt die rootfs-Prüfung fehl). Er
schreibt nicht auf den NAND, solange er die BBT findet (gleiche Lage bei 255 MiB wie im
SDK-Layout); alle mtd-Partitionen sind read-only.

## Plan

1. udev-Regel installieren (sudo), Flashing-Modus, `tools/usb-ramboot.sh backup`,
   `scripts/nand-backup.sh`. Sicherung doppelt ablegen.
2. Klären, welche Firmware drauf ist (`rootfs`-Abbild -> `etc/build.info`).
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
