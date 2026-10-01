#!/usr/bin/env bash
# Baut das LADSPA-Plugin für den Leuchtring-Visualizer (Tonabgriff in asound-music.conf) gegen glibc 2.23 armhf
# (Xenial-Cross). Quelle: device/src/invoke-viz-tap.c
#   tools/build-viztap.sh   -> build/viztap/invoke-viz-tap.so
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$here/build/viztap"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
docker run --rm -v "$here/device/src:/src:ro" -v "$here/build/viztap:/out" invoke-xenial-armhf \
  arm-linux-gnueabihf-gcc -shared -fPIC -O2 -Wall -Wextra -o /out/invoke-viz-tap.so /src/invoke-viz-tap.c
file "$here/build/viztap/invoke-viz-tap.so"
