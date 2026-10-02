#!/bin/sh
# Prüft Module im Zielgerät generic (targets/generic/setup.sh --module / --remove-module / --uninstall): Dateien landen in
# der Installation, das Modul merkt sich seine Dateien, Entfernen löscht genau diese (und leere Modulordner), andere
# Dateien bleiben. Ohne root und systemd (id und systemctl sind Attrappen).
#   sh tests/module_test.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
fail=0
check(){ if eval "$2"; then echo "ok    $1"; else echo "FEHLER $1"; fail=1; fi; }
mkdir -p "$T/bin" "$T/d/bin" "$T/d/services" "$T/d/log" "$T/m/bin" "$T/m/services" "$T/m/tidal/lib"
printf '#!/bin/sh\n[ "$1" = -u ] && echo 0 || command id "$@"\n' > "$T/bin/id"
printf '#!/bin/sh\necho "$@" >> %s/systemctl.log\n[ "$1" = is-active ] && exit 1\nexit 0\n' "$T" > "$T/bin/systemctl"
chmod 755 "$T/bin/id" "$T/bin/systemctl"
export PATH="$T/bin:$PATH"
: > "$T/d/hook.sh"; echo eigen > "$T/d/bin/leuchtfeuerd"; echo 'DEVICE_NAME="X"' > "$T/d/config"
# ein Modul wie das von Tidal, aber für jede Architektur
printf 'name=probe\nversion=1\narch=any\ncheck=bin/probe --check\n' > "$T/m/MODULE"
printf '#!/bin/sh\necho "probe-check $LEUCHTFEUER_DIR"\n' > "$T/m/bin/probe"; chmod 755 "$T/m/bin/probe"
echo dienst > "$T/m/services/probe.sh"; echo lib > "$T/m/tidal/lib/libx.so"
tar -C "$T/m" -czf "$T/probe.tar.gz" .
S=$ROOT/targets/generic/setup.sh
out=$(sh "$S" --dir "$T/d" --module "$T/probe.tar.gz" 2>&1)
check "Modul installiert" '[ -x "$T/d/bin/probe" ] && [ -f "$T/d/services/probe.sh" ] && [ -f "$T/d/tidal/lib/libx.so" ] && [ ! -e "$T/d/MODULE" ]'
check "Prüfung des Moduls lief" 'echo "$out" | grep -q "probe-check $T/d"'
check "Dateiliste gemerkt" '[ "$(sort "$T/d/modules/probe.files" | tr "\n" " ")" = "bin/probe services/probe.sh tidal/lib/libx.so " ]'
out=$(sh "$S" --dir "$T/d" --module "$T/probe.tar.gz" 2>&1)
check "erneut installieren geht" '[ -x "$T/d/bin/probe" ] && [ "$(wc -l < "$T/d/modules/probe.files")" = 3 ]'
sh "$S" --dir "$T/d" --remove-module probe >/dev/null 2>&1
check "Entfernen löscht die Moduldateien und leere Modulordner" '[ ! -e "$T/d/bin/probe" ] && [ ! -e "$T/d/tidal" ] && [ ! -e "$T/d/modules/probe.files" ]'
check "andere Dateien und Ordner bleiben" '[ -f "$T/d/bin/leuchtfeuerd" ] && [ -d "$T/d/log" ] && [ -d "$T/d/services" ] && [ -f "$T/d/config" ]'
check "Entfernen eines fehlenden Moduls scheitert" '! sh "$S" --dir "$T/d" --remove-module probe >/dev/null 2>&1'
printf 'name=../böse\narch=any\n' > "$T/m/MODULE"; tar -C "$T/m" -czf "$T/bad.tar.gz" .
check "ungültiger Modulname abgelehnt" '! sh "$S" --dir "$T/d" --module "$T/bad.tar.gz" >/dev/null 2>&1'
printf 'name=fremd\narch=mips\n' > "$T/m/MODULE"; tar -C "$T/m" -czf "$T/arch.tar.gz" .
check "fremde Architektur abgelehnt" '! sh "$S" --dir "$T/d" --module "$T/arch.tar.gz" >/dev/null 2>&1'
sh "$S" --dir "$T/d" --module "$T/probe.tar.gz" >/dev/null 2>&1
sh "$S" --dir "$T/d" --uninstall >/dev/null 2>&1
check "Deinstallieren entfernt auch Module, Einstellungen bleiben" '[ ! -e "$T/d/tidal" ] && [ ! -e "$T/d/modules" ] && [ -f "$T/d/config" ]'
exit $fail
