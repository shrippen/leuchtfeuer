# Changelog

## [1.0.2] — 2026-10-10

### English

The first published release of Leuchtfeuer 1.0: any small computer with a speaker, or the abandoned Harman Kardon Invoke, becomes a smart speaker without a cloud. Packages are signed and come from GitHub.

#### New
- Receivers: Spotify Connect, AirPlay, UPnP/DLNA, Cast, Sendspin (Music Assistant), Snapcast multiroom and Bluetooth, through one audio chain into the built-in sound card, HDMI, USB or a Bluetooth speaker.
- At home: a morning briefing with weather, appointments and news, alarms with a backup tone, web radio with station search, room correction measured with your phone, and a setup assistant for the first steps.
- Home Assistant: the speaker registers itself over MQTT, and a custom integration adds a media player with volume, radio, timers, alarms, now playing and announcements over the music.
- Any Linux with systemd and ALSA (Raspberry Pi, mini PC) from one package and `setup.sh`; the Harman Kardon Invoke with its knob, buttons, light ring and microphones.
- A password-protected web interface, a firewall that opens only switched-on services, API keys, Prometheus metrics and a hardware watchdog.
- Signed packages: every package has an Ed25519 signature (`.sig`) next to its checksum (`.sha256`). Updates from the web interface and `install.sh --prebuilt` check it against the key in `docs/release-key.pub` and refuse foreign or altered packages; a failed update rolls back by itself.

#### Improved
- Packages come from GitHub: the release files (`invoke`, `generic-amd64`, `generic-arm64`, `generic-armv7`) are attached to the GitHub releases (https://github.com/shrippen/leuchtfeuer/releases), and `install.sh --prebuilt` and the update from the web interface load them from there.

#### Note
- The tags `v1.0.0` and `v1.0.1` have no release; their builds failed on the runner back then. 1.0.2 contains their state.
- On the Harman Kardon Invoke the installation flashes the speaker, which can brick it: back up the NAND first, as its guide describes.

### Deutsch

Das erste veröffentlichte Release von Leuchtfeuer 1.0: Jeder kleine Rechner mit Lautsprecher, oder der aufgegebene Harman Kardon Invoke, wird ein Smartspeaker ohne Cloud. Die Pakete sind signiert und kommen von GitHub.

#### Neu
- Empfänger: Spotify Connect, AirPlay, UPnP/DLNA, Cast, Sendspin (Music Assistant), Snapcast-Multiroom und Bluetooth, über eine Audiokette in die eingebaute Soundkarte, HDMI, USB oder einen Bluetooth-Lautsprecher.
- Zu Hause: ein Morgen-Briefing mit Wetter, Terminen und Nachrichten, Wecker mit Ersatzton, Webradio mit Sendersuche, eine mit dem Handy gemessene Raumkorrektur und ein Einrichtungsassistent für die ersten Schritte.
- Home Assistant: Der Lautsprecher meldet sich per MQTT an, und eine eigene Integration bringt einen Mediaplayer mit Lautstärke, Radio, Timern, Weckern, aktuellem Titel und Durchsagen über der Musik.
- Jedes Linux mit systemd und ALSA (Raspberry Pi, Mini-PC) aus einem Paket und `setup.sh`; der Harman Kardon Invoke mit Drehknopf, Tasten, Lichtring und Mikrofonen.
- Eine passwortgeschützte Weboberfläche, eine Firewall, die nur eingeschaltete Dienste öffnet, API-Schlüssel, Prometheus-Metriken und ein Hardware-Watchdog.
- Signierte Pakete: Jedes Paket hat eine Ed25519-Signatur (`.sig`) neben der Prüfsumme (`.sha256`). Updates aus der Weboberfläche und `install.sh --prebuilt` prüfen sie gegen den Schlüssel in `docs/release-key.pub` und lehnen fremde oder veränderte Pakete ab; ein gescheitertes Update rollt sich selbst zurück.

#### Verbessert
- Pakete kommen von GitHub: Die Release-Dateien (`invoke`, `generic-amd64`, `generic-arm64`, `generic-armv7`) hängen an den GitHub-Releases (https://github.com/shrippen/leuchtfeuer/releases), und `install.sh --prebuilt` und das Update aus der Weboberfläche laden sie von dort.

#### Hinweis
- Die Tags `v1.0.0` und `v1.0.1` haben kein Release; ihre Builds sind damals im Runner gescheitert. 1.0.2 enthält deren Stand.
- Auf dem Harman Kardon Invoke flasht die Installation den Lautsprecher, was ihn unbrauchbar machen kann: zuerst den NAND sichern, wie es seine Anleitung beschreibt.
