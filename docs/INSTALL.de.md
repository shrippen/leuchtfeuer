# Installationsanleitung

Leuchtfeuer läuft auf jedem kleinen Rechner mit Lautsprecher: einem Raspberry Pi mit Sound-HAT oder USB-DAC, einem Mini-PC
am HiFi-Verstärker, einem alten Laptop. Diese Anleitung richtet es auf **jedem Linux mit systemd und ALSA** ein (Zielgerät
`generic`). [English](INSTALL.md)

Für den **Harman Kardon Invoke** gibt es eine eigene Anleitung, weil er vorher einen Hardware-Schritt braucht:
**[INVOKE.de.md](INVOKE.de.md)**.

## Was du brauchst

- **Das Gerät:** Linux mit systemd und ALSA (Raspberry Pi OS, Debian, Ubuntu, Fedora, Arch, …) auf `amd64`, `arm64` oder
  `armv7`, mit Lautsprecher oder Verstärker an einer Soundkarte (3,5 mm, HDMI, USB-DAC, I²S-HAT). Netzwerk, am besten per
  Kabel oder gutem WLAN.
- **Zum Bauen des Pakets:** `git`, Go ≥ 1.22 und ein C-Compiler für das Gerät (`cc` auf dem Gerät selbst oder ein
  Cross-Compiler: `aarch64-linux-gnu-gcc` für `arm64`, `arm-linux-gnueabihf-gcc` für `armv7`). Fertige Release-Pakete gibt
  es noch nicht.
- **Empfänger** kommen aus deiner Distribution (siehe Schritt 3); Leuchtfeuer bringt seine Weboberfläche, den Cast-Empfänger
  und den Bluetooth-Agenten selbst mit.

## 1. Paket bauen

Auf deinem Rechner oder direkt auf dem Gerät:

```sh
git clone https://git.arianw.de/shrippen/leuchtfeuer.git && cd leuchtfeuer
tools/build-generic.sh arm64                         # amd64, arm64 oder armv7; ohne Angabe: Architektur dieses Rechners
tools/make-release.sh --target generic --arch arm64  # -> dist/leuchtfeuer-<Version>-generic-arm64.tar.gz
```

## 2. Installieren

Das Paket aufs Gerät kopieren und daraus `setup.sh` starten:

```sh
mkdir lf && tar -xzf leuchtfeuer-*-generic-arm64.tar.gz -C lf
sudo sh lf/setup.sh --name Küche --password 'dein Web-Passwort'
```

`setup.sh` installiert nach `/opt/leuchtfeuer` (`--dir` ändert das), schreibt die Einstellungsdatei `config`
(Weboberfläche auf Port 8080, `--port` ändert das; Zeitzone vom System) und aktiviert `leuchtfeuer.service`. Ohne
`--password` erzeugt es ein Passwort und schreibt es nach `/opt/leuchtfeuer/log/leuchtfeuerd.log`.

## 3. Empfänger

Leuchtfeuer startet jeden Empfänger, dessen Programm installiert ist; die anderen erscheinen als „nicht installiert“. Unter
Debian und Raspberry Pi OS:

```sh
sudo apt install alsa-utils librespot shairport-sync gmediarender snapclient
```

