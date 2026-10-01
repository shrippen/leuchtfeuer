#!/usr/bin/env bash
# Builds all programs for the Invoke (ARMv7) into build/. Needs Docker (toolchains for glibc 2.23 and musl),
# Go >= 1.22 (castrecv, btagent), curl, git. 20-60 minutes depending on the computer; finished parts are skipped
# (--force rebuilds). / Baut alle Programme für den Invoke (ARMv7) nach build/.
#
#   ./build.sh [--force] [--no-tidal]
#
# Result: build/dropbear, librespot, gmrender, sendspin, castrecv, btagent, bluez, shim, invoked, shairport, snapclient,
# viztap (LADSPA plugins), (tidal). A signed release package for web updates: tools/make-release.sh
set -euo pipefail
cd "$(dirname "$0")"
. scripts/lib.sh
force=0; tidal=1
for a in "$@"; do
  case $a in
    --force) force=1 ;;
    --no-tidal) tidal=0 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "unknown option: $a" >&2; exit 2 ;;
  esac
done
for c in docker go curl git file; do command -v $c >/dev/null || die "$c is missing" "$c fehlt"; done

step(){ # step <Name> <Ergebnisdatei> <Skript>
  if [ $force = 0 ] && [ -e "$2" ]; then ok "$1: already built ($2)" "$1: schon gebaut ($2)"; return; fi
  say "$1 ..." "$1 ..."; "$3"
}
step "dropbear (SSH)"         build/dropbear/dropbearmulti    tools/build-dropbear.sh
step "librespot (Spotify)"    build/librespot/librespot       tools/build-librespot.sh
step "gmrender (UPnP/DLNA)"   build/gmrender/gmediarender     tools/build-gmrender.sh
step "sendspin-go"            build/sendspin/sendspin-player  tools/build-sendspin.sh
step "castrecv (Cast)"        build/castrecv/castrecv         tools/build-castrecv.sh
step "btagent"                build/btagent/btagent           tools/build-btagent.sh
step "BlueZ + bluez-alsa"     build/bluez/bluetoothd          tools/build-bluez.sh
step "avahi-Shim"             build/shim/avahi-user-shim.so   tools/build-shim.sh
step "invoked (Web, Wecker, ...)" build/invoked/invoked        tools/build-invoked.sh
step "shairport-sync (AirPlay)" build/shairport/shairport-sync tools/build-shairport.sh
step "snapclient (Snapcast)"   build/snapclient/snapclient     tools/build-snapclient.sh
step "Tonkette (LADSPA: Abgriff, Klang)" build/viztap/invoke-eq.so tools/build-viztap.sh
if [ $tidal = 1 ]; then
  step "Tidal-Connect-Bündel" build/tidal/bin/tidal_connect_application tools/build-tidal-bundle.sh
fi
info "Done. Next: ./install.sh" "Fertig. Weiter mit ./install.sh"
