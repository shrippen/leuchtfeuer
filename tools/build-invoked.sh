#!/usr/bin/env bash
# Baut invoked (src/invoked: Weboberfläche, WLAN-Wächter, Webradio, Wecker, Timer, Tastenbelegung, Home-Assistant-
# Anbindung) für den Invoke (ARMv7, reines Go ohne cgo).
#   tools/build-invoked.sh   -> build/invoked/invoked
# Prüft, dass kein Demo-Code im Programm steckt: Der Demo-Modus (nur Screenshots, Build-Tag "demo") darf nie
# ausgeliefert werden; steht seine Marke im Ergebnis, wird es gelöscht und der Build bricht ab.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$here/build/invoked"
ver=$(git -C "$here" describe --tags --always --dirty 2>/dev/null || echo dev)
cd "$here/src/invoked"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w -X main.version=$ver" -o "$here/build/invoked/invoked" .
if grep -aq "INVOKE-DEMO-BUILD" "$here/build/invoked/invoked"; then
  rm -f "$here/build/invoked/invoked"
  echo "FEHLER: Demo-Code im Release-Programm (Marke INVOKE-DEMO-BUILD) - abgebrochen" >&2
  exit 1
fi
file "$here/build/invoked/invoked"