| Empfänger | Programm | Hinweise |
|---|---|---|
| Spotify Connect | `librespot` | braucht Spotify Premium |
| AirPlay 1 | `shairport-sync` | |
| UPnP / DLNA | `gmediarender` (gmrender-resurrect) | |
| Snapcast | `snapclient` | anfangs aus; `SNAPCAST_SERVER` setzen |
| Sendspin (Music Assistant) | `sendspin-player` aus [sendspin-go](https://github.com/Sendspin/sendspin-go/releases) | nach `/opt/leuchtfeuer/bin/` legen |
| Cast (für Music Assistant, Home Assistant, VLC) | `castrecv`, im Paket enthalten | |
| Bluetooth A2DP | `bluetoothd` und bluez-alsa des Systems (`sudo apt install bluez bluez-alsa-utils`) | unter Einstellungen > Dienste einschalten; Leuchtfeuer ergänzt seinen Agenten |

Tidal Connect gibt es nur auf dem Invoke.

## 4. Erster Start

`http://<gerät>:8080/` öffnen und anmelden. Eine frische Installation beginnt mit dem **Einrichtungsassistenten**: Name,
Zeitzone und Feiertage, Ort für das Wetter, ein paar Webradio-Sender und ob du Home Assistant nutzt. Danach erscheint das
Gerät unter seinem Namen in Spotify, AirPlay, UPnP-Apps und Music Assistant.

## Einstellungen

Fast alles lässt sich in der Weboberfläche einstellen. Die Datei `/opt/leuchtfeuer/config` enthält, was schon vor der
Weboberfläche gebraucht wird:

| Einstellung | Bedeutung |
|---|---|
| `ALSA_CARD` | Soundkarte für Ausgabe und Lautstärke (Nummer oder Name aus `aplay -l`) |
| `ALSA_OUTPUT` | `"pipewire"` oder `"pulse"`, wenn ein Sound-Server die Karte belegt (Desktop-Systeme) |
| `WEB_PORT`, `WEB_TLS` | Port der Weboberfläche, HTTPS mit eigenem Zertifikat |
| `WIFI_IFACE` | Netzwerk-Schnittstelle (leer = die der Standardroute) |
| `SERVICE_<NAME>` | welche Empfänger laufen (auch unter Einstellungen > Dienste) |
| `SNAPCAST_SERVER`, `SENDSPIN_SERVER` | Server, wenn sie nicht von selbst gefunden werden |
| `FIREWALL` | `on` baut eine iptables-Kette für die eingeschalteten Dienste (anfangs aus) |
| `UPDATE_PUBKEY` | Signaturschlüssel der Releases für Updates aus der Weboberfläche |

Nach einer Änderung an der Datei: `sudo systemctl restart leuchtfeuer`.

Was die Weboberfläche alles kann, steht in der [README](README.de.md); die HTTP-API in [API.md](API.md) (englisch), die
Home-Assistant-Integration in [HOMEASSISTANT.md](HOMEASSISTANT.md).

## Aktualisieren und entfernen

- **Aktualisieren:** ein neueres Paket bauen und dessen `setup.sh` erneut ausführen; Einstellungen, Schlüssel und
  Kopplungen bleiben. Mit einem Signaturschlüssel (`UPDATE_PUBKEY`) installiert Einstellungen > Sichern & Update signierte
  Pakete und nimmt ein fehlerhaftes Update von selbst zurück.
- **Entfernen:** `sudo sh /opt/leuchtfeuer/setup.sh --uninstall` (Einstellungen bleiben), `--purge` löscht auch die.

## Fehlersuche

| Anzeichen | Prüfen |
|---|---|
| Weboberfläche geht nicht auf | `systemctl status leuchtfeuer`; ist der Port (`WEB_PORT`) belegt oder von einer Firewall gesperrt? |
| Ein Empfänger ist „nicht installiert“ | sein Programm fehlt (Tabelle in Schritt 3); installieren, er startet binnen 30 s |
| Kein Ton | `aplay -l` zeigt die Karte; steht `ALSA_CARD` darauf? Belegt ein Sound-Server die Karte: `ALSA_OUTPUT="pipewire"` |
| Protokolle | `/opt/leuchtfeuer/log/` und `/opt/leuchtfeuer/hook.log`, oder Einstellungen > Dienste > Protokoll, oder `journalctl -u leuchtfeuer` |
| In Apps nicht zu finden | mDNS zwischen WLAN und LAN (siehe [INVOKE.de.md](INVOKE.de.md#erkennung-zwischen-wlan--lan), gilt für jedes Gerät) |

## Ein Gerät mit eigener Hardware

Tasten, ein Leuchtring oder Hersteller-Software sind **Erweiterungen** eines Zielgeräts. Wie man ein Gerät ergänzt
(Treiber in leuchtfeuerd, Hook, Ton und Paket): [TARGETS.md](TARGETS.md) (englisch).
