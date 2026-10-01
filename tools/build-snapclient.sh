#!/usr/bin/env bash
# Baut snapclient (Snapcast-Client, Multiroom) statisch für den Invoke (armv7 musl), ohne ALSA: Ausgabe über den
# Datei-Player auf stdout, das aplay des Geräts spielt sie über "leuchtfeuer_snapcast" ab (services/snapclient.sh).
# Codecs: FLAC (Standard von snapserver) und PCM; libFLAC, OpenSSL und alsa-lib statisch (snapcast verlangt
# beide beim Bauen; abgespielt wird trotzdem über den Datei-Player), Boost nur als Header.
#   tools/build-snapclient.sh [tag]   -> build/snapclient/snapclient
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tag=${1:-v0.31.0}
out=$here/build/snapclient
mkdir -p "$out"
docker image inspect invoke-armv7-musl >/dev/null 2>&1 || \
  docker build -q -t invoke-armv7-musl -f "$here/tools/docker/armv7-musl.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" -e TAG="$tag" invoke-armv7-musl bash -euc '
  apk add --no-cache -q cmake git boost-dev xz >/dev/null
  H=armv7l-linux-musleabihf
  # ohne PIE wie dropbear (statisch, fester Ladeort; der Kernel 3.8 des Geräts)
  export CFLAGS="-O2 -fno-pie" CXXFLAGS="-O2 -fno-pie"
  export CC=/opt/tc/bin/gcc CXX=/opt/tc/bin/g++ AR=/opt/tc/bin/ar RANLIB=/opt/tc/bin/ranlib
  P=/opt/deps; mkdir -p $P
  cd /tmp
  wget -q https://downloads.xiph.org/releases/flac/flac-1.4.3.tar.xz && tar xf flac-1.4.3.tar.xz
  (cd flac-1.4.3 && ./configure -q --host=$H --prefix=$P --enable-static --disable-shared --disable-cpplibs --disable-programs \
     --disable-examples --disable-doxygen-docs --disable-ogg >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  wget -q https://www.alsa-project.org/files/pub/lib/alsa-lib-1.2.12.tar.bz2 && tar xf alsa-lib-1.2.12.tar.bz2
  (cd alsa-lib-1.2.12 && ./configure -q --host=$H --prefix=$P --enable-static --disable-shared --disable-python \
     --disable-ucm --disable-topology >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  wget -q https://github.com/openssl/openssl/releases/download/openssl-3.0.15/openssl-3.0.15.tar.gz && tar xf openssl-3.0.15.tar.gz
  (cd openssl-3.0.15 && ./Configure linux-armv4 no-shared no-tests --prefix=$P --libdir=lib >/dev/null \
     && make -s -j$(nproc) build_libs >/dev/null && make -s install_dev >/dev/null)
  sha256sum flac-1.4.3.tar.xz alsa-lib-1.2.12.tar.bz2 openssl-3.0.15.tar.gz > /out/deps.sha256
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
