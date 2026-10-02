#!/usr/bin/env bash
# Baut snapclient (Snapcast-Client, Multiroom) statisch für den Invoke (armv7 musl), ohne ALSA: Ausgabe über den
# Datei-Player auf stdout, das aplay des Geräts spielt sie über "leuchtfeuer_snapcast" ab (services/snapclient.sh).
# Codecs: FLAC (Standard von snapserver) und PCM; libFLAC, OpenSSL und alsa-lib statisch (snapcast verlangt
# beide beim Bauen; abgespielt wird trotzdem über den Datei-Player), Boost nur als Header.
#   tools/build-snapclient.sh [tag]   -> build/snapclient/snapclient
# Die Prüfsummen unten sind gegen die veröffentlichten Werte der Projekte geprüft (OpenSSL: .sha256 des Releases,
# FLAC: SHA256SUMS.txt von xiph, alsa-lib: GPG-Signatur des ALSA Release Teams). Bei neuen Versionen Version und
# Prüfsumme zusammen anpassen und die Herkunft erneut kontrollieren.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tag=${1:-v0.35.0}
out=$here/build/snapclient
FLAC_VER=1.5.0
FLAC_SHA=f2c1c76592a82ffff8413ba3c4a1299b6c7ab06c734dee03fd88630485c2b920
ALSA_VER=1.2.16.1
ALSA_SHA=f740db7f488255944ffd4428416ee3390a96742856916433df468c281436480e
SSL_VER=3.5.9   # LTS-Linie; 3.0.x läuft im Sicherheitsunterhalt aus
SSL_SHA=603f5602e2eef00d77fbd429d34dcd5822bb301757a1bc9cdb24c670f1eb859a
mkdir -p "$out"
docker image inspect invoke-armv7-musl >/dev/null 2>&1 || \
  docker build -q -t invoke-armv7-musl -f "$here/tools/docker/armv7-musl.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" -e TAG="$tag" \
  -e FLAC_VER=$FLAC_VER -e FLAC_SHA=$FLAC_SHA -e ALSA_VER=$ALSA_VER -e ALSA_SHA=$ALSA_SHA \
  -e SSL_VER=$SSL_VER -e SSL_SHA=$SSL_SHA invoke-armv7-musl bash -euc '
  apk add --no-cache -q cmake git boost-dev xz >/dev/null
  H=armv7l-linux-musleabihf
  # ohne PIE wie dropbear (statisch, fester Ladeort; der Kernel 3.8 des Geräts)
  export CFLAGS="-O2 -fno-pie" CXXFLAGS="-O2 -fno-pie"
  export CC=/opt/tc/bin/gcc CXX=/opt/tc/bin/g++ AR=/opt/tc/bin/ar RANLIB=/opt/tc/bin/ranlib
  P=/opt/deps; mkdir -p $P
  cd /tmp
  wget -q https://downloads.xiph.org/releases/flac/flac-$FLAC_VER.tar.xz
  echo "$FLAC_SHA  flac-$FLAC_VER.tar.xz" | sha256sum -cs || { echo "Pruefsumme falsch: flac-$FLAC_VER.tar.xz" >&2; exit 1; }
  tar xf flac-$FLAC_VER.tar.xz
  (cd flac-$FLAC_VER && ./configure -q --host=$H --prefix=$P --enable-static --disable-shared --disable-cpplibs --disable-programs \
     --disable-examples --disable-doxygen-docs --disable-ogg >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  wget -q https://www.alsa-project.org/files/pub/lib/alsa-lib-$ALSA_VER.tar.bz2
  echo "$ALSA_SHA  alsa-lib-$ALSA_VER.tar.bz2" | sha256sum -cs || { echo "Pruefsumme falsch: alsa-lib-$ALSA_VER.tar.bz2" >&2; exit 1; }
  tar xf alsa-lib-$ALSA_VER.tar.bz2
  (cd alsa-lib-$ALSA_VER && ./configure -q --host=$H --prefix=$P --enable-static --disable-shared --disable-python \
     --disable-ucm --disable-topology >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  wget -q https://github.com/openssl/openssl/releases/download/openssl-$SSL_VER/openssl-$SSL_VER.tar.gz
  echo "$SSL_SHA  openssl-$SSL_VER.tar.gz" | sha256sum -cs || { echo "Pruefsumme falsch: openssl-$SSL_VER.tar.gz" >&2; exit 1; }
  tar xf openssl-$SSL_VER.tar.gz
  (cd openssl-$SSL_VER && ./Configure linux-armv4 no-shared no-tests --prefix=$P --libdir=lib >/dev/null \
     && make -s -j$(nproc) build_libs >/dev/null && make -s install_dev >/dev/null)
  { echo "flac $FLAC_VER $FLAC_SHA"; echo "alsa-lib $ALSA_VER $ALSA_SHA"; echo "openssl $SSL_VER $SSL_SHA"; } > /out/deps.sha256
  export PKG_CONFIG_PATH=$P/lib/pkgconfig PKG_CONFIG_LIBDIR=$P/lib/pkgconfig
  git clone -q --depth 1 --branch "$TAG" https://github.com/badaix/snapcast.git && cd snapcast
  git rev-parse HEAD > /out/source.commit
  mkdir build && cd build
  cmake .. -DCMAKE_BUILD_TYPE=Release -DCMAKE_SYSTEM_NAME=Linux -DCMAKE_SYSTEM_PROCESSOR=arm \
    -DCMAKE_FIND_ROOT_PATH="$P" -DCMAKE_PREFIX_PATH="$P" -DBoost_INCLUDE_DIR=/usr/include -DOPENSSL_ROOT_DIR="$P" -DOPENSSL_USE_STATIC_LIBS=ON \
    -DBUILD_SERVER=OFF -DBUILD_CLIENT=ON -DBUILD_TESTS=OFF -DBUILD_WITH_ALSA=OFF -DBUILD_WITH_PULSE=OFF \
    -DBUILD_WITH_AVAHI=OFF -DBUILD_WITH_VORBIS=OFF -DBUILD_WITH_TREMOR=OFF -DBUILD_WITH_OPUS=OFF -DBUILD_WITH_FLAC=ON \
    -DBUILD_WITH_EXPAT=OFF -DBUILD_WITH_JACK=OFF -DBUILD_SHARED_LIBS=OFF -DBUILD_STATIC_LIBS=ON \
    -DCMAKE_C_COMPILER=$CC -DCMAKE_CXX_COMPILER=$CXX -DCMAKE_POSITION_INDEPENDENT_CODE=OFF -DCMAKE_EXE_LINKER_FLAGS="-static -no-pie" >/out/cmake.log 2>&1 || { tail -30 /out/cmake.log; exit 1; }
  make -s -j$(nproc) snapclient >/out/make.log 2>&1 || { tail -40 /out/make.log; exit 1; }
  f=$(find .. -name snapclient -type f -perm -u+x | head -n 1)
  /opt/tc/bin/strip "$f" && cp "$f" /out/snapclient
'
file "$out/snapclient"; ls -la "$out/snapclient"; cat "$out/source.commit"
