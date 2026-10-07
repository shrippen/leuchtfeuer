#!/usr/bin/env bash
# Baut ein statisches dropbear (armv7, musl) für den Invoke. Das mitgelieferte
# dropbear 2016.72 kann kein ed25519. Toolchain: tools/docker/armv7-musl.Dockerfile.
#   tools/build-dropbear.sh [tag]   -> build/dropbear/dropbearmulti
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
. "$here/tools/docker-run.sh"
tag=${1:-DROPBEAR_2026.94}
out=$here/build/dropbear
mkdir -p "$out"
docker image inspect invoke-armv7-musl >/dev/null 2>&1 || \
  docker build -q -t invoke-armv7-musl -f "$here/tools/docker/armv7-musl.Dockerfile" "$here/tools/docker"
drun -v "$out:/out" -e TAG="$tag" invoke-armv7-musl sh -euc '
  cd /tmp
  wget -q -O db.tar.gz "https://github.com/mkj/dropbear/archive/refs/tags/$TAG.tar.gz"
  sha256sum db.tar.gz > /out/source.sha256
  tar xzf db.tar.gz && cd dropbear-*
  cat > localoptions.h <<EOF
#define DROPBEAR_X11FWD 0
#define DROPBEAR_SVR_PASSWORD_AUTH 0
#define DROPBEAR_SVR_PAM_AUTH 0
#define DROPBEAR_SVR_PUBKEY_AUTH 1
#define DROPBEAR_ED25519 1
#define DROPBEAR_ECDSA 1
#define DROPBEAR_RSA 1
#define DEFAULT_PATH "/usr/bin:/bin:/usr/sbin:/sbin:/lsync/opt/bin"
#define DEFAULT_ROOT_PATH "/usr/sbin:/usr/bin:/sbin:/bin:/lsync/opt/bin"
#define SFTPSERVER_PATH "/lsync/opt/libexec/sftp-server"
EOF
  CC=gcc AR=ar RANLIB=ranlib LDFLAGS="-static -no-pie" CFLAGS="-Os -fno-pie" ./configure --host=armv7l-linux-musleabihf \
     --disable-zlib --disable-lastlog --disable-utmp --disable-utmpx --disable-wtmp --disable-wtmpx \
     --disable-pututline --disable-pututxline --enable-static >/dev/null
  make -j"$(nproc)" PROGRAMS="dropbear dropbearkey scp" MULTI=1 STATIC=1 >/dev/null
  strip dropbearmulti
  cp dropbearmulti /out/
'
file "$out/dropbearmulti"
cat "$out/source.sha256"
