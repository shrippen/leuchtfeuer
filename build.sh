#!/usr/bin/env bash
# Baut alle Programme für den Invoke (ARMv7) nach build/. Braucht Docker (Toolchains für glibc 2.23 und
# musl), Go ≥ 1.22 (castrecv, btagent), curl, git. Dauer je nach Rechner 20-60 Minuten; bereits gebaute
# Teile werden übersprungen (mit --force neu bauen).
#
#   ./build.sh [--force] [--no-tidal]
#
# Ergebnis: build/dropbear, librespot, gmrender, sendspin, castrecv, btagent, bluez, shim, (tidal)
set -euo pipefail
cd "$(dirname "$0")"
force=0; tidal=1
for a in "$@"; do
  case $a in
    --force) force=1 ;;
    --no-tidal) tidal=0 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "unbekannte Option: $a" >&2; exit 2 ;;
  esac
done
for c in docker go curl git file; do command -v $c >/dev/null || { echo "fehlt: $c" >&2; exit 1; }; done

step(){ # step <Name> <Ergebnisdatei> <Skript>
  if [ $force = 0 ] && [ -e "$2" ]; then echo "== $1: vorhanden ($2)"; return; fi
  echo "== $1 ..."; "$3"
}
step "dropbear (SSH)"         build/dropbear/dropbearmulti    tools/build-dropbear.sh
step "librespot (Spotify)"    build/librespot/librespot       tools/build-librespot.sh
step "gmrender (UPnP/DLNA)"   build/gmrender/gmediarender     tools/build-gmrender.sh
step "sendspin-go"            build/sendspin/sendspin-player  tools/build-sendspin.sh
step "castrecv (Cast)"        build/castrecv/castrecv         tools/build-castrecv.sh
step "btagent"                build/btagent/btagent           tools/build-btagent.sh
step "BlueZ + bluez-alsa"     build/bluez/bluetoothd          tools/build-bluez.sh
step "avahi-Shim"             build/shim/avahi-user-shim.so   tools/build-shim.sh
if [ $tidal = 1 ]; then
  step "Tidal-Connect-Bündel" build/tidal/bin/tidal_connect_application tools/build-tidal-bundle.sh
fi
echo "Fertig. Weiter mit ./install.sh"
