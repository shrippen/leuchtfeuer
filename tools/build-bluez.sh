#!/usr/bin/env bash
# Baut BlueZ (bluetoothd), bluez-alsa mit AAC (fdk-aac statisch; A2DP-Empfänger, bluealsa-aplay) für den
# Invoke: glibc 2.23 armhf (Xenial-Cross), libsbc (Xenial-Paket)/libbluetooth statisch, GLib/D-Bus/ALSA dynamisch
# aus dem Gerät. Pfade zur Laufzeit liegen unter /data/leuchtfeuer/bluez (Zustand der Kopplungen auf /data).
#   tools/build-bluez.sh   -> build/bluez/{bluetoothd,bluealsa,bluealsa-aplay,...}
#
# BlueZ ist der wichtigste Posten für den Sicherheitsstand des Geräts: bluetoothd nimmt über Funk Verbindungen
# entgegen und parst SDP/A2DP/AVRCP fremder Geräte. Darum die neueste 5.x-Fassung; die Prüfsumme unten ist gegen
# die GPG-Signatur von Marcel Holtmann auf kernel.org geprüft (bluez-<Version>.tar.sign).
#
# bluez-alsa bleibt bei 3.1.0 (letzte Fassung der 3er-Linie). 4.x/5.x verlangen GLib >= 2.58.2 und sbc >= 1.5;
# der Invoke hat GLib 2.46.2 fest im System, Xenial liefert sbc 1.3. Ein eigenes GLib mitzubringen wäre groß
# (GLib samt libffi/PCRE) und widerspräche dem Grundsatz, Bibliotheken des Systems mitzunutzen. Auf dem Zielgerät
# "generic" kommt bluez-alsa ohnehin aus der Distribution und ist dort aktuell.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
. "$here/tools/docker-run.sh"
out=$here/build/bluez
BLUEZ_VER=5.87
BLUEZ_SHA=26bdcf2cebd7310c6f598850606b037ef0c515fe6608ebc54d22c50c4c32b35f
BA_TAG=v3.1.0
mkdir -p "$out"
docker image inspect invoke-xenial-armhf >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf -f "$here/tools/docker/xenial-armhf.Dockerfile" "$here/tools/docker"
docker image inspect invoke-xenial-armhf-bt >/dev/null 2>&1 || \
  docker build -q -t invoke-xenial-armhf-bt -f "$here/tools/docker/xenial-armhf-bt.Dockerfile" "$here/tools/docker"
drun -v "$out:/out" -e BLUEZ_VER=$BLUEZ_VER -e BLUEZ_SHA=$BLUEZ_SHA -e BA_TAG=$BA_TAG \
  invoke-xenial-armhf-bt bash -euc '
  H=arm-linux-gnueabihf; P=/opt/bt; R=/data/leuchtfeuer/bluez
  export PKG_CONFIG_LIBDIR=/usr/lib/$H/pkgconfig:/usr/share/pkgconfig:$P/lib/pkgconfig
  cd /tmp
  # AAC-Codec (fdk-aac), statisch: iPhones und viele Android-Geräte senden damit besser als mit SBC
  git clone -q --depth 1 --branch v2.0.3 https://github.com/mstorsjo/fdk-aac.git && (cd fdk-aac && autoreconf -fi >/dev/null 2>&1 \
     && ./configure -q --host=$H --prefix=$P --enable-static --disable-shared >/dev/null && make -s -j$(nproc) >/dev/null && make -s install >/dev/null)
  # BlueZ (--disable-health/--disable-sap gibt es seit 5.5x nicht mehr: die Plugins sind entfallen)
  wget -q https://www.kernel.org/pub/linux/bluetooth/bluez-$BLUEZ_VER.tar.xz
  echo "$BLUEZ_SHA  bluez-$BLUEZ_VER.tar.xz" | sha256sum -c --quiet
  tar xf bluez-$BLUEZ_VER.tar.xz
  cd bluez-$BLUEZ_VER
  ./configure -q --host=$H --prefix=$R --sysconfdir=$R/etc --localstatedir=$R/var --enable-static \
    --enable-library --enable-tools --enable-deprecated --disable-client --disable-systemd --disable-udev \
    --disable-cups --disable-obex --disable-manpages --disable-nfc \
    --disable-hid --disable-hog --disable-network --disable-midi --disable-mesh --disable-test --disable-monitor >/dev/null
  make -s -j$(nproc) >/dev/null
  mkdir -p /tmp/bz && make -s install DESTDIR=/tmp/bz >/dev/null
  # libbluetooth für bluez-alsa
  mkdir -p $P/include $P/lib/pkgconfig && cp -r /tmp/bz$R/include/bluetooth $P/include/ && cp /tmp/bz$R/lib/libbluetooth.a $P/lib/
  cat > $P/lib/pkgconfig/bluez.pc <<PC
