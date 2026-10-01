#!/usr/bin/env bash
# Baut librespot (Spotify Connect) statisch für den Invoke (armv7 musl), ohne ALSA:
# Ausgabe über das subprocess-Backend an das aplay des Geräts (Geräte-alsa-lib, dmix "music").
# Patch tools/patches/librespot-fixed-volume.patch: "--volume-ctrl fixed" dämpft gar nicht; die Lautstärke aus der
# Spotify-App setzt invoked über audio-ui (Ereignis volume_changed über --onevent), wie beim Drehrad.
#   tools/build-librespot.sh [tag]   -> build/librespot/librespot
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
tag=${1:-v0.8.0}
out=$here/build/librespot
mkdir -p "$out" "$here/build/cargo-cache"
docker image inspect invoke-rust-armv7 >/dev/null 2>&1 || \
  docker build -q -t invoke-rust-armv7 -f "$here/tools/docker/rust-armv7.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" -v "$here/build/cargo-cache:/usr/local/cargo/registry" -v "$here/tools/patches:/patches:ro" \
  -e TAG="$tag" invoke-rust-armv7 bash -euc '
  cd /tmp && git clone -q --depth 1 --branch "$TAG" https://github.com/librespot-org/librespot.git && cd librespot
  git rev-parse HEAD > /out/source.commit
  git apply /patches/librespot-fixed-volume.patch
  cargo build -q --release --locked --target armv7-unknown-linux-musleabihf \
    --no-default-features --features "rustls-tls-webpki-roots,with-libmdns"
  cp target/armv7-unknown-linux-musleabihf/release/librespot /out/
  /opt/tc/bin/strip /out/librespot
'
file "$out/librespot"; ls -la "$out/librespot"; cat "$out/source.commit"
