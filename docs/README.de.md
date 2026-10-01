# HK Invoke Hack

Macht aus einem **Harman Kardon Invoke** (dem Cortana-Lautsprecher, den Harman aufgegeben hat) einen
normalen Netzwerk-Lautsprecher. Nach der Installation erscheint er als **„HK Invoke“** in:

| Was | Wie | Hinweise |
|---|---|---|
| **Spotify Connect** | [librespot](https://github.com/librespot-org/librespot) | braucht ein Spotify-Premium-Konto |
| **UPnP / DLNA-Renderer** | [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect) | z. B. mit Symfonium, BubbleUPnP, foobar2000 |
| **Sendspin** (Music Assistant) | [sendspin-go](https://github.com/Sendspin/sendspin-go) | „Legacy“-Dialekt (unverschlüsselt), siehe unten |
| **AirPlay** (AirPlay 1) | [shairport-sync](https://github.com/mikebrady/shairport-sync) 3.3.9 | iPhone, iPad, Mac; AirPlay 2 und Multiroom werden nicht unterstützt |
| **Google Cast** (Audio) | eigene Empfänger-Nachbildung (`src/castrecv`) | nur für Sender ohne Geräteprüfung durch Google: Music Assistant, Home Assistant, VLC, pychromecast. **Nicht** YouTube / Spotify-Cast / Chrome-Tab |
| **Tidal Connect** (optional) | proprietäres iFi-Programm | **nicht für dieses Gerät lizenziert**, kann gesperrt werden – nur auf Wunsch, siehe [Rechtliches](#rechtliches-und-risiken) |
| **Bluetooth-A2DP-Empfänger** | BlueZ 5.50 + bluez-alsa + eigener Agent (`src/btagent`) | unsichtbar, bis du den **Bluetooth-Knopf** am Lautsprecher drückst (2 Minuten Pairing-Fenster, Rückmeldung am Leuchtring); koppelt ohne Abfrage, merkt sich Kopplungen, Handy-Lautstärke und Drehrad bleiben synchron |

**Weboberfläche** (Port 80, Design [Kante](https://github.com/shrippen/shrippen.github.io), Englisch/Deutsch):
Status, **Webradio**, **Wecker** (Anstieg, Schlummern, Radio oder Signalton) und **Timer**, **Tastenbelegung**, der **WLAN-Wächter**
(misst die echte Verbindungsqualität zum Router und wechselt bei anhaltend schlechter Qualität zu einem besseren Access Point
desselben Netzes), **Home Assistant** (MQTT-Erkennung: Lautstärke, Stumm, Webradio, Bluetooth-Kopplung, Timer,
Wecker-Tasten, Temperatur, WLAN, Tasten-Ereignisse) und Einstellungen. Wecker und Timer nutzen deine Zeitzone und die
Animationen des Leuchtrings.

Alles läuft über die eigene DSP-/Verstärkerkette des Lautsprechers. Das **Drehrad** regelt alle Quellen und bleibt
über Bluetooth in beide Richtungen mit der Handy-Lautstärke synchron.

Außerdem: SSH nur mit Schlüssel (eigenes dropbear), die offene Root-Shell auf Port 5555 (adb) ist zu, eine Firewall
lässt nur die nötigen Ports herein, und die Harman-Clouddienste (Cortana, OTA-Updates, Absturzberichte, totes
Harman-Spotify) sind abgeschaltet.

**Kein Ziel:** Cortana, das Wake-Word oder einen Sprachassistenten wiederherzustellen.

## Funktionsweise (kurz)

Der Invoke läuft mit Linux 3.8 (Yocto + Android-Teile) auf einem Marvell BG2CD. Die beschreibbare Partition `/data`
übersteht Neustarts, deshalb wird nach dem ersten Flashen nichts mehr am schreibgeschützten Systemabbild geändert:

1. Ein **gerootetes Stock-Abbild** („StockRoot“ aus [coggy9/HKHacking](https://github.com/coggy9/HKHacking)) liefert
   den ersten Zugang. Es wird mit dem Hersteller-Werkzeug über den Service-USB-Port geflasht, **ohne die
   gerätespezifische Partition `factory_setting` zu löschen**.
2. `dnsmasq` (läuft beim Start als root) dient über ein paar Zeilen in `/data/dnsmasq.conf` als **Autostart-Haken**
   und startet `/data/invoke/hook.sh`, einen kleinen Überwacher: SSH und Firewall einrichten, Harman-Dienste
   kürzen, die Audiodienste am Laufen halten.
3. Alle Programme werden mit Docker und Go für das Gerät gebaut (glibc 2.23 / musl, ARMv7), siehe `build.sh`.

Details: [docs/ARCHITECTURE.md](ARCHITECTURE.md) (englisch). Forschungsnotizen:
[docs/RESEARCH-NOTES.md](RESEARCH-NOTES.md).

## Schnellstart

Voraussetzungen: ein Invoke, ein Linux-PC (entwickelt unter Arch), USB-Kabel für den Service-Port, `adb`,
`docker`, `go` ≥ 1.22, `curl`, `git`, `unzip`, `python3`, `socat`, `telnet`. Der Invoke muss im WLAN erreichbar sein.

```sh
git clone https://git.arianw.de/shrippen/invoke-hack.git && cd invoke-hack

# 1. ZUERST docs/INSTALL.de.md Teile 1-3 lesen: NAND sichern, StockRoot flashen, Lautsprecher ins WLAN bringen.
# 2. installieren: fragt und erklärt Schritt für Schritt (baut bei Bedarf auch alles, Docker, einmalig 20-60 Minuten)
./install.sh
# (ohne Rückfragen: ./install.sh --non-interactive --ip <ip> --key ~/.ssh/id_ed25519.pub)
```

Danach das Handy mit „HK Invoke“ koppeln oder ihn in Spotify / der UPnP-App / Music Assistant auswählen.
Die vollständige Schritt-für-Schritt-Anleitung samt Hardware-Teil steht in **[docs/INSTALL.de.md](INSTALL.de.md)**
([English](INSTALL.md), [README in English](../README.md)).

## Konfiguration

`/data/invoke/config` auf dem Lautsprecher (Vorlage: `device/invoke/config.example`): Gerätename, Adresse des
Music-Assistant-Servers für Sendspin, DHCP-Hostname. Ein erneutes `./install.sh` aktualisiert den Lautsprecher und
lässt diese Datei unverändert, außer man gibt `--config` an.

## Notbremse / Deinstallieren

- `ssh root@<ip> 'touch /data/invoke/disable-hook'` und neu starten: Originalverhalten (dann gilt wieder der gesperrte
  Hersteller-sshd, und adb ist an).
- `./uninstall.sh --ip <ip> --key <pub>` entfernt den Autostart-Haken (`--purge` löscht auch `/data/invoke`).

## Rechtliches und Risiken

- **Du kannst den Lautsprecher unbrauchbar machen.** Flashen des NAND über USB geschieht auf eigene Gefahr. Mach zuerst
  die Sicherung aus [docs/INSTALL.de.md](INSTALL.de.md) Teil 1 und bewahre sie auf: Die Partition `factory_setting`
  (Zertifikate, MAC, Kalibrierung) gibt es nur im Lautsprecher und in dieser Sicherung. Niemals `l2nand 83` ohne `-m`.
- Dieses Projekt **enthält keine Harman- oder Marvell-Firmware**; `scripts/fetch.sh` lädt sie aus den öffentlichen
  Releases von coggy9/HKHacking. Die NAND-Sicherung nicht weitergeben (enthält die Geräteschlüssel).
- **Tidal Connect** nutzt das proprietäre `tidal_connect_application` von iFi audio mit iFis Gerätezertifikat (aus
  [TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker)). Das ist für diesen Lautsprecher
  nicht lizenziert, Tidal kann es jederzeit sperren. Es ist freiwillig: `./build.sh --no-tidal` und
  `./install.sh --no-tidal` lassen es weg.
- Der Cast-Empfänger ist eine **eigenständige Nachbildung** des Cast-Protokolls, keine Google-Software.
- Sendspin verbindet sich derzeit im Legacy-Dialekt (unverschlüsselt); Music Assistant zeigt dazu einen Hinweis.
- Die auf dem Lautsprecher installierten Programme (librespot, gmrender-resurrect, sendspin-go, BlueZ, bluez-alsa,
  dropbear, FFmpeg/avahi im Tidal-Bündel, …) behalten ihre eigenen Lizenzen. `build.sh` holt die genauen Quellen per
  Tag/Commit.

## Lizenz

MIT, siehe [LICENSE](../LICENSE). Das gilt für Code und Dokumentation dieses Repositories; die gebauten und installierten
Programme (librespot, gmrender-resurrect, BlueZ, …) und die Hersteller-Firmware behalten ihre eigenen Lizenzen und Bedingungen.

## Danksagung

[coggy9/HKHacking](https://github.com/coggy9/HKHacking) (StockRoot-Abbild, Flash-Verfahren),
[librespot](https://github.com/librespot-org/librespot), [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect),
[Sendspin](https://github.com/Sendspin/sendspin-go), [BlueZ](https://www.bluez.org/),
[bluez-alsa](https://github.com/arkq/bluez-alsa), [dropbear](https://matt.ucc.asn.au/dropbear/dropbear.html),
[TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker).
