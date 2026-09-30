#!/usr/bin/env bash
# Baut den Sendspin-Player (sendspin-go) für den Invoke: Go + cgo gegen glibc 2.23 armhf
# (Xenial-Cross). miniaudio lädt libasound.so.2 des Geräts zur Laufzeit; libopus 1.5.2 statisch.
#   tools/build-sendspin.sh [tag]   -> build/sendspin/sendspin-player
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tag=${1:-v1.8.2}
out=$here/build/sendspin
mkdir -p "$out" "$here/build/go-cache"
docker run --rm -v "$out:/out" -v "$here/build/go-cache:/root/go" -e TAG="$tag" invoke-xenial-armhf bash -euc '
  cd /tmp && wget -q https://go.dev/dl/go1.25.1.linux-amd64.tar.gz && tar xzf go1.25.1.linux-amd64.tar.gz
  # libopus aktuell und statisch (Xenial hat 1.1.2, zu alt für hraban/opus)
  wget -q https://downloads.xiph.org/releases/opus/opus-1.5.2.tar.gz && tar xzf opus-1.5.2.tar.gz
  (cd opus-1.5.2 && ./configure -q --host=arm-linux-gnueabihf --prefix=/opt/opus --enable-static --disable-shared \
     --disable-doc --disable-extra-programs CFLAGS="-O2 -fPIC -mfpu=neon" >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  sha256sum opus-1.5.2.tar.gz > /out/opus.sha256
  export PATH=/tmp/go/bin:$PATH
  mkdir -p /tmp/pc && cat > /tmp/pc/opus.pc <<PC
Name: opus
Description: opus (statisch)
Version: 1.5.2
Cflags: -I/opt/opus/include/opus
Libs: /opt/opus/lib/libopus.a -lm
PC
  git clone -q --depth 1 --branch "$TAG" https://github.com/Sendspin/sendspin-go.git && cd sendspin-go
  git rev-parse HEAD > /out/source.commit
  # Invoke: miniaudio spinnt mit mmap auf dmix/softvol (ein Kern 100 % beim Abspielen) -> kein mmap
  sed -i "s/^\(\s*\)deviceConfig.PeriodSizeInMilliseconds = 20$/&\n\1deviceConfig.Alsa.NoMMap = 1/" pkg/audio/output/malgo.go
  grep -q "Alsa.NoMMap = 1" pkg/audio/output/malgo.go
  export CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 CC=arm-linux-gnueabihf-gcc PKG_CONFIG_LIBDIR=/tmp/pc
  go build -trimpath -tags nolibopusfile -ldflags "-s -w" -o /out/sendspin-player .
  arm-linux-gnueabihf-readelf -d /out/sendspin-player | grep NEEDED > /out/needed.txt
  arm-linux-gnueabihf-objdump -T /out/sendspin-player | awk "/UND/{print \$NF}" | sort -u > /out/undefined.txt
'
file "$out/sendspin-player"; cat "$out/needed.txt"
