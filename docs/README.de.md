<p align="center"><img src="logo.svg" alt="Leuchtfeuer" width="96" height="96"></p>

# Leuchtfeuer

_Netzwerk-Lautsprecher-Werkzeuge für den Harman Kardon Invoke_

Macht aus einem **Harman Kardon Invoke** (dem Cortana-Lautsprecher, den Harman aufgegeben hat) einen
normalen Netzwerk-Lautsprecher. Nach der Installation erscheint er als **„HK Invoke“** in:

| Was | Wie | Hinweise |
|---|---|---|
| **Spotify Connect** | [librespot](https://github.com/librespot-org/librespot) | braucht ein Spotify-Premium-Konto |
| **UPnP / DLNA-Renderer** | [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect) | z. B. mit Symfonium, BubbleUPnP, foobar2000 |
| **Sendspin** (Music Assistant) | [sendspin-go](https://github.com/Sendspin/sendspin-go) | „Legacy“-Dialekt (unverschlüsselt), siehe unten |
| **AirPlay** (AirPlay 1) | [shairport-sync](https://github.com/mikebrady/shairport-sync) 3.3.9 | iPhone, iPad, Mac; AirPlay 2 wird nicht unterstützt (Multiroom: Snapcast oder Sendspin) |
| **Google Cast** (Audio) | eigene Empfänger-Nachbildung (`src/castrecv`) | nur für Sender ohne Geräteprüfung durch Google: Music Assistant, Home Assistant, VLC, pychromecast. **Nicht** YouTube / Spotify-Cast / Chrome-Tab |
| **Tidal Connect** (optional) | proprietäres iFi-Programm | **nicht für dieses Gerät lizenziert**, kann gesperrt werden – nur auf Wunsch, siehe [Rechtliches](#rechtliches-und-risiken) |
| **Snapcast** (optional) | [snapclient](https://github.com/badaix/snapcast) 0.31 | Multiroom synchron mit anderen Snapcast-Lautsprechern; anfangs aus, braucht deinen snapserver |
| **Bluetooth-A2DP-Empfänger** | BlueZ 5.50 + bluez-alsa + eigener Agent (`src/btagent`) | unsichtbar, bis du den **Bluetooth-Knopf** am Lautsprecher drückst (2 Minuten Pairing-Fenster, Rückmeldung am Leuchtring); koppelt ohne Abfrage, merkt sich Kopplungen, Handy-Lautstärke und Drehrad bleiben synchron |

**Weboberfläche** (Port 80, optional HTTPS, Design [Kante](https://github.com/shrippen/shrippen.github.io), Englisch/Deutsch,
live aktualisiert): Status und **Läuft gerade** (Titel und Interpret von Spotify, AirPlay, Bluetooth, Cast, Webradio),
**Webradio**, **Wecker** (Anstieg, Schlummern, Radio, Signalton oder jede Stream-Adresse, **Lichtwecker** am Ring, nicht an
Feiertagen, einmal aussetzen, Ausblenden) und **Timer**, ein **Schlummertimer**, **Klang** (Bass, Höhen, Loudness,
Nachtmodus), eine **Quellen-Regel** (die neueste Quelle spielt, die anderen pausieren) und **Lautstärkegrenzen** je Quelle,
**Durchsagen** über die Musik, **Tastenbelegung**, der **WLAN-Wächter** (misst die echte Verbindungsqualität zum Router und
wechselt bei anhaltend schlechter Qualität zu einem besseren Access Point desselben Netzes), **Home Assistant**
(MQTT-Erkennung: Lautstärke, Stumm, Webradio, Bluetooth-Kopplung, Timer, Schlummertimer, Wecker-Tasten, Durchsagen, der
Leuchtring als Licht, Läuft gerade, Temperatur, WLAN, Tasten-Ereignisse; auf Wunsch über TLS), der **Leuchtring**
(Visualizer: Spektrum, Pegel oder Puls; Lampe in jeder Farbe; Timer-Fortschritt), **Dienste an/aus**, **Sichern und
Wiederherstellen**, ein **Diagnosepaket** und **signierte Updates mit automatischem Rückfall**.

Außerdem in der Weboberfläche:
- ein **Morgen-Briefing**: Begrüßung, Wetter, Unwetterwarnungen, Pollenflug, Kalender (ICS, auch Müllabfuhr),
  Nachrichten wie die *tagesschau in 100 Sekunden*, Vorlagen aus Home Assistant; gesprochen über die Sprachausgabe von
  Home Assistant; auch als Weckton
- eine **Sendersuche** (radio-browser.info) mit Lieblingssendern auf Tasten (Doppel-/Dreifachdruck)
- eine **Raumkorrektur**: mit dem Handy messen, der Lautsprecher schlägt Filter gegen Dröhnen vor
- ein **Pegelausgleich** je Quelle, und Quellen **blenden über**, wenn eine andere übernimmt
- ein **Sprach-Satellit** für Home Assistant Assist (Wyoming, optional)
- eine Übersicht **anderer Leuchtfeuer** (Einstellungen kopieren, Updates anstoßen)
- **API-Schlüssel**, **SSH-Schlüssel**, **Prometheus-Metriken**, ein **Live-Protokoll** und die Weitergabe an **Syslog**

Die Weboberfläche schickt strenge Sicherheits-Header und prüft die Herkunft jeder Anfrage. Streams verbinden sich von
selbst neu; fällt der Sender eines Weckers aus, klingelt der eingebaute Ton. Ein optionaler **Hardware-Watchdog** startet
einen hängenden Lautsprecher neu.
Home-Assistant-Integration mit echtem Mediaplayer: [HOMEASSISTANT.md](HOMEASSISTANT.md).
API: [API.md](API.md).

Alles läuft über die eigene DSP-/Verstärkerkette des Lautsprechers. Das **Drehrad** regelt alle Quellen und bleibt
über Bluetooth in beide Richtungen mit der Handy-Lautstärke synchron.

Außerdem: SSH nur mit Schlüssel (eigenes dropbear), die offene Root-Shell auf Port 5555 (adb) ist zu, eine Firewall
lässt nur die nötigen Ports herein, und die Harman-Clouddienste (Cortana, OTA-Updates, Absturzberichte, totes
Harman-Spotify) sind abgeschaltet.

**Kein Ziel:** Cortana wiederherzustellen. Der optionale Sprach-Satellit reicht nur das Mikrofon an das eigene Home Assistant weiter.

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
git clone https://git.arianw.de/shrippen/leuchtfeuer.git && cd leuchtfeuer

# 1. ZUERST docs/INSTALL.de.md Teile 1-3 lesen: NAND sichern, StockRoot flashen, Lautsprecher ins WLAN bringen.
# 2. installieren: fragt und erklärt Schritt für Schritt (baut bei Bedarf auch alles, Docker, einmalig 20-60 Minuten)
./install.sh
# (ohne Rückfragen: ./install.sh --non-interactive --ip <ip> --key ~/.ssh/id_ed25519.pub)
# (ohne Bauen, kein Docker: ./install.sh --prebuilt   – Release-Paket von Gitea, ohne Tidal)
```

Danach das Handy mit „HK Invoke“ koppeln oder ihn in Spotify / der UPnP-App / Music Assistant auswählen.
Die vollständige Schritt-für-Schritt-Anleitung samt Hardware-Teil steht in **[docs/INSTALL.de.md](INSTALL.de.md)**
([English](INSTALL.md), [README in English](../README.md)).

## Konfiguration

`/data/invoke/config` auf dem Lautsprecher (Vorlage: `device/invoke/config.example`): Gerätename, Adresse des
Music-Assistant-Servers für Sendspin, DHCP-Hostname, welche Dienste laufen (`SERVICE_<NAME>="on|off"`, auch in der
Weboberfläche), Snapcast-Server, NTP-Server, HTTPS für die Weboberfläche und der öffentliche Schlüssel für signierte
Updates. Ein erneutes `./install.sh` aktualisiert den Lautsprecher und
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
- Release-Pakete (`install.sh --prebuilt`, Updates aus der Weboberfläche) enthalten keine Hersteller-Firmware und kein
  Tidal; Updates aus der Weboberfläche werden nur mit gültiger Ed25519-Signatur (`UPDATE_PUBKEY`) angenommen, ein
  fehlerhaftes Update wird zurückgenommen.
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
