#!/usr/bin/env bash
# Minimales FFmpeg 3.4 (Sonamen libavcodec.so.57, libavformat.so.57, libavutil.so.55,
# libswresample.so.2) für tidal_connect_application: nur Audio-Decoder/Demuxer, gegen glibc 2.23.
# Ohne Netzwerk und ohne TLS: Tidal holt die Daten selbst (curl) und reicht sie über avio_alloc_context durch
# (avformat_network_init wird nie aufgerufen). So braucht FFmpeg 3.4 (die letzte Version der Linie, die die Sonamen 57
# liefert) kein OpenSSL und hat keine Angriffsfläche im Netz.
#   tools/build-ffmpeg34.sh   -> build/ffmpeg34/lib/*.so.*
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/build/ffmpeg34
mkdir -p "$out"
docker run --rm -v "$out:/out" invoke-xenial-armhf bash -euc '
  cd /tmp && wget -q https://ffmpeg.org/releases/ffmpeg-3.4.13.tar.xz && sha256sum ffmpeg-3.4.13.tar.xz > /out/source.sha256
  tar xJf ffmpeg-3.4.13.tar.xz && cd ffmpeg-3.4.13
  ./configure --enable-cross-compile --cross-prefix=arm-linux-gnueabihf- --arch=arm --cpu=cortex-a9 --target-os=linux \
    --enable-neon --enable-shared --disable-static --disable-programs --disable-doc \
    --disable-avdevice --disable-swscale --disable-postproc --disable-avfilter \
    --disable-everything --disable-network --disable-openssl \
    --enable-decoder=aac,aac_latm,aac_fixed,flac,alac,mp3,mp3float,opus,vorbis,pcm_s16le,pcm_s16be,pcm_s24le,pcm_s24be,pcm_s32le,pcm_f32le \
    --enable-demuxer=mov,flac,mp3,aac,ogg,wav,matroska,mpegts,data \
    --enable-parser=aac,aac_latm,flac,mpegaudio,opus,vorbis \
    --enable-bsf=aac_adtstoasc \
    --enable-protocol=file,data,pipe \
    --prefix=/opt/ff >/out/configure.log
  make -s -j$(nproc) >/dev/null 2>&1 && make -s install >/dev/null
  mkdir -p /out/lib && cp -a /opt/ff/lib/lib*.so.* /out/lib/
  arm-linux-gnueabihf-strip /out/lib/*.so.*.*
'
ls -la "$out/lib"
