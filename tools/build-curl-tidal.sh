#!/usr/bin/env bash
# Baut eine schlanke, aktuelle libcurl.so.4 für Tidal Connect (armhf): curl (aktuell) mit mbedTLS (LTS, statisch
# eingebaut), nur HTTP(S). tidal_connect_application verlangt die Symbolversion CURL_OPENSSL_3 (Debians libcurl3); die
# Schnittstelle von libcurl ist seit 7.16 stabil, deshalb erfüllt eine aktuelle curl sie, wenn der Versionsname passt.
# Das Programm übergibt curl keine OpenSSL-Objekte (keine CURLOPT_SSL_CTX_FUNCTION, keine Client-Zertifikate; geprüft
# an allen curl_easy_setopt-Aufrufen), das TLS von curl darf also mbedTLS sein. Ersetzt libcurl3 aus Jessie (curl 7.38
# von 2014) und 16 Bibliotheken, die es mitbrachte (~2,7 MB).
#   build/curl-tidal/libcurl.so.4      (glibc 2.23, läuft auf dem Invoke und jedem neueren armhf-Linux)
# Gebraucht von tools/build-tidal-bundle.sh. CA: CURLOPT_CAPATH /etc/ssl/certs (setzt Tidal selbst), Vorgabe-Bündel
# /etc/ssl/certs/ca-certificates.crt. Neue Versionen: CURL_VER/CURL_SHA und MBED_VER/MBED_SHA anpassen (Signatur bzw.
# Release-Prüfsumme kontrollieren).
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/build/curl-tidal
CURL_VER=8.22.0
CURL_SHA=f7ef3ae8a22e521f289803fe93543eb64c329b58aa73a9e224dfd915a2a5f4f7   # GPG-Signatur Daniel Stenberg geprüft
MBED_VER=3.6.7
MBED_SHA=a7e8bcbec0e6f761b4af24f25677626b35f762f68eef79c08677a363212d11f6   # = Prüfsumme im GitHub-Release
mkdir -p "$out"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" -e CURL_VER=$CURL_VER -e CURL_SHA=$CURL_SHA -e MBED_VER=$MBED_VER -e MBED_SHA=$MBED_SHA \
  invoke-xenial-armhf bash -euc '
  H=arm-linux-gnueabihf; M=/tmp/mbed; cd /tmp
  wget -q https://github.com/Mbed-TLS/mbedtls/releases/download/mbedtls-$MBED_VER/mbedtls-$MBED_VER.tar.bz2
  echo "$MBED_SHA  mbedtls-$MBED_VER.tar.bz2" | sha256sum -c --quiet
  tar xjf mbedtls-$MBED_VER.tar.bz2 && cd mbedtls-$MBED_VER
  # Ohne getrandom (Kernel < 3.17, z. B. der Invoke mit 3.8) liest mbedTLS sonst /dev/random und blockiert bei wenig
  # Entropie minutenlang; /dev/urandom ist nach dem Start gleichwertig (so macht es auch getrandom)
  make -s -C library CC=$H-gcc AR=$H-ar CFLAGS="-O2 -fPIC -DMBEDTLS_PLATFORM_DEV_RANDOM=\\\"/dev/urandom\\\"" static >/dev/null
  mkdir -p $M/lib && cp library/*.a $M/lib/ && cp -r include $M/
  cd /tmp && wget -q https://curl.se/download/curl-$CURL_VER.tar.xz
  echo "$CURL_SHA  curl-$CURL_VER.tar.xz" | sha256sum -c --quiet
  tar xJf curl-$CURL_VER.tar.xz && cd curl-$CURL_VER
  ./configure -q --host=$H --prefix=/usr --enable-shared --disable-static --enable-versioned-symbols \
    --with-mbedtls=$M --without-openssl --without-gnutls --without-wolfssl --without-rustls \
    --without-libpsl --without-libidn2 --without-nghttp2 --without-brotli --without-zstd --without-libssh2 \
    --disable-ldap --disable-ldaps --disable-dict --disable-telnet --disable-tftp --disable-pop3 --disable-imap \
    --disable-smtp --disable-gopher --disable-rtsp --disable-mqtt --disable-ftp --disable-file --disable-websockets \
    --disable-manual --disable-docs --without-ca-fallback \
    --with-ca-bundle=/etc/ssl/certs/ca-certificates.crt --with-ca-path=/etc/ssl/certs --with-zlib >/dev/null
  # Versionsname wie Debians libcurl3, den tidal_connect_application verlangt (sonst CURL_MBEDTLS_4)
  sed -i "1s/.*/CURL_OPENSSL_3/" lib/libcurl.vers
  make -s -j$(nproc) -C lib >/dev/null
  cp -L lib/.libs/libcurl.so.4 /out/libcurl.so.4
  $H-strip /out/libcurl.so.4
  $H-readelf -V /out/libcurl.so.4 | grep -q "Name: CURL_OPENSSL_3" || { echo "libcurl ohne CURL_OPENSSL_3" >&2; exit 1; }
  { echo "curl $CURL_VER, mbedTLS $MBED_VER"; $H-readelf -d /out/libcurl.so.4 | grep NEEDED; } > /out/needed.txt
'
ls -la "$out/libcurl.so.4"; cat "$out/needed.txt"
