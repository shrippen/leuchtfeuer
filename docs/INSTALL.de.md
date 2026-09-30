# Installationsanleitung

Der vollständige Weg vom Werkszustand eines Invoke bis zum Zustand aus der [README](../README.de.md). Die
Hardware-Teile (1-3) wurden einmal an einem Gerät durchgeführt; der Software-Teil (4-5) ist mit `build.sh` und
`install.sh` automatisiert.

> **Teststand.** Der Update-Weg von `install.sh` (SSH, Übertragung per Prüfsummenvergleich, Neustart,
> `verify-install.sh`) wurde gegen das fertige Gerät ausgeführt und hat den Zustand exakt reproduziert. Der Weg der
> **Erstinstallation über adb** (Teil 5, „frisches StockRoot-Gerät“) ist umgesetzt und durchgesehen, wurde aber **noch nicht
> an einem fabrikfrischen Gerät ausgeführt**; das gilt auch für `wifi-setup.sh` und `uninstall.sh` (die Formularfelder der
> WLAN-Einrichtung stammen aus der Hersteller-Bibliothek). Probleme bitte als Issue melden. **Alles einmal lesen, bevor
> es losgeht.**

> ⚠️ Teile 1-3 schreiben in den NAND-Flash des Lautsprechers. Ein Fehler kann ihn unbrauchbar machen. Teil 1
> (Sicherung) ist Pflicht. Den U-Boot-Befehl `l2nand 83` **niemals ohne `-m`** ausführen: Er löscht den ganzen Flash
> inklusive `factory_setting` (Zertifikate, MAC, Kalibrierung des Geräts), die in keinem Firmware-Abbild steht.

## 0. Vorbereitung (PC)

Linux-PC. Nötig: `adb`, `docker`, `go` (≥ 1.22), `curl`, `git`, `unzip`, `python3`, `socat`, `telnet`, `gh`
(GitHub-CLI, nur für `scripts/fetch.sh`; alternativ die drei Dateien von Hand laden, siehe unten).

```sh
git clone <dieses Repo> invoke-hack && cd invoke-hack
./scripts/fetch.sh                      # lädt Hersteller-Flashwerkzeug, das geroottete Abbild „StockRoot“ und das OTA aus den
                                        # Releases von github.com/coggy9/HKHacking nach firmware/ (nicht im Git)
sudo cp tools/99-invoke-marvell.rules /etc/udev/rules.d/ && sudo udevadm control --reload && sudo udevadm trigger
```

Ohne `gh`: `Harman.Kardon.INVOKE.Flashing.zip` (Release *HarmanFlash*), `83_IMAGE` (Release *StockRoot*) und
`Harman.Kardon.INVOKE.Driver.OTA2.zip` (Release *FinalOTA*) nach `firmware/` legen, dann
`unzip firmware/Harman.Kardon.INVOKE.Flashing.zip -d firmware/extracted/flashing`. Das StockRoot-Abbild muss die
SHA-256 `f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7` haben.

### Flashing-Modus (für Teil 1 und 2)

Im Normalbetrieb meldet sich der Invoke nicht am USB. So kommt man ins Marvell-Boot-ROM (USB-ID `1286:8174`):

1. Strom abziehen, den **Service-USB-Port** mit dem PC verbinden.
2. Das **Reset-Loch** (Büroklammer) gedrückt halten, Strom anstecken, weiter halten.
3. Innerhalb von 5 s **genau 4× die Mic-aus-Taste** drücken. Der Leuchtring wird gelb.
4. Reset loslassen, sobald Konsole/U-Boot in der Skriptausgabe erscheint.

Das kann mehrere Anläufe brauchen (das Boot-ROM setzt sich manchmal 2-3× zurück, bevor die Kette durchläuft).

## 1. NAND sichern (Pflicht)

```sh
tools/usb-ramboot.sh backup         # starten, dann den Flashing-Modus auslösen; bootet Kernel + Ramdisk nur im RAM,
                                    # alle Partitionen read-only, nichts eingehängt, NAND unberührt
scripts/nand-backup.sh              # (zweites Terminal) liest jede Partition zweimal per adb, vergleicht Prüfsummen,
                                    # danach den ganzen Chip roh inkl. OOB -> backup/<zeit>/
python3 scripts/verify-backup.py backup/<zeit>
```