Name: BlueZ
Description: BlueZ Library
Version: $BLUEZ_VER
Libs: -L$P/lib -lbluetooth
Cflags: -I$P/include
PC
  cd /tmp
  git clone -q --branch "$BA_TAG" https://github.com/arkq/bluez-alsa.git && cd bluez-alsa
  git rev-parse HEAD > /out/bluez-alsa.commit
  autoreconf --install >/dev/null 2>&1
  ./configure -q --host=$H --prefix=$R --enable-aac --enable-aplay --disable-hcitop --disable-rfcomm --disable-manpages \
     --disable-payloadcheck --disable-debug --disable-ofono --disable-upower \
     PKG_CONFIG_PATH=$P/lib/pkgconfig SBC_CFLAGS=" " SBC_LIBS="-Wl,-Bstatic -lsbc -Wl,-Bdynamic" FDKAAC_CFLAGS="-I$P/include/fdk-aac" FDKAAC_LIBS="$P/lib/libfdk-aac.a -lm" >/dev/null
  make -s -j$(nproc) >/dev/null
  mkdir -p /tmp/ba && make -s install DESTDIR=/tmp/ba >/dev/null
  # Ergebnis einsammeln
  cp /tmp/bz$R/libexec/bluetooth/bluetoothd /out/ 2>/dev/null || cp /tmp/bz$R/lib/bluetooth/bluetoothd /out/ 2>/dev/null || find /tmp/bz -name bluetoothd -exec cp {} /out/ \;
  # btmgmt entfällt: es braucht die interaktive Shell von BlueZ (readline), die --disable-client abschaltet.
  # Im Projekt wird es nirgends benutzt; zum Nachsehen reichen hciconfig und hcitool.
  for t in hciconfig hcitool l2ping sdptool; do find /tmp/bz -name $t -type f -exec cp {} /out/ \; ; done
  find /tmp/ba -type f \( -name bluealsa -o -name bluealsa-aplay \) -exec cp {} /out/ \;
  # ALSA-Plugin "bluealsa": Ausgabe an Bluetooth-Lautsprecher (A2DP-Quelle), siehe asound-target.conf
  find /tmp/ba -type f -name libasound_module_pcm_bluealsa.so -exec cp {} /out/ \;
  $H-strip /out/* 2>/dev/null || true
  for f in /out/bluetoothd /out/bluealsa /out/bluealsa-aplay; do echo "== $f"; $H-readelf -d $f | grep NEEDED; done > /out/needed.txt
  # Undefinierte Symbole festhalten: das Gerät hat GLib 2.46.2, der Bau-Container 2.48. Ruft BlueZ etwas Neueres
  # auf, fehlt es erst auf dem Gerät. tests/bluez_symbols_test.sh prüft die Liste gegen die Bibliotheken des Geräts.
  for f in /out/bluetoothd /out/bluealsa /out/bluealsa-aplay; do $H-objdump -T $f | awk "/UND/{print \$NF}"; done | sort -u > /out/undefined.txt
'
# libsbc.so.1 (Xenial) zur Laufzeit auf dem Gerät: bluealsa bindet sie dynamisch
mkdir -p "$out/lib"
drun -v "$out/lib:/out" invoke-xenial-armhf-bt cp -L /usr/lib/arm-linux-gnueabihf/libsbc.so.1 /out/libsbc.so.1
ls -l "$out"; cat "$out/needed.txt"
