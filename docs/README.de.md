<p align="center"><img src="icon.svg" alt="Leuchtfeuer" width="96" height="96"></p>

# Leuchtfeuer

_Macht aus einem kleinen Rechner mit Lautsprecher einen Smartspeaker_

Leuchtfeuer macht aus jedem kleinen Linux-Rechner mit Lautsprecher einen **Netzwerk- und Smartspeaker**: einen Raspberry Pi
mit Sound-HAT, einen Mini-PC am HiFi-Verstärker oder einen Smartspeaker, den der Hersteller aufgegeben hat, wie den
**Harman Kardon Invoke**. Der gemeinsame Teil (Empfänger, Weboberfläche, Wecker, Home Assistant) ist überall gleich; was ein
Gerät zusätzlich hat (Tasten, einen Leuchtring, einen eigenen Verstärker), kommt von seinem **Zielgerät**.
[English](../README.md)

| Zielgerät | Gerät | Anleitung |
|---|---|---|
| `generic` | jedes Linux mit systemd und ALSA: Raspberry Pi, Mini-PC, alter Laptop | [INSTALL.de.md](INSTALL.de.md) |
| `invoke` | Harman Kardon Invoke (der aufgegebene Cortana-Lautsprecher): Drehrad, Tasten, Leuchtring, Mikrofone | [INVOKE.de.md](INVOKE.de.md) |

Ein weiteres Gerät mit eigener Hardware: [TARGETS.md](TARGETS.md) (englisch).

## Empfänger

Nach der Installation erscheint das Gerät unter seinem Namen in:

