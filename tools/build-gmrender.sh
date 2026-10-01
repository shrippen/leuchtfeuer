#!/usr/bin/env bash
# Baut gmrender-resurrect (UPnP/DLNA-Renderer) für den Invoke: glibc 2.23 armhf (Xenial-Cross),
# libupnp 1.14 statisch eingebunden, GStreamer/GLib dynamisch aus dem Gerät (1.10 / 2.46).
#   tools/build-gmrender.sh   -> build/gmrender/gmediarender
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/build/gmrender
mkdir -p "$out"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" invoke-xenial-armhf bash -euc '
  H=arm-linux-gnueabihf; P=/opt/upnp
  export PKG_CONFIG_LIBDIR=/usr/lib/$H/pkgconfig:/usr/share/pkgconfig:$P/lib/pkgconfig
  cd /tmp
  git clone -q --depth 1 --branch release-1.14.31 https://github.com/pupnp/pupnp.git
  (cd pupnp && ./bootstrap >/dev/null 2>&1 && ./configure -q --host=$H --prefix=$P --enable-static --disable-shared \
     --disable-samples --disable-blocking-tcp-connections --enable-ipv6=no >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  git clone -q https://github.com/hzeller/gmrender-resurrect.git && cd gmrender-resurrect
  git checkout -q 3d87b3678bef4d0886cc526fe75d5e1285704310
  git rev-parse HEAD > /out/source.commit
  ./autogen.sh >/dev/null 2>&1
  ./configure -q --host=$H --prefix=/data/leuchtfeuer >/dev/null
  make -s -j$(nproc) >/dev/null
  $H-strip src/gmediarender && cp src/gmediarender /out/
  $H-readelf -d /out/gmediarender | grep NEEDED > /out/needed.txt
  $H-objdump -T /out/gmediarender | awk "/UND/{print \$NF}" | sort -u > /out/undefined.txt
'
file "$out/gmediarender"; cat "$out/needed.txt"
