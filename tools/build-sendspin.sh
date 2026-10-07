#!/usr/bin/env bash
# Baut den Sendspin-Player (sendspin-go) für den Invoke: Go + cgo gegen glibc 2.23 armhf
# (Xenial-Cross). miniaudio lädt libasound.so.2 des Geräts zur Laufzeit; libopus statisch.
#   tools/build-sendspin.sh [tag]   -> build/sendspin/sendspin-player
# Die Prüfsummen unten sind gegen die veröffentlichten Werte geprüft (Go: Liste auf go.dev/dl, opus: SHA256SUMS.txt
# von xiph). Bei neuen Versionen Version und Prüfsumme zusammen anpassen und die Herkunft erneut kontrollieren.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
. "$here/tools/docker-run.sh"
tag=${1:-v1.8.2}
out=$here/build/sendspin
GO_VER=1.27.1
GO_SHA=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445
OPUS_VER=1.6.1
OPUS_SHA=6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1
mkdir -p "$out" "$here/build/go-cache"
drun -v "$out:/out" -v "$here/build/go-cache:/root/go" -e TAG="$tag" \
  -e GO_VER=$GO_VER -e GO_SHA=$GO_SHA -e OPUS_VER=$OPUS_VER -e OPUS_SHA=$OPUS_SHA invoke-xenial-armhf bash -euc '
  cd /tmp && wget -q https://go.dev/dl/go$GO_VER.linux-amd64.tar.gz
  echo "$GO_SHA  go$GO_VER.linux-amd64.tar.gz" | sha256sum -c --quiet
  tar xzf go$GO_VER.linux-amd64.tar.gz
  # libopus aktuell und statisch (Xenial hat 1.1.2, zu alt für hraban/opus)
  wget -q https://downloads.xiph.org/releases/opus/opus-$OPUS_VER.tar.gz
  echo "$OPUS_SHA  opus-$OPUS_VER.tar.gz" | sha256sum -c --quiet
  tar xzf opus-$OPUS_VER.tar.gz
  (cd opus-$OPUS_VER && ./configure -q --host=arm-linux-gnueabihf --prefix=/opt/opus --enable-static --disable-shared \
     --disable-doc --disable-extra-programs CFLAGS="-O2 -fPIC -mfpu=neon" >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  { echo "go $GO_VER $GO_SHA"; echo "opus $OPUS_VER $OPUS_SHA"; } > /out/opus.sha256
  export PATH=/tmp/go/bin:$PATH
  mkdir -p /tmp/pc && cat > /tmp/pc/opus.pc <<PC
Name: opus
Description: opus (statisch)
Version: $OPUS_VER
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
