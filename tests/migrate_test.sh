#!/bin/sh
# Prüft device/leuchtfeuer/migrate.sh: Umzug einer älteren Installation (/data/invoke, invoked, /run/invoke-*) nach
# /data/leuchtfeuer, auch zweimal hintereinander und mit schon vorhandenem neuen Verzeichnis.
#   sh tests/migrate_test.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'kill $H $S1 $DB 2>/dev/null; rm -rf "$T"' EXIT
fail=0
check(){ if eval "$2"; then echo "ok    $1"; else echo "FEHLER $1"; fail=1; fi; }
export LEUCHTFEUER_OLD_DIR=$T/data/invoke LEUCHTFEUER_DIR=$T/data/leuchtfeuer LEUCHTFEUER_RUN=$T/run LEUCHTFEUER_DNSMASQ=$T/data/dnsmasq.conf
mkdir -p $T/data/invoke/services $T/data/invoke/bin $T/data/invoke/lib/ladspa $T/data/invoke/bluez/var $T/run
echo 'DEVICE_NAME="Küche"' > $T/data/invoke/config
echo '{"timezone":"Europe/Berlin"}' > $T/data/invoke/invoked.json
echo key > $T/data/invoke/bluez/var/pairing
for f in services/invoked.sh bin/invoked lib/ladspa/invoke-eq.so; do echo alt > $T/data/invoke/$f; done
printf 'port=0\n# --- Leuchtfeuer: Autostart-Haken (%s/boot.sh) ---\ndhcp-script=%s/boot.sh\nleasefile-ro\n' "$T/data/invoke" "$T/data/invoke" > $T/data/dnsmasq.conf
sleep 30 & H=$!; echo $H > $T/run/invoke-hook.pid
sleep 30 & S1=$!; echo $S1 > $T/run/invoke-svc-invoked.pid
sleep 30 & DB=$!; echo $DB > $T/run/invoke-dropbear.pid
echo "1 0 0 0" > $T/run/invoke-svc-invoked.state

sh "$ROOT/device/leuchtfeuer/migrate.sh"
sleep 0.2
check "Verzeichnis umgezogen, alter Pfad verweist" '[ -d $T/data/leuchtfeuer ] && [ -L $T/data/invoke ] && [ "$(cat $T/data/invoke/config)" = "DEVICE_NAME=\"Küche\"" ]'
check "verschoben, nicht kopiert (laufender Hook, Protokoll zieht mit)" '[ ! -e $T/data/invoke.alt ] && grep -q "alten Hook" $T/data/leuchtfeuer/hook.log'
check "Einstellungen umbenannt" '[ -f $T/data/leuchtfeuer/leuchtfeuerd.json ] && [ ! -e $T/data/leuchtfeuer/invoked.json ]'
check "alte Programme entfernt, Kopplungen bleiben" '[ ! -e $T/data/leuchtfeuer/bin/invoked ] && [ ! -e $T/data/leuchtfeuer/services/invoked.sh ] && [ -f $T/data/leuchtfeuer/bluez/var/pairing ]'
check "alter Hook und alte Dienste beendet" '! kill -0 $H 2>/dev/null && ! kill -0 $S1 2>/dev/null'
check "dropbear läuft weiter, PID-Datei neu benannt" 'kill -0 $DB && [ "$(cat $T/run/leuchtfeuer-dropbear.pid)" = $DB ]'
check "keine alten Laufzeitdateien" '[ -z "$(ls $T/run | grep invoke-)" ]'
check "Autostart-Pfad angepasst" 'grep -q "dhcp-script=$T/data/leuchtfeuer/boot.sh" $T/data/dnsmasq.conf && ! grep -q "data/invoke/boot.sh" $T/data/dnsmasq.conf && grep -q "^port=0" $T/data/dnsmasq.conf'
before=$(ls -R $T/data | md5sum)
sh "$ROOT/device/leuchtfeuer/migrate.sh"
check "zweiter Lauf ändert nichts" '[ "$(ls -R $T/data | md5sum)" = "$before" ]'

# beide Verzeichnisse vorhanden (Installer hat schon kopiert): Fehlendes übernehmen, altes beiseite
rm $T/data/invoke; mkdir -p $T/data/invoke; echo alt > $T/data/invoke/config; echo k > $T/data/invoke/authorized_keys
mkdir -p $T/data/invoke/tidal/lib; echo so > $T/data/invoke/tidal/lib/libx.so.1.2; ln -s libx.so.1.2 $T/data/invoke/tidal/lib/libx.so.1
sh "$ROOT/device/leuchtfeuer/migrate.sh"
check "Fehlendes übernommen, Vorhandenes bleibt" '[ "$(cat $T/data/leuchtfeuer/authorized_keys)" = k ] && [ "$(cat $T/data/leuchtfeuer/config)" = "DEVICE_NAME=\"Küche\"" ] && [ -d $T/data/invoke.alt ] && [ -L $T/data/invoke ]'
check "Symlinks übernommen" '[ -L $T/data/leuchtfeuer/tidal/lib/libx.so.1 ] && [ "$(cat $T/data/leuchtfeuer/tidal/lib/libx.so.1)" = so ]'

# frische Installation: nichts zu tun
rm -rf $T/data $T/run; mkdir -p $T/data/leuchtfeuer $T/run
sh "$ROOT/device/leuchtfeuer/migrate.sh"
check "ohne alte Installation nichts" '[ ! -e $T/data/invoke ] && [ -z "$(ls $T/run)" ]'
exit $fail
