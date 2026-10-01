#!/usr/bin/env bash
# Baut leuchtfeuerd (src/leuchtfeuerd: Weboberfläche, WLAN-Wächter, Webradio, Wecker, Timer, Tastenbelegung, Home-Assistant-
# Anbindung) für den Invoke (ARMv7, reines Go ohne cgo).
#   tools/build-leuchtfeuerd.sh   -> build/leuchtfeuerd/leuchtfeuerd
# Prüft, dass kein Demo-Code im Programm steckt: Der Demo-Modus (nur Screenshots, Build-Tag "demo") darf nie
# ausgeliefert werden; steht seine Marke im Ergebnis, wird es gelöscht und der Build bricht ab.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$here/build/leuchtfeuerd"
ver=$(git -C "$here" describe --tags --always --dirty 2>/dev/null || echo dev)
cd "$here/src/leuchtfeuerd"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w -X main.version=$ver" -o "$here/build/leuchtfeuerd/leuchtfeuerd" .
if grep -aq "LEUCHTFEUER-DEMO-BUILD" "$here/build/leuchtfeuerd/leuchtfeuerd"; then
  rm -f "$here/build/leuchtfeuerd/leuchtfeuerd"
  echo "FEHLER: Demo-Code im Release-Programm (Marke LEUCHTFEUER-DEMO-BUILD) - abgebrochen" >&2
  exit 1
fi
file "$here/build/leuchtfeuerd/leuchtfeuerd"
