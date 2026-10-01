#!/usr/bin/env bash
# Baut die LADSPA-Plugins der Tonkette (asound-music.conf) gegen glibc 2.23 armhf (Xenial-Cross):
#   invoke-viz-tap.so  Tonabgriff für den Leuchtring-Visualizer (device/src/invoke-viz-tap.c)
#   invoke-eq.so       Klang: Bass, Höhen, Raumkorrektur, Loudness, Nachtmodus (device/src/invoke-eq.c)
# Beide sind Pflicht: asound-music.conf bindet sie ein, ohne sie gibt es keinen Ton.
#   tools/build-viztap.sh   -> build/viztap/invoke-viz-tap.so, build/viztap/invoke-eq.so
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$here/build/viztap"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
docker run --rm -v "$here/device/src:/src:ro" -v "$here/build/viztap:/out" invoke-xenial-armhf bash -euc '
  arm-linux-gnueabihf-gcc -shared -fPIC -O2 -Wall -Wextra -o /out/invoke-viz-tap.so /src/invoke-viz-tap.c
  arm-linux-gnueabihf-gcc -shared -fPIC -O2 -mfpu=neon -Wall -Wextra -o /out/invoke-eq.so /src/invoke-eq.c -lm
'
file "$here/build/viztap/invoke-viz-tap.so" "$here/build/viztap/invoke-eq.so"
