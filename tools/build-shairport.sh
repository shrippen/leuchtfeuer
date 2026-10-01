#!/usr/bin/env bash
# Baut shairport-sync (AirPlay-1-Empfänger) für den Invoke: glibc 2.23 armhf (Xenial-Cross), popt/libconfig/OpenSSL und
# Apples ALAC-Dekoder (mikebrady/alac) statisch eingebunden, ALSA aus dem Gerät, mDNS über das eingebaute tinysvcmdns (kein avahi nötig).
#   tools/build-shairport.sh [tag]   -> build/shairport/shairport-sync
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tag=${1:-3.3.9}
out=$here/build/shairport
mkdir -p "$out"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
# das bt-Image bringt popt/libconfig/openssl-Header: bei Änderung der Dockerfile neu bauen
docker image inspect invoke-xenial-armhf-bt >/dev/null 2>&1 && \
  ! docker run --rm invoke-xenial-armhf-bt test -e /usr/include/popt.h 2>/dev/null && docker rmi invoke-xenial-armhf-bt >/dev/null
docker image inspect invoke-xenial-armhf-bt >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf-bt -f "$here/tools/docker/xenial-armhf-bt.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" -e TAG="$tag" invoke-xenial-armhf-bt bash -euc '
  H=arm-linux-gnueabihf
  export PKG_CONFIG_LIBDIR=/usr/lib/$H/pkgconfig:/usr/share/pkgconfig
  cd /tmp
  # Apple-ALAC-Dekoder (der eingebaute von shairport-sync 3.3 stürzt bei manchen Strömen ab)
  git clone -q --depth 1 https://github.com/mikebrady/alac.git && (cd alac && autoreconf -fi >/dev/null 2>&1 \
     && ./configure -q --host=$H --prefix=/opt/alac --enable-static --disable-shared >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  export PKG_CONFIG_PATH=/opt/alac/lib/pkgconfig
  git clone -q --depth 1 --branch "$TAG" https://github.com/mikebrady/shairport-sync.git && cd shairport-sync
  git rev-parse HEAD > /out/source.commit
  autoreconf -fi >/dev/null 2>&1
  ./configure --host=$H --with-alsa --with-ssl=openssl --with-tinysvcmdns --with-metadata --with-apple-alac CPPFLAGS="-I/opt/alac/include" CXXFLAGS="-I/opt/alac/include -O2" \
     --sysconfdir=/data/invoke/shairport >/dev/null
  # popt, libconfig, OpenSSL und ALAC statisch einbinden (erst nach configure: dessen Pthread-Prüfung verträgt kein -Bstatic)
  L=/usr/lib/$H
  CXXLIB=$($H-g++ -print-file-name=libstdc++.a)
  sed -i "s|-lconfig|$L/libconfig.a|g; s|-lpopt|$L/libpopt.a|g; s|-lssl|$L/libssl.a|g; s|-lcrypto|$L/libcrypto.a -ldl|g; s|-lalac|/opt/alac/lib/libalac.a $CXXLIB -lm|g" Makefile
  make -s -j$(nproc) >/dev/null
  $H-strip shairport-sync && cp shairport-sync /out/
  $H-readelf -d /out/shairport-sync | grep NEEDED > /out/needed.txt
'
file "$out/shairport-sync"; cat "$out/needed.txt"
