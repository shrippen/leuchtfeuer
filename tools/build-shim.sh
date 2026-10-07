#!/usr/bin/env bash
# Baut den LD_PRELOAD-Shim für avahi-daemon (liefert den Benutzer "avahi", /etc/passwd des Geräts ist
# schreibgeschützt) gegen glibc 2.23 armhf (Xenial-Cross). Quelle: device/src/avahi-user-shim.c
#   tools/build-shim.sh   -> build/shim/avahi-user-shim.so
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
. "$here/tools/docker-run.sh"
mkdir -p "$here/build/shim"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
drun -v "$here/device/src:/src:ro" -v "$here/build/shim:/out" invoke-xenial-armhf \
  arm-linux-gnueabihf-gcc -shared -fPIC -O2 -o /out/avahi-user-shim.so /src/avahi-user-shim.c -ldl
file "$here/build/shim/avahi-user-shim.so"
