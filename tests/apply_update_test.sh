#!/bin/sh
# Prüft device/leuchtfeuer/apply-update.sh: Sichern, Ersetzen (Bind-Mount-Dateien an Ort und Stelle), Entfernen, Rückfall.
#   sh tests/apply_update_test.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export LEUCHTFEUER_DIR=$T/d LEUCHTFEUER_RUN=$T/r
mkdir -p $T/d/bin $T/d/services $T/r $T/s/bin
fail=0
check(){ if eval "$2"; then echo "ok    $1"; else echo "FEHLER $1"; fail=1; fi; }
echo alt > $T/d/bin/a; echo bleibt > $T/d/bin/k; echo weg > $T/d/services/volume-sync.sh
echo v1 > $T/d/VERSION; echo pod1 > $T/d/podium.conf; : > $T/d/hook.log
echo neu > $T/s/bin/a; echo frisch > $T/s/bin/n; echo pod2 > $T/s/podium.conf; echo v2 > $T/s/VERSION
echo services/volume-sync.sh > $T/s/.remove
ino=$(ls -i $T/d/podium.conf | awk '{print $1}')
sh "$ROOT/device/leuchtfeuer/apply-update.sh" apply $T/s norestart >/dev/null
check "neue Datei ersetzt" '[ "$(cat $T/d/bin/a)" = neu ] && [ "$(cat $T/d/bin/n)" = frisch ]'
check "Version" '[ "$(cat $T/d/VERSION)" = v2 ]'
check "podium.conf an Ort und Stelle (gleiche Inode)" '[ "$(cat $T/d/podium.conf)" = pod2 ] && [ "$(ls -i $T/d/podium.conf | awk "{print \$1}")" = "$ino" ]'
check "entfallene Datei entfernt" '[ ! -e $T/d/services/volume-sync.sh ]'
check "update-pending gesetzt" '[ -f $T/d/update-pending ]'
check "Stufe aufgeräumt" '[ ! -e $T/s ]'
# Rückfall ohne Neustart der Dienste prüfen
sed 's/rollback "${2:-}"; restart_all/rollback "${2:-}"/' "$ROOT/device/leuchtfeuer/apply-update.sh" > $T/au.sh
sh $T/au.sh rollback Test >/dev/null
check "alter Stand zurück" '[ "$(cat $T/d/bin/a)" = alt ] && [ "$(cat $T/d/VERSION)" = v1 ] && [ "$(cat $T/d/podium.conf)" = pod1 ]'
check "neue Datei wieder weg, entfernte wieder da" '[ ! -e $T/d/bin/n ] && [ "$(cat $T/d/services/volume-sync.sh)" = weg ]'
check "Rückfall vermerkt" 'grep -q Test $T/d/update-rolledback && [ ! -e $T/d/update-pending ]'
exit $fail