`backup/<zeit>/` **sicher und an zwei Orten** aufbewahren. Es enthält die Geräteschlüssel (`factory_setting`) und
eventuelle WLAN-Zugangsdaten in `app`; nie veröffentlichen. Eine beschädigte `factory_setting` lässt sich nur aus
dieser Sicherung wiederherstellen.

## 2. Geroottetes Abbild flashen

```sh
tools/usb-flash.sh                  # starten; dann Flashing-Modus auslösen. Lädt U-Boot in den RAM, Konsole in recon/
tools/uboot-send.sh "l2nand -m 83"  # flasht firmware/83_IMAGE (StockRoot 11.1842). -m = nur die beschriebenen Blöcke
                                    # löschen. (uboot-send.sh verweigert l2nand ohne -m)
```

Im Log auf `Congratulations! u2nand succeed!` warten. Empfohlene Prüfung, dass `factory_setting`, die B-Partitionen
und die Bad-Block-Tabelle noch mit der Sicherung übereinstimmen:

```sh
tools/uboot-send.sh ramdisk         # bootet wieder die read-only-Ramdisk, ohne Neustart dazwischen
scripts/verify-after-flash.sh backup/<zeit>
```

Lautsprecher aus- und einstecken. StockRoot ist die Hersteller-Version 11.1842 plus Root-Login, adbd an (Port 5555,
ohne Anmeldung) und gesperrte OTA-Server in `/etc/hosts`.

## 3. Lautsprecher ins WLAN bringen

Nach dem Flashen öffnet der Lautsprecher einen unverschlüsselten Zugangspunkt `HK Invoke_XXXXXX` (Adresse
192.168.43.1). PC damit verbinden, dann:

```sh
scripts/wifi-setup.sh "<deine SSID>" "<deine WLAN-Passphrase>"    # WPA2-PSK, 2,4 GHz
```

Die Antwort `continue` heißt: Er verbindet sich mit deinem Netz. PC wieder ins Heimnetz, die IP des Lautsprechers im
Router nachsehen. Prüfen: `adb connect <ip>:5555 && adb shell id` muss `uid=0(root)` ausgeben.

> Der Lautsprecher reagiert empfindlich auf die WLAN-Qualität. Am Rand der Reichweite zeigen Pings Verlust und hohe
> Laufzeiten; näher stellen oder an einen guten Access Point binden (`wpa_cli` / Router). Alles Folgende läuft über das
> WLAN; eine schlechte Verbindung macht `install.sh` langsam, aber es setzt fort (nur geänderte Dateien werden übertragen).

## 4. Software bauen

```sh
./build.sh            # dropbear, librespot, gmrender, sendspin, castrecv, btagent, BlueZ+bluez-alsa, (Tidal-Bündel)
./build.sh --no-tidal # ohne das proprietäre Tidal-Connect-Bündel
```

Braucht Docker (Toolchain-Images entstehen beim ersten Mal) und Go. Beim ersten Mal etwa 20-60 Minuten; fertige Teile
werden übersprungen (`--force` baut neu). Ergebnisse in `build/`.

## 5. Installieren

1. Konfigurationsvorlage kopieren und anpassen (optional, es gibt Standardwerte):

   ```sh
   cp device/invoke/config.example config
   $EDITOR config     # DEVICE_NAME, SENDSPIN_SERVER („host:8927“ deines Music Assistant), DHCP_HOSTNAME
   ```

2. Installer mit deinem **öffentlichen** SSH-Schlüssel starten (der private muss im `ssh-agent` oder daneben liegen):

   ```sh
   ./install.sh --ip <ip-des-lautsprechers> --key ~/.ssh/id_ed25519.pub --config config
   ```

   Bei einem frischen StockRoot-Gerät: verbindet sich per adb (Port 5555), prüft Root, Firmware (StockRoot 11.1842) und
   Platz auf `/data`, richtet das eigene dropbear mit gerätespezifischem Host-Schlüssel und deinem Schlüssel ein,
   hängt den Autostart-Haken an `/data/dnsmasq.conf` (das Original wird vorher als `dnsmasq.conf.orig` gesichert), wartet
   auf SSH (und prüft dabei den selbst erzeugten Host-Schlüssel), kopiert alle Dateien per SSH, schließt adb, startet
   neu und führt zuletzt `scripts/verify-install.sh` aus.

   Nützliche Optionen: `--dry-run`, `--no-tidal`, `--no-reboot`, `--yes`. Späteres erneutes Ausführen aktualisiert den
   Lautsprecher; nur geänderte Dateien werden übertragen, deine `config` auf dem Lautsprecher bleibt, außer man gibt
   `--config` an.

