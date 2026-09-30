# Invoke Hack – aktueller Stand

Stand: 2026-09-30. Ausführliche Herleitung und alle Details: `NOTES.md`.

## Gerät

- Harman Kardon Invoke, jetzt mit **StockRoot 11.1842** (coggy9), geflasht mit
  `l2nand -m 83` (ohne Chip-Löschung). factory_setting, Bad-Block-Tabelle, B-Partitionen und
  fw_stat vor dem ersten Start gegen die Sicherung geprüft: unverändert.
- Rohsicherung aller Partitionen: `backup/20260930-000419/` (nur lokal, nicht im Git).
  Offen: zweite Kopie außerhalb dieses Rechners.
- Im WLAN „Friedelmine“ als **invoke.lan** (192.168.231.61, WLAN-MAC d8:f7:10:c1:29:06).

## Zugang

- `ssh invoke.lan` (Eintrag in `~/.ssh/config`, Schlüssel `~/.ssh/HKInvoke.pub` im
  Bitwarden-Agent, Host-Schlüssel in `~/.ssh/known_hosts`,
  SHA256:dBJeQTcwjPnksG1Y5n67XSjLIbkLl7Q1bktX4hJ4SnY).
- Nur Schlüssel-Login (eigenes dropbear 2026.94, Passwort-Login nicht einkompiliert).
  adb (Port 5555) ist aus, IPv6 aus, Firewall lässt nur SSH, mDNS, DHCP, ICMP und die
  Ports der Musikdienste herein. Update-Server über /etc/hosts gesperrt.
- Neustart auf dem Gerät: `/bin/reboot` (busybox-`reboot` tut nichts).
- Notbremse: `touch /data/invoke/disable-hook` und neu starten -> Original-Verhalten
  (dann ist allerdings adbd wieder offen und SSH nicht nutzbar).

## Was auf dem Gerät liegt (`/data/invoke`, Quellen in `device/invoke/`)

- `boot.sh` – Autostart, wird von dnsmasq aufgerufen (`/data/dnsmasq.conf`, Zeilen am Ende).
- `hook.sh` – Schleife alle 30 s: SSH, Firewall, adbd aus, IPv6 aus, gekürzte Harman-Dienste
  (`podium.conf`), DHCP-Name `invoke`, Dienste aus `services/` starten und überwachen.
- `services/` – `librespot.sh`, `gmrender.sh`, `sendspin.sh`, `tidal-1-dbus.sh`,
  `tidal-2-avahi.sh`, `tidal-3-connect.sh`. Logs in `/data/invoke/log/`.
- `bin/` (librespot, gmediarender, sendspin-player), `tidal/` (Tidal-Bundle), `ports.local`,
  `asound-music.conf`, Host-Schlüssel, `authorized_keys`.
- Gebaut werden die Programme mit den Skripten in `tools/` (Docker); Ergebnis in `build/`.

## Musikdienste (alle „HK Invoke“)

| Dienst | Programm | Stand |
|---|---|---|
| Spotify Connect | librespot 0.8.0 | läuft, im WLAN angekündigt; Test mit App offen |
| UPnP/DLNA | gmrender-resurrect | läuft, Testton per UPnP erfolgreich abgespielt |
| Sendspin | sendspin-go 1.8.2 | verbunden mit Music Assistant (ploetze.lan:8927); braucht beim Abspielen einen ganzen Kern – Fix (kein mmap) gebaut, noch nicht getestet/verteilt |
| Tidal Connect | iFi tidal_connect_application 1.1.3 | läuft, im WLAN angekündigt; Test mit App offen. Nutzt ein iFi-Zertifikat, nicht für dieses Gerät lizenziert, kann gesperrt werden |

Ausgabe aller Dienste: ALSA `music` (Lautstärkeregler „music“) -> dmix -> DSP, 48 kHz.

## Harman-Dienste

- Abgeschaltet: Cortana, Harman-Spotify (tot), OTA, Absturzbericht-Upload.
- Weiter aktiv: Tasten/LEDs, DSP/Verstärker, WLAN-Verwaltung, Tonquellen-Verwaltung,
  Bluetooth, Überhitzungsschutz (Abschaltung bei 95 °C; im Betrieb ~78 °C).

## Offene Punkte

1. **WLAN schlecht** seit ~01:30: Invoke am AVM-AP 98:9b:cb:ee:2b:0d, 80 % Ping-Verlust.
   Entscheidung offen: Strom aus/an, im Router binden, oder BSSID fest eintragen.
2. Apps testen: Spotify, Tidal, DLNA, Music Assistant.
3. Sendspin-Fix testen und verteilen.
4. Lautstärketasten des Geräts auf den Regler „music“ legen.
5. Bluetooth (Harman-Stack läuft; Logs und `/data/bluetooth` ansehen).
6. Optional: avahi-Hostname „invoke“ statt „hk-invoke“ (Router beansprucht den Namen per mDNS).
7. Zweite Kopie der NAND-Sicherung außerhalb dieses Rechners.
