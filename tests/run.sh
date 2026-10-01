#!/bin/sh
# Alle Prüfungen, die ohne Lautsprecher laufen (auch in der CI): Go (vet, gofmt, Tests mit Race-Detector),
# Klang-Plugin, Hook, Update-Skript, Shell-Syntax (shellcheck, falls vorhanden) und die Weboberfläche (node --check).
#   tests/run.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
fail=0
step(){ printf '\n== %s\n' "$1"; shift; "$@" || { echo "FEHLER: $*"; fail=1; }; }
for m in wamp invoked btagent castrecv relsign; do
  [ -d src/$m ] || continue
  step "go vet $m" sh -c "cd src/$m && go vet ./..."
  step "gofmt $m" sh -c "cd src/$m && test -z \"\$(gofmt -l .)\" || { gofmt -l .; exit 1; }"
  step "go test $m" sh -c "cd src/$m && go test -race -count=1 ./..."
done
step "go vet invoked (Demo)" sh -c "cd src/invoked && go vet -tags demo ./..."
step "Klang-Plugin" sh -c "cc -O2 -Wall -Wextra -Werror -o /tmp/leuchtfeuer-eq-test tests/eq_test.c device/src/invoke-eq.c -lm && /tmp/leuchtfeuer-eq-test"
step "Visualizer-Plugin übersetzt" sh -c "cc -shared -fPIC -O2 -Wall -Wextra -Werror -o /tmp/leuchtfeuer-tap.so device/src/invoke-viz-tap.c"
step "Hook" sh tests/hook_test.sh
step "apply-update.sh" sh tests/apply_update_test.sh
step "Shell-Syntax" sh -c 'for f in device/invoke/*.sh device/invoke/services/*.sh; do sh -n "$f" || exit 1; done; for f in *.sh scripts/*.sh tools/*.sh tests/*.sh; do bash -n "$f" || exit 1; done'
if command -v shellcheck >/dev/null; then
  step "shellcheck (Gerät, POSIX sh)" shellcheck -s sh -S warning -e SC3043 device/invoke/*.sh device/invoke/services/*.sh
fi
step "Weboberfläche (Syntax)" sh -c "node --check src/invoked/web/app.js && node --check src/invoked/web/roomeq.js && node --check src/invoked/web/login.js"
step "Raum einmessen (Auswertung)" node tests/roomeq_test.js
step "Home-Assistant-Integration" sh tests/homeassistant/run.sh
echo
[ $fail = 0 ] && echo "Alles in Ordnung." || echo "Es gibt Fehler."
exit $fail