3. Nach dem Neustart ~2 Minuten warten; der Haken startet alle Dienste binnen etwa 90 s.

## 6. Benutzen

| Quelle | Wie |
|---|---|
| Bluetooth | „HK Invoke“ am Handy koppeln (ohne PIN); bleibt gekoppelt und verbindet sich selbst wieder |
| Spotify | „HK Invoke“ erscheint in der Geräteliste der Spotify-App (gleiches WLAN; Premium) |
| UPnP/DLNA | „HK Invoke“ in der UPnP-App als Renderer wählen; https://-Streams gehen (aktueller CA-Bestand) |
| Music Assistant | Der Player erscheint über Sendspin (`SENDSPIN_SERVER` setzen) und, wenn der Cast-Anbieter ihn findet, als Chromecast; sonst im Google-Cast-Anbieter unter „known hosts“ per IP eintragen, wenn mDNS nicht zwischen WLAN und LAN durchkommt |
| Tidal | „HK Invoke“ erscheint in der Tidal-Connect-Liste der Tidal-App |

Das Drehrad regelt alle Quellen. Logs liegen unter `/data/invoke/log/` auf dem Lautsprecher
(`ssh root@<ip> 'tail -f /data/invoke/log/*.log'`).

### Erkennung zwischen WLAN ↔ LAN

Viele Router (z. B. FRITZ!Box) leiten Multicast nicht zwischen WLAN und LAN weiter. Ein PC im LAN sieht den
Lautsprecher dann nicht per mDNS, obwohl Handys im WLAN ihn sehen. Auswege: die IP verwenden (Cast „known hosts“,
`SENDSPIN_SERVER`) oder den Client ins WLAN setzen.

## 7. Wartung

- **Aktualisieren:** `git pull && ./build.sh && ./install.sh --ip <ip> --key <pub>`.
- **Prüfen:** `scripts/verify-install.sh --ip <ip> --key <pub>`.
- **Notbremse:** `ssh root@<ip> 'touch /data/invoke/disable-hook'`, neu starten → Originalverhalten.
- **Deinstallieren:** `./uninstall.sh --ip <ip> --key <pub> [--purge]` (entfernt den Autostart-Haken, stellt
  `dnsmasq.conf` wieder her, startet neu; `--purge` löscht auch `/data/invoke`).
- **Werkszustand:** Die Hersteller-Firmware lässt sich mit dem Hersteller-Werkzeug erneut flashen (`l2nand -m 83` mit
  dem Hersteller-Abbild); eine beschädigte `factory_setting` aus der Sicherung wiederherstellen.

## 8. Fehlersuche

| Symptom | Prüfen |
|---|---|
| `adb shell id` zeigt nicht root | Kein StockRoot (Firmware 12.x hat adbd aus und Port 22 zu) – Teil 2 |
| `install.sh` langsam oder Zeitüberschreitung | WLAN-Verbindung (Ping-Verlust); Lautsprecher näher stellen, erneut starten – unveränderte Dateien werden übersprungen |
| SSH verweigert | `--key` muss der öffentliche Schlüssel zum privaten Schlüssel/Agenten sein; zu viele Agent-Schlüssel erschöpfen die 10 Versuche von dropbear → `-o IdentitiesOnly=yes` |
| Dienst fehlt | `ssh root@<ip> 'ps; tail /data/invoke/log/<dienst>.log'`; der Haken startet tote Dienste alle 30 s neu |
| Bluetooth nicht sichtbar | `scripts/verify-install.sh`; `/data/invoke/log/bluetooth-*.log`; `hciconfig hci0` muss `UP RUNNING PSCAN ISCAN` zeigen |
| Music Assistant meldet „Legacy-Modus“ bei Sendspin | Erwartet: sendspin-go 1.8.x spricht den unverschlüsselten Dialekt; wird akzeptiert, solange „Allow legacy clients“ an ist |
| Tidal-Anmeldung scheitert | Das iFi-Zertifikat wurde evtl. gesperrt; hier nicht behebbar |