| Was | Wie | Hinweise |
|---|---|---|
| **Spotify Connect** | [librespot](https://github.com/librespot-org/librespot) | braucht ein Spotify-Premium-Konto |
| **UPnP / DLNA-Renderer** | [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect) | z. B. mit Symfonium, BubbleUPnP, foobar2000 |
| **Sendspin** (Music Assistant) | [sendspin-go](https://github.com/Sendspin/sendspin-go) | „Legacy“-Dialekt (unverschlüsselt) |
| **AirPlay** (AirPlay 1) | [shairport-sync](https://github.com/mikebrady/shairport-sync) | iPhone, iPad, Mac; AirPlay 2 wird nicht unterstützt (Multiroom: Snapcast oder Sendspin) |
| **Google Cast** (Audio) | eigene Empfänger-Nachbildung (`src/castrecv`) | nur für Sender ohne Geräteprüfung durch Google: Music Assistant, Home Assistant, VLC, pychromecast. **Nicht** YouTube / Spotify-Cast / Chrome-Tab |
| **Snapcast** (optional) | [snapclient](https://github.com/badaix/snapcast) | Multiroom synchron mit anderen Snapcast-Lautsprechern; anfangs aus, braucht deinen snapserver |
| **Bluetooth-A2DP-Empfänger** | BlueZ + bluez-alsa + eigener Agent (`src/btagent`) | Kopplungsfenster aus der Weboberfläche, aus Home Assistant oder per Taste; merkt sich Kopplungen, Handy-Lautstärke bleibt synchron |
| **Tidal Connect** (optional, nur ARM) | proprietäres iFi-Programm | **nicht für dein Gerät lizenziert**, kann gesperrt werden – nur auf Wunsch, nie in einem Paket: Invoke siehe [INVOKE.de.md](INVOKE.de.md#risiken), sonst als Modul, siehe [INSTALL.de.md](INSTALL.de.md) |

Der Ton kann auch an eine andere Soundkarte (HDMI, USB) oder einen **Bluetooth-Lautsprecher** gehen (Einstellungen > Ausgabe).

## Weboberfläche

Port 80 auf dem Invoke, sonst 8080; optional HTTPS, Design [Kante](https://github.com/shrippen/Kante), Englisch/Deutsch, live
aktualisiert. Beim ersten Start fragt ein **Einrichtungsassistent** nach Name, Zeitzone, Ort, Sendern und Home Assistant.

- **Läuft gerade** (Titel und Interpret von Spotify, AirPlay, Bluetooth, Cast, Webradio) und Lautstärke
- **Webradio** mit **Sendersuche** (radio-browser.info) und Lieblingssendern auf Tasten
- **Wecker** (Anstieg, Schlummern, Radio, Signalton oder jede Stream-Adresse, nicht an Feiertagen, einmal aussetzen,
  Ausblenden), **Timer** und ein **Schlummertimer**
- ein **Morgen-Briefing**: Begrüßung, Wetter, Unwetterwarnungen, Pollenflug, Kalender (ICS, auch Müllabfuhr), Nachrichten
  wie die *tagesschau in 100 Sekunden*, Vorlagen aus Home Assistant; gesprochen über die Sprachausgabe von Home Assistant;
  auch als Weckton
- **Klang** (Bass, Höhen, Loudness, Nachtmodus, wirkt sofort) und eine **Raumkorrektur**, mit dem Handy gemessen
- eine **Quellen-Regel** (die neueste Quelle spielt, die anderen blenden aus und pausieren), **Lautstärkegrenzen** und ein
  **Pegelausgleich** je Quelle
- **Durchsagen** über die Musik (Sprachausgabe, Türklingel)
- **Home Assistant**: MQTT-Erkennung (Lautstärke, Stumm, Webradio, Bluetooth-Kopplung, Timer, Wecker-Tasten, Durchsagen,
  Läuft gerade, Sensoren), eine [eigene Integration](HOMEASSISTANT.md) mit echtem Mediaplayer und ein **Sprach-Satellit**
  für Assist (Wyoming, optional, mit Mikrofon)
- der **WLAN-Wächter** (misst die echte Verbindungsqualität und wechselt zu einem besseren Access Point desselben Netzes)
- **Dienste an/aus**, **Sichern und Wiederherstellen**, ein **Diagnosepaket**, **signierte Updates mit automatischem Rückfall**
- eine Übersicht **anderer Leuchtfeuer** (Einstellungen kopieren, Updates anstoßen)
- **API-Schlüssel**, **SSH-Schlüssel**, **Prometheus-Metriken**, ein **Live-Protokoll**, Weitergabe an **Syslog**
- **Klänge**: die Töne von Leuchtfeuer durch eigene Dateien ersetzen

Auf Geräten mit diesen Erweiterungen außerdem: **Tastenbelegung** (jede Taste, per Drücken angelernt), der **Leuchtring**
(Visualizer, Lampe, Lichtwecker, Timer-Fortschritt, als Licht in Home Assistant) und die **eigenen Klänge** des Geräts.

Die Weboberfläche schickt strenge Sicherheits-Header und prüft die Herkunft jeder Anfrage. Streams verbinden sich von
selbst neu; fällt der Sender eines Weckers aus, klingelt der eingebaute Ton. Ein optionaler **Hardware-Watchdog** startet
ein hängendes Gerät neu. API: [API.md](API.md) (englisch).

**Kein Ziel:** ein Cloud-Sprachassistent. Der optionale Sprach-Satellit reicht nur das Mikrofon an das eigene Home Assistant
weiter.

## Funktionsweise (kurz)

Ein kleiner Überwacher (`hook.sh`, gestartet von systemd oder vom Startvorgang des Geräts) startet und beobachtet die
Empfänger, richtet Firewall und SSH ein, wo das Zielgerät es will, und stellt die Uhr. **leuchtfeuerd** liefert
Weboberfläche und API und kümmert sich um Wecker, Timer, Briefing, Home Assistant und Lautstärke. Alle Quellen spielen in
**eine ALSA-Tonkette** mit Reglern je Quelle, dem Klang-Plugin und dem Visualizer-Abgriff, die am Ausgang des Geräts endet.
Einzelheiten: [ARCHITECTURE.md](ARCHITECTURE.md) (englisch).

## Schnellstart

**Jedes Linux** (Raspberry Pi, Mini-PC), siehe [INSTALL.de.md](INSTALL.de.md):

```sh
git clone https://git.arianw.de/shrippen/leuchtfeuer.git && cd leuchtfeuer
tools/build-generic.sh arm64 && tools/make-release.sh --target generic --arch arm64
# auf dem Gerät, mit dem Paket aus dist/:
mkdir lf && tar -xzf leuchtfeuer-*-generic-arm64.tar.gz -C lf && sudo sh lf/setup.sh --name Küche
sudo apt install alsa-utils librespot shairport-sync gmediarender     # die gewünschten Empfänger
```

Danach `http://<gerät>:8080/` öffnen.

**Harman Kardon Invoke:** zuerst den Lautsprecher sichern und flashen, dann `./install.sh` (fragt und erklärt jeden
Schritt). Alles steht in **[INVOKE.de.md](INVOKE.de.md)**.

## Entwicklung

- Tests ohne Gerät: `tests/run.sh` (Go mit Race-Detector, Shell, Klang-Plugin, Weboberfläche, Home-Assistant-Integration;
  läuft auch in der CI). Auf einem Gerät: `scripts/smoke.sh`.
- Demo-Modus für Screenshots (ohne Gerät, Demodaten „Studio Weber“): `demo/start.sh`.
- Releases: ein Paket je Zielgerät, signiert (`tools/make-release.sh`, `.github/workflows/release.yml`, auf GitHub).

## Rechtliches

- Die Empfänger (librespot, gmrender-resurrect, sendspin-go, shairport-sync, snapclient, BlueZ, bluez-alsa, …) behalten
  ihre eigenen Lizenzen; auf dem Invoke holt `build.sh` die genauen Quellen per Tag oder Commit, sonst kommen sie aus
  deiner Distribution.
- Der Cast-Empfänger ist eine **eigenständige Nachbildung** des Cast-Protokolls, keine Google-Software.
- Sendspin verbindet sich derzeit im Legacy-Dialekt (unverschlüsselt); Music Assistant zeigt dazu einen Hinweis.
- Release-Pakete enthalten keine Hersteller-Firmware und kein Tidal; Updates aus der Weboberfläche werden nur mit gültiger
  Ed25519-Signatur (`UPDATE_PUBKEY`) angenommen, ein fehlerhaftes Update wird zurückgenommen.
- Invoke: Flashen kann den Lautsprecher unbrauchbar machen, und Tidal Connect ist für ihn nicht lizenziert – siehe
  [INVOKE.de.md](INVOKE.de.md#risiken).

## Lizenz

MIT, siehe [LICENSE](../LICENSE). Das gilt für Code und Dokumentation dieses Repositories; die gebauten und installierten
Programme und eine Hersteller-Firmware behalten ihre eigenen Lizenzen und Bedingungen.

## Danksagung

[coggy9/HKHacking](https://github.com/coggy9/HKHacking) (StockRoot-Abbild und Flash-Verfahren für den Invoke),
[librespot](https://github.com/librespot-org/librespot), [gmrender-resurrect](https://github.com/hzeller/gmrender-resurrect),
[Sendspin](https://github.com/Sendspin/sendspin-go), [shairport-sync](https://github.com/mikebrady/shairport-sync),
[Snapcast](https://github.com/badaix/snapcast), [BlueZ](https://www.bluez.org/), [bluez-alsa](https://github.com/arkq/bluez-alsa),
[dropbear](https://matt.ucc.asn.au/dropbear/dropbear.html), [TonyTromp/tidal-connect-docker](https://github.com/TonyTromp/tidal-connect-docker).
