#!/usr/bin/env bash
# Stellt Tidal Connect zusammen (armhf):
#   tools/build-tidal-bundle.sh [invoke]   -> build/tidal/           (auf den Invoke nach /data/leuchtfeuer/tidal)
#   tools/build-tidal-bundle.sh generic    -> build/tidal-generic/   (Modul für jedes armhf-/arm64-Linux, siehe
#                                                                     tools/make-tidal-module.sh)
#
# ACHTUNG: tidal_connect_application ist ein proprietäres iFi-Programm und meldet sich mit einem
# iFi-Gerätezertifikat bei Tidal an (Quelle: TonyTromp/tidal-connect-docker). Nicht für diese Geräte
# lizenziert; kann jederzeit gesperrt werden. Nur auf ausdrücklichen Wunsch, nie in einem Release-Paket.
#
# Das Programm stammt von 2019 und verlangt alte Schnittstellen. Damit möglichst aktuelle Software läuft:
#   - HTTPS über eine aktuelle curl mit mbedTLS (tools/build-curl-tidal.sh) statt curl 7.38 aus Jessie
#   - OpenSSL 1.0.2u aus den Debian-Sicherheitsupdates (stretch, deb9u7) für das eigene TLS des Programms; die
#     1.0er-Schnittstelle ist nicht ersetzbar. patchelf stellt die Namen von 1.0.0 auf 1.0.2 um und löst die
#     Versionsbindung; die SSLv3-Methoden, die gepflegte Builds nicht mehr haben, leitet
#     device/src/leuchtfeuer-ssl3compat.c auf die aushandelnden um (SSLv3 bleibt aus)
#   - FFmpeg 3.4.13 (letzte Version der Linie) ohne Netzwerk und TLS (tools/build-ffmpeg34.sh)
# Inhalt:
#   bin/tidal_connect_application   (sha256 geprüft, dann mit patchelf umgestellt)
#   cert/IfiAudio_ZenStream.dat
#   lib/   gemeinsam: FFmpeg, libcurl, libssl/libcrypto 1.0.2, ssl3compat, libFLAC++
#          invoke:    libstdc++ 6.0.22, portaudio, jack, opus, avahi-client/-common/-core, libdaemon (das Gerät hat
#                     sie nicht oder zu alt); sbin/avahi-daemon. Alles Übrige (glibc 2.23, libasound, libFLAC, libogg,
#                     libz, …) kommt vom Gerät.
#          generic:   libFLAC.so.8 (aktuelle Systeme haben libFLAC.so.12). glibc, libstdc++, libasound, portaudio,
#                     avahi-client, zlib, libogg kommen vom System (armhf, auf arm64 per Multiarch).
# Braucht: docker, curl, patchelf >= 0.12 (--clear-symbol-version).
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
. "$here/tools/docker-run.sh"
target=${1:-invoke}
case $target in invoke) out=$here/build/tidal ;; generic) out=$here/build/tidal-generic ;;
  *) echo "Zielgerät: invoke | generic" >&2; exit 2 ;; esac
command -v patchelf >/dev/null || { echo "patchelf fehlt (>= 0.12)" >&2; exit 1; }
patchelf --help 2>&1 | grep -q clear-symbol-version || { echo "patchelf zu alt (--clear-symbol-version fehlt)" >&2; exit 1; }
TC_COMMIT=690b76ff8c6596f2e66b347b875544e5607ca645
TC_SHA=7e659a88c3c121e4efe35ba2852aec01eb38ba9f8ba30868ad6ca3e0d5b50930
rm -rf "$out"; mkdir -p "$out"/{bin,cert,lib,debs}
[ -f "$here/build/ffmpeg34/lib/libavcodec.so.57" ] || "$here/tools/build-ffmpeg34.sh"
[ -f "$here/build/curl-tidal/libcurl.so.4" ] || "$here/tools/build-curl-tidal.sh"

raw=https://raw.githubusercontent.com/TonyTromp/tidal-connect-docker/$TC_COMMIT/Docker/src
curl -sfL -o "$out/bin/tidal_connect_application" "$raw/bin/tidal_connect_application"
echo "$TC_SHA  $out/bin/tidal_connect_application" | sha256sum -c --quiet
curl -sfL -o "$out/cert/IfiAudio_ZenStream.dat" "$raw/id_certificate/IfiAudio_ZenStream.dat"
chmod 755 "$out/bin/tidal_connect_application"

