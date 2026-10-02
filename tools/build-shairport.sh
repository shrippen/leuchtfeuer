#!/usr/bin/env bash
# Baut shairport-sync (AirPlay-1-Empfänger) für den Invoke: glibc 2.23 armhf (Xenial-Cross), popt/libconfig/mbedTLS und
# Apples ALAC-Dekoder (mikebrady/alac) statisch eingebunden, ALSA aus dem Gerät, mDNS über das eingebaute tinysvcmdns (kein avahi nötig).
#   tools/build-shairport.sh [tag]   -> build/shairport/shairport-sync
#
# TLS: mbedTLS (LTS, statisch) statt des OpenSSL 1.0.2 aus Xenial. Xenials libssl/libcrypto sind seit 2019 ohne
# Sicherheitsunterhalt; shairport-sync 4.x unterstützt mbedTLS 3.x ausdrücklich (MBEDTLS_VERSION_MAJOR == 3).
# mbedTLS ist zudem klein und wird hier schon für Tidal Connect verwendet (tools/build-curl-tidal.sh).
#
# 4.x statt der alten 3.3.9: die 3er-Linie wird nicht mehr gepflegt. AirPlay 2 bleibt aus (--with-airplay-2 würde
# libplist, libsodium, libgcrypt und FFmpeg nachziehen); AirPlay 1 braucht davon nichts.
# Die Prüfsumme von mbedTLS entspricht der des GitHub-Releases. Bei neuen Versionen beide zusammen anpassen.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tag=${1:-4.3.7}
out=$here/build/shairport
MBED_VER=3.6.7
MBED_SHA=a7e8bcbec0e6f761b4af24f25677626b35f762f68eef79c08677a363212d11f6
mkdir -p "$out"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
# das bt-Image bringt popt/libconfig-Header: bei Änderung der Dockerfile neu bauen
docker image inspect invoke-xenial-armhf-bt >/dev/null 2>&1 && \
  ! docker run --rm invoke-xenial-armhf-bt test -e /usr/include/popt.h 2>/dev/null && docker rmi invoke-xenial-armhf-bt >/dev/null
docker image inspect invoke-xenial-armhf-bt >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf-bt -f "$here/tools/docker/xenial-armhf-bt.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" -e TAG="$tag" -e MBED_VER=$MBED_VER -e MBED_SHA=$MBED_SHA invoke-xenial-armhf-bt bash -euc '
  H=arm-linux-gnueabihf; M=/opt/mbed
  export PKG_CONFIG_LIBDIR=/usr/lib/$H/pkgconfig:/usr/share/pkgconfig
  cd /tmp
  # mbedTLS statisch. Ohne getrandom (Kernel < 3.17, der Invoke hat 3.8) liest mbedTLS sonst /dev/random und
  # blockiert bei wenig Entropie minutenlang; /dev/urandom ist nach dem Start gleichwertig (wie bei curl-tidal).
  wget -q https://github.com/Mbed-TLS/mbedtls/releases/download/mbedtls-$MBED_VER/mbedtls-$MBED_VER.tar.bz2
  echo "$MBED_SHA  mbedtls-$MBED_VER.tar.bz2" | sha256sum -c --quiet
  tar xjf mbedtls-$MBED_VER.tar.bz2 && cd mbedtls-$MBED_VER
  make -s -C library CC=$H-gcc AR=$H-ar CFLAGS="-O2 -fPIC -DMBEDTLS_PLATFORM_DEV_RANDOM=\\\"/dev/urandom\\\"" static >/dev/null
  mkdir -p $M/lib && cp library/*.a $M/lib/ && cp -r include $M/
  cd /tmp
  # Apple-ALAC-Dekoder (der eingebaute von shairport-sync stürzt bei manchen Strömen ab)
  git clone -q --depth 1 https://github.com/mikebrady/alac.git && (cd alac && autoreconf -fi >/dev/null 2>&1 \
     && ./configure -q --host=$H --prefix=/opt/alac --enable-static --disable-shared >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  export PKG_CONFIG_PATH=/opt/alac/lib/pkgconfig
  git clone -q --depth 1 --branch "$TAG" https://github.com/mikebrady/shairport-sync.git && cd shairport-sync
  git rev-parse HEAD > /out/source.commit
  autoreconf -fi >/dev/null 2>&1
  # LIBS mit mbedx509/mbedcrypto vorbelegen: configure prüft zuerst -lmbedtls allein, und das löst bei
  # statischem mbedTLS die Symbole der beiden anderen Teile nicht auf (die Prüfung schlüge sonst fehl).
  ./configure --host=$H --with-alsa --with-ssl=mbedtls --with-tinysvcmdns --with-metadata --with-apple-alac \
     CPPFLAGS="-I/opt/alac/include -I$M/include" LDFLAGS="-L$M/lib" CXXFLAGS="-I/opt/alac/include -O2" \
     LIBS="-lmbedx509 -lmbedcrypto" \
     --sysconfdir=/data/leuchtfeuer/shairport >/dev/null
  # popt, libconfig, mbedTLS und ALAC statisch einbinden (erst nach configure: dessen Pthread-Prüfung verträgt kein -Bstatic)
  L=/usr/lib/$H
  CXXLIB=$($H-g++ -print-file-name=libstdc++.a)
  sed -i "s|-lconfig|$L/libconfig.a|g; s|-lpopt|$L/libpopt.a|g; s|-lalac|/opt/alac/lib/libalac.a $CXXLIB -lm|g" Makefile
  # Reihenfolge wichtig: mbedtls -> mbedx509 -> mbedcrypto
  sed -i "s|-lmbedtls|$M/lib/libmbedtls.a|g; s|-lmbedx509|$M/lib/libmbedx509.a|g; s|-lmbedcrypto|$M/lib/libmbedcrypto.a|g" Makefile
  make -s -j$(nproc) >/dev/null
  $H-strip shairport-sync && cp shairport-sync /out/
  $H-readelf -d /out/shairport-sync | grep NEEDED > /out/needed.txt
  # Gegenprobe: kein OpenSSL mehr drin, mbedTLS wirklich statisch (keine NEEDED-Zeile dafür)
  if $H-readelf -d /out/shairport-sync | grep -qE "libssl|libcrypto|libmbed"; then
    echo "FEHLER: TLS-Bibliothek dynamisch gebunden statt statisch" >&2; exit 1
  fi
'
file "$out/shairport-sync"; ls -la "$out/shairport-sync"; cat "$out/needed.txt"
