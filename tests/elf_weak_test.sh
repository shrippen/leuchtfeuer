#!/bin/sh
# Prüft tools/elf-weak-verneed.py: markiert die Versionsanforderungen an eine Bibliothek als schwach, lässt andere
# unberührt, und das Programm läuft danach noch (ld.so akzeptiert die Datei).
#   sh tests/elf_weak_test.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
fail=0
check(){ if eval "$2"; then echo "ok    $1"; else echo "FEHLER $1"; fail=1; fi; }
command -v cc >/dev/null && command -v readelf >/dev/null || { echo "übersprungen: cc oder readelf fehlt"; exit 0; }
printf '#include <stdio.h>\n#include <math.h>\nint main(int c, char **v){ (void)v; printf("%%.0f\\n", floor(c + 0.5)); return 0; }\n' > "$T/p.c"
cc -o "$T/p" "$T/p.c" -lm || { echo "FEHLER Übersetzen"; exit 1; }
out=$(python3 "$ROOT/tools/elf-weak-verneed.py" "$T/p" libm)
check "Anforderungen an libm gemeldet" 'echo "$out" | grep -q "^schwach: libm"'
check "libm-Anforderungen schwach" '! readelf -V "$T/p" | sed -n "/File: libm/,/File:/p" | grep "Name:" | grep -v -q WEAK'
check "libc-Anforderungen unverändert" 'readelf -V "$T/p" | sed -n "/File: libc.so/,/File:/p" | grep "Name: GLIBC" | grep -v -q WEAK'
check "Programm läuft weiter" '[ "$("$T/p")" = 1 ]'
check "zweiter Lauf ändert nichts" '[ -z "$(python3 "$ROOT/tools/elf-weak-verneed.py" "$T/p" libm)" ]'
exit $fail
