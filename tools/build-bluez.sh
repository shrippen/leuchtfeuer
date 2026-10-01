#!/usr/bin/env bash
# Baut BlueZ 5.50 (bluetoothd), bluez-alsa mit AAC (fdk-aac statisch; A2DP-Empfänger, bluealsa-aplay) für den
# Invoke: glibc 2.23 armhf (Xenial-Cross), libsbc (Xenial-Paket)/libbluetooth statisch, GLib/D-Bus/ALSA dynamisch
# aus dem Gerät. Pfade zur Laufzeit liegen unter /data/leuchtfeuer/bluez (Zustand der Kopplungen auf /data).
#   tools/build-bluez.sh   -> build/bluez/{bluetoothd,bluealsa,bluealsa-aplay,...}
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/build/bluez
mkdir -p "$out"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
docker image inspect invoke-xenial-armhf-bt >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf-bt -f "$here/tools/docker/xenial-armhf-bt.Dockerfile" "$here/tools/docker"
docker run --rm -v "$out:/out" invoke-xenial-armhf-bt bash -euc '
  H=arm-linux-gnueabihf; P=/opt/bt; R=/data/leuchtfeuer/bluez
  export PKG_CONFIG_LIBDIR=/usr/lib/$H/pkgconfig:/usr/share/pkgconfig:$P/lib/pkgconfig
  cd /tmp
  # AAC-Codec (fdk-aac), statisch: iPhones und viele Android-Geräte senden damit besser als mit SBC
  git clone -q --depth 1 --branch v2.0.3 https://github.com/mstorsjo/fdk-aac.git && (cd fdk-aac && autoreconf -fi >/dev/null 2>&1 \
     && ./configure -q --host=$H --prefix=$P --enable-static --disable-shared >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  # BlueZ
  wget -q https://www.kernel.org/pub/linux/bluetooth/bluez-5.50.tar.xz && tar xf bluez-5.50.tar.xz
  cd bluez-5.50
  ./configure -q --host=$H --prefix=$R --sysconfdir=$R/etc --localstatedir=$R/var --enable-static \
    --enable-library --enable-tools --enable-deprecated --disable-client --disable-systemd --disable-udev \
    --disable-cups --disable-obex --disable-manpages --disable-health --disable-sap --disable-nfc \
    --disable-hid --disable-hog --disable-network --disable-midi --disable-mesh --disable-test --disable-monitor >/dev/null
  make -s -j$(nproc) >/dev/null
  mkdir -p /tmp/bz && make -s install DESTDIR=/tmp/bz >/dev/null
  # libbluetooth für bluez-alsa
  mkdir -p $P/include $P/lib/pkgconfig && cp -r /tmp/bz$R/include/bluetooth $P/include/ && cp /tmp/bz$R/lib/libbluetooth.a $P/lib/
  cat > $P/lib/pkgconfig/bluez.pc <<PC
Name: BlueZ
Description: BlueZ Library
Version: 5.50
Libs: -L$P/lib -lbluetooth
Cflags: -I$P/include
PC
  cd /tmp
  git clone -q --branch v3.1.0 https://github.com/arkq/bluez-alsa.git && cd bluez-alsa
  git rev-parse HEAD > /out/bluez-alsa.commit
  autoreconf --install >/dev/null 2>&1
  ./configure -q --host=$H --prefix=$R --enable-aac --enable-aplay --disable-hcitop --disable-rfcomm --disable-manpages \
     --disable-payloadcheck --disable-debug --disable-ofono --disable-upower \
     PKG_CONFIG_PATH=$P/lib/pkgconfig SBC_CFLAGS=" " SBC_LIBS="-Wl,-Bstatic -lsbc -Wl,-Bdynamic" FDKAAC_CFLAGS="-I$P/include/fdk-aac" FDKAAC_LIBS="$P/lib/libfdk-aac.a -lm" >/dev/null
  make -s -j$(nproc) >/dev/null
  mkdir -p /tmp/ba && make -s install DESTDIR=/tmp/ba >/dev/null
  # Ergebnis einsammeln
  cp /tmp/bz$R/libexec/bluetooth/bluetoothd /out/ 2>/dev/null || cp /tmp/bz$R/lib/bluetooth/bluetoothd /out/ 2>/dev/null || find /tmp/bz -name bluetoothd -exec cp {} /out/ \;
  for t in hciconfig hcitool btmgmt l2ping sdptool; do find /tmp/bz -name $t -type f -exec cp {} /out/ \; ; done
  find /tmp/ba -type f \( -name bluealsa -o -name bluealsa-aplay \) -exec cp {} /out/ \;
  $H-strip /out/* 2>/dev/null || true
  for f in /out/bluetoothd /out/bluealsa /out/bluealsa-aplay; do echo "== $f"; $H-readelf -d $f | grep NEEDED; done > /out/needed.txt
'
# libsbc.so.1 (Xenial) zur Laufzeit auf dem Gerät: bluealsa bindet sie dynamisch
mkdir -p "$out/lib"
docker run --rm -v "$out/lib:/out" invoke-xenial-armhf-bt cp -L /usr/lib/arm-linux-gnueabihf/libsbc.so.1 /out/libsbc.so.1
ls -l "$out"; cat "$out/needed.txt"