fetch(){ # fetch <image> <sources.list-Zeilen> <pakete...>
  local img=$1 src=$2; shift 2
  drun -v "$out/debs:/debs" "$img" bash -c "
    printf '%b' '$src' > /etc/apt/sources.list; rm -f /etc/apt/sources.list.d/*
    dpkg --add-architecture armhf; apt-get -o Acquire::Check-Valid-Until=false update >/dev/null 2>&1
    cd /debs; for p in $*; do apt-get -o Acquire::Check-Valid-Until=false download -q \$p:armhf >/dev/null 2>&1 || { echo \"fehlt: \$p\" >&2; exit 1; }; done"
}
stretch='deb http://archive.debian.org/debian stretch main\ndeb http://archive.debian.org/debian-security stretch/updates main\n'
pkgs="libssl1.0.2 libflac++6v5"
[ "$target" = invoke ] && pkgs="$pkgs libstdc++6 libportaudio2 libavahi-client3 libavahi-common3 libavahi-core7 libdaemon0 libjack-jackd2-0 libopus0 avahi-daemon"
[ "$target" = generic ] && pkgs="$pkgs libflac8"
fetch debian/eol:stretch "$stretch" $pkgs
( cd "$out/debs" && sha256sum ./*.deb > SHA256SUMS && grep -o 'libssl1.0.2_[^ ]*' SHA256SUMS )
x=$(mktemp -d); trap 'rm -rf "$x"' EXIT
for d in "$out"/debs/*.deb; do
  m=$(ar t "$d" | grep data.tar); case $m in *xz) ar p "$d" "$m" | tar xJ -C "$x";; *gz) ar p "$d" "$m" | tar xz -C "$x";; esac
done
libs="libssl.so.1.0.2 libcrypto.so.1.0.2 libFLAC++.so.6"
[ "$target" = invoke ] && libs="$libs libstdc++.so.6 libportaudio.so.2 libavahi-client.so.3 libavahi-common.so.3 libavahi-core.so.7 libdaemon.so.0 libjack.so.0 libopus.so.0"
[ "$target" = generic ] && libs="$libs libFLAC.so.8"
for l in $libs; do
  p=$(find "$x"/lib/arm-linux-gnueabihf "$x"/usr/lib/arm-linux-gnueabihf -maxdepth 1 -name "$l" 2>/dev/null | head -1 || true)
  [ -n "$p" ] || { echo "fehlt: $l" >&2; exit 1; }
  cp -L "$p" "$out/lib/$l"
done
[ "$target" = invoke ] && { mkdir -p "$out/sbin"; cp "$x/usr/sbin/avahi-daemon" "$out/sbin/"; }
# FFmpeg unter den Sonamen (keine Symlinks: Installer und Updates übertragen nur Dateien)
for f in "$here"/build/ffmpeg34/lib/*.so.*.*; do b=$(basename "$f"); cp "$f" "$out/lib/${b%.*.*}"; done
cp "$here/build/curl-tidal/libcurl.so.4" "$out/lib/"
# SSLv3-Ausgleich gegen die mitgelieferte libssl 1.0.2 bauen
drun --user "$(id -u):$(id -g)" -v "$here/device/src:/src:ro" -v "$out/lib:/lib-out" invoke-xenial-armhf \
  arm-linux-gnueabihf-gcc -shared -fPIC -O2 -Wall -Wextra -Werror -o /lib-out/libleuchtfeuer-ssl3compat.so \
  /src/leuchtfeuer-ssl3compat.c /lib-out/libssl.so.1.0.2
# Programm auf OpenSSL 1.0.2 umstellen: Namen ersetzen, Versionsbindung lösen, Ausgleich anhängen
bin=$out/bin/tidal_connect_application
# Symboltabellen braucht zur Laufzeit niemand (~0,9 MB)
drun --user "$(id -u):$(id -g)" -v "$out/bin:/b" invoke-xenial-armhf arm-linux-gnueabihf-strip /b/tidal_connect_application
args=()
for s in $(readelf --dyn-syms -W "$bin" | awk '$7=="UND"{print $8}' | grep '@OPENSSL' | sed 's/@.*//' | sort -u); do
  args+=(--clear-symbol-version "$s")
done
patchelf --replace-needed libssl.so.1.0.0 libssl.so.1.0.2 --replace-needed libcrypto.so.1.0.0 libcrypto.so.1.0.2 \
  --add-needed libleuchtfeuer-ssl3compat.so "${args[@]}" "$bin"
# ld.so prüft zusätzlich jede Versionsanforderung: die an libssl/libcrypto als schwach markieren (fehlt dann nicht)
python3 "$here/tools/elf-weak-verneed.py" "$bin" libssl libcrypto
if readelf -V "$bin" | grep 'Name: OPENSSL' | grep -v -q WEAK; then echo "Versionsanforderung an OpenSSL nicht schwach" >&2; exit 1; fi
readelf -d "$bin" | grep -q 'libssl.so.1.0.2' || { echo "patchelf: libssl nicht umgestellt" >&2; exit 1; }
# Vollständigkeit: jede Bibliothek, die Bündel und Programm brauchen, liegt im Bündel oder kommt vom System (Liste)
system="libc.so.6 libm.so.6 libdl.so.2 libpthread.so.0 librt.so.1 libgcc_s.so.1 ld-linux-armhf.so.3 libz.so.1 libasound.so.2 libdbus-1.so.3 libogg.so.0 libresolv.so.2"
[ "$target" = invoke ] && system="$system libFLAC.so.8"
[ "$target" = generic ] && system="$system libstdc++.so.6 libportaudio.so.2 libavahi-client.so.3 libavahi-common.so.3"
missing=""
for f in "$bin" "$out"/lib/*; do
  for n in $(readelf -d "$f" | sed -n 's/.*NEEDED.*\[\(.*\)\]/\1/p'); do
    [ -e "$out/lib/$n" ] || case " $system " in *" $n "*) ;; *) missing="$missing $(basename "$f")->$n" ;; esac
  done
done
[ -z "$missing" ] || { echo "nicht aufgelöst:$missing" >&2; exit 1; }
rm -rf "$out/debs"/*.deb
echo "$system" > "$out/SYSTEM-LIBS"
du -sh "$out"; ls "$out/lib"
