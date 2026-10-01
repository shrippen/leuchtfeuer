#!/usr/bin/env bash
# Baut die eigenen Programme für das Zielgerät "generic" (beliebiges Linux): leuchtfeuerd, castrecv, btagent und die
# beiden LADSPA-Plugins der Tonkette. Musikdienste anderer Projekte (librespot, shairport-sync, gmediarender,
# snapclient, sendspin-player) kommen dort aus den Paketen des Systems (Kopfzeile "# requires:" der Dienste).
#   tools/build-generic.sh [amd64|arm64|armv7]   -> build/generic-<arch>/   (Vorgabe: Architektur dieses Rechners)
# C-Compiler: CC, sonst cc (amd64), aarch64-linux-gnu-gcc (arm64), arm-linux-gnueabihf-gcc (armv7).
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
arch=${1:-$(case $(uname -m) in x86_64) echo amd64 ;; aarch64) echo arm64 ;; armv7l) echo armv7 ;; *) uname -m ;; esac)}
case $arch in
  amd64) goarch=amd64; cc=${CC:-cc}; cflags="" ;;
  arm64) goarch=arm64; cc=${CC:-aarch64-linux-gnu-gcc}; cflags="" ;;
  armv7) goarch=arm; cc=${CC:-arm-linux-gnueabihf-gcc}; cflags="-mfpu=neon" ;;
  *) echo "Architektur $arch unbekannt (amd64, arm64, armv7)" >&2; exit 2 ;;
esac
out=$here/build/generic-$arch
mkdir -p "$out"
ver=$(git -C "$here" describe --tags --always --dirty 2>/dev/null || echo dev)
for m in leuchtfeuerd castrecv btagent; do
  (cd "$here/src/$m" && CGO_ENABLED=0 GOOS=linux GOARCH=$goarch GOARM=7 go build -trimpath -ldflags "-s -w -X main.version=$ver" -o "$out/$m" .)
done
if grep -aq "LEUCHTFEUER-DEMO-BUILD" "$out/leuchtfeuerd"; then
  rm -f "$out/leuchtfeuerd"; echo "FEHLER: Demo-Code im Release-Programm - abgebrochen" >&2; exit 1
fi
# shellcheck disable=SC2086
"$cc" -shared -fPIC -O2 -Wall -Wextra -o "$out/leuchtfeuer-viz-tap.so" "$here/device/src/leuchtfeuer-viz-tap.c"
# shellcheck disable=SC2086
"$cc" -shared -fPIC -O2 $cflags -Wall -Wextra -o "$out/leuchtfeuer-eq.so" "$here/device/src/leuchtfeuer-eq.c" -lm
file "$out"/* 2>/dev/null || ls -la "$out"
