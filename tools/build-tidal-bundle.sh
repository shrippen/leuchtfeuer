#!/usr/bin/env bash
# Stellt Tidal Connect für den Invoke zusammen -> build/tidal/ (auf das Gerät nach /data/leuchtfeuer/tidal).
#
# ACHTUNG: tidal_connect_application ist ein proprietäres iFi-Programm und meldet sich mit einem
# iFi-Gerätezertifikat bei Tidal an (Quelle: TonyTromp/tidal-connect-docker). Nicht für dieses
# Gerät lizenziert; kann jederzeit gesperrt werden. Auf ausdrücklichen Wunsch des Nutzers.
#
# Inhalt:
#   bin/tidal_connect_application   (sha256 geprüft)
#   cert/IfiAudio_ZenStream.dat
#   lib/   FFmpeg 3.4 minimal (tools/build-ffmpeg34.sh), libstdc++ 6.0.22, portaudio, FLAC++,
#          avahi-client/-common/-core, libdaemon, jack, opus (Debian stretch armhf);
#          libssl/libcrypto 1.0.1t + libcurl3 7.38 mit Abhängigkeiten (Debian jessie armhf,
#          wie im Docker-Image: das Programm braucht die Debian-Symbolversionen)
#   sbin/avahi-daemon (stretch) – dbus-daemon nimmt das Gerät selbst (1.10.6)
# Alles Übrige (glibc 2.23, libasound, libFLAC, libogg, libz, libgcrypt, …) kommt vom Gerät.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/build/tidal
TC_COMMIT=690b76ff8c6596f2e66b347b875544e5607ca645
TC_SHA=7e659a88c3c121e4efe35ba2852aec01eb38ba9f8ba30868ad6ca3e0d5b50930
rm -rf "$out"; mkdir -p "$out"/{bin,cert,lib,sbin,debs}
[ -f "$here/build/ffmpeg34/lib/libavcodec.so.57" ] || "$here/tools/build-ffmpeg34.sh"

raw=https://raw.githubusercontent.com/TonyTromp/tidal-connect-docker/$TC_COMMIT/Docker/src
curl -sfL -o "$out/bin/tidal_connect_application" "$raw/bin/tidal_connect_application"
echo "$TC_SHA  $out/bin/tidal_connect_application" | sha256sum -c --quiet
curl -sfL -o "$out/cert/IfiAudio_ZenStream.dat" "$raw/id_certificate/IfiAudio_ZenStream.dat"
chmod 755 "$out/bin/tidal_connect_application"

fetch(){ # fetch <image> <sources.list-Zeilen> <pakete...>
  local img=$1 src=$2; shift 2
  docker run --rm -v "$out/debs:/debs" "$img" bash -c "
    printf '%b' '$src' > /etc/apt/sources.list; rm -f /etc/apt/sources.list.d/*
    dpkg --add-architecture armhf; apt-get -o Acquire::Check-Valid-Until=false update >/dev/null 2>&1
    cd /debs; for p in $*; do apt-get -o Acquire::Check-Valid-Until=false download -q \$p:armhf >/dev/null || exit 1; done"
}
fetch debian/eol:stretch 'deb http://archive.debian.org/debian stretch main\n' \
  libstdc++6 libportaudio2 libflac++6v5 libavahi-client3 libavahi-common3 libavahi-core7 \
  libdaemon0 libjack-jackd2-0 libopus0 avahi-daemon
fetch debian/eol:jessie 'deb http://archive.debian.org/debian jessie main\ndeb http://archive.debian.org/debian-security jessie/updates main\n' \
  libssl1.0.0 libcurl3 libgnutls-deb0-28 libnettle4 libhogweed2 libgssapi-krb5-2 libk5crypto3 \
  libkrb5-3 libkrb5support0 libkeyutils1 libcomerr2 libldap-2.4-2 libsasl2-2 librtmp1 libssh2-1 \
  libp11-kit0 libtasn1-6
( cd "$out/debs" && sha256sum *.deb > SHA256SUMS )
x=$(mktemp -d); trap 'rm -rf "$x"' EXIT
for d in "$out"/debs/*.deb; do
  m=$(ar t "$d" | grep data.tar); case $m in *xz) ar p "$d" "$m" | tar xJ -C "$x";; *gz) ar p "$d" "$m" | tar xz -C "$x";; esac
done
libs="libstdc++.so.6 libportaudio.so.2 libFLAC++.so.6 libavahi-client.so.3 libavahi-common.so.3
libavahi-core.so.7 libdaemon.so.0 libjack.so.0 libopus.so.0 libssl.so.1.0.0 libcrypto.so.1.0.0
libcurl.so.4 libgnutls-deb0.so.28 libnettle.so.4 libhogweed.so.2 libgssapi_krb5.so.2 libk5crypto.so.3
libkrb5.so.3 libkrb5support.so.0 libkeyutils.so.1 libcom_err.so.2 libldap_r-2.4.so.2 liblber-2.4.so.2
libsasl2.so.2 librtmp.so.1 libssh2.so.1 libp11-kit.so.0 libtasn1.so.6"
for l in $libs; do
  p=$(find "$x/lib/arm-linux-gnueabihf" "$x/usr/lib/arm-linux-gnueabihf" -maxdepth 1 -name "$l" | head -1)
  [ -n "$p" ] || { echo "fehlt: $l" >&2; exit 1; }
  cp -L "$p" "$out/lib/$l"
done
cp -a "$here"/build/ffmpeg34/lib/*.so.* "$out/lib/"
cp "$x/usr/sbin/avahi-daemon" "$out/sbin/"
rm -rf "$out/debs"/*.deb
du -sh "$out"; ls "$out/lib"
