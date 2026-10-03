# Teil von scripts/assemble.sh für den Invoke (Variablen S, d, t, TIDAL). Programme für ARMv7 aus ./build.sh.
# shellcheck shell=bash
mkdir -p "$S"/bluez/bin "$S"/bluez/lib
cp "$t/boot.sh" "$t/podium.conf" "$t/ca-certificates.crt" "$t/dbus-system.conf" "$S/"
cp build/dropbear/dropbearmulti "$S/"
cp build/librespot/librespot build/gmrender/gmediarender build/sendspin/sendspin-player build/castrecv/castrecv \
   build/btagent/btagent build/leuchtfeuerd/leuchtfeuerd build/shairport/shairport-sync build/snapclient/snapclient "$S/bin/"
cp build/bluez/{bluetoothd,bluealsa,bluealsa-aplay,hciconfig,hcitool} "$S/bluez/bin/"
cp build/bluez/lib/libsbc.so.1 "$S/bluez/lib/"
# ALSA-Plugin für die Ausgabe an Bluetooth-Lautsprecher (fehlt bei älteren Bauten: dann geht nur der Empfang)
if [ -f build/bluez/libasound_module_pcm_bluealsa.so ]; then cp build/bluez/libasound_module_pcm_bluealsa.so "$S/bluez/lib/"
else echo "Hinweis: build/bluez/libasound_module_pcm_bluealsa.so fehlt (tools/build-bluez.sh neu laufen lassen): keine Ausgabe an Bluetooth-Lautsprecher" >&2; fi
# Pflicht für asound-music.conf: ohne die beiden Plugins gibt es keinen Ton
cp build/viztap/leuchtfeuer-viz-tap.so build/viztap/leuchtfeuer-eq.so "$S/lib/ladspa/"
chmod 755 "$S"/dropbearmulti "$S"/bluez/bin/*
# Tidal-Bibliotheken früherer Bündel (curl 7.38 aus Jessie mit Abhängigkeiten, OpenSSL 1.0.1, FFmpeg unter Vollnamen):
# entfallen, seit curl aktuell mit mbedTLS und OpenSSL 1.0.2u gebaut wird (tools/build-tidal-bundle.sh)
for l in libssl.so.1.0.0 libcrypto.so.1.0.0 libgnutls-deb0.so.28 libnettle.so.4 libhogweed.so.2 libgssapi_krb5.so.2 \
  libk5crypto.so.3 libkrb5.so.3 libkrb5support.so.0 libkeyutils.so.1 libcom_err.so.2 libldap_r-2.4.so.2 liblber-2.4.so.2 \
  libsasl2.so.2 librtmp.so.1 libssh2.so.1 libp11-kit.so.0 libtasn1.so.6 libavcodec.so.57.107.100 \
  libavformat.so.57.83.100 libavutil.so.55.78.100 libswresample.so.2.9.100; do echo "tidal/lib/$l"; done >> "$S/.remove"
# D-Bus war bis Oktober 2026 Teil des Tidal-Moduls (jetzt services/dbus.sh, für Bluetooth auch ohne Tidal)
printf '%s\n' services/tidal-1-dbus.sh tidal/dbus-system.conf >> "$S/.remove"
if [ "$TIDAL" = 1 ]; then
  mkdir -p "$S/tidal"
  cp -a build/tidal/bin build/tidal/cert build/tidal/lib build/tidal/sbin "$S/tidal/"
  # Symlinks (libavformat.so.57 -> libavformat.so.57.83.100) durch die Datei selbst ersetzen: Installer und Updates
  # übertragen nur reguläre Dateien, und das Programm braucht nur den SONAME-Namen. Die Ziele entfallen danach.
  links=$(find "$S/tidal" -type l)
  targets=$(for l in $links; do readlink -f "$l"; done | sort -u)
  for l in $links; do cp --remove-destination "$(readlink -f "$l")" "$l"; done
  for f in $targets; do rm -f "$f"; done
  cp build/shim/avahi-user-shim.so "$S/tidal/lib/"
  cp "$t/tidal/avahi-daemon.conf" "$S/tidal/"
  find "$S/tidal" -name '*.so*' -exec chmod 755 {} +
  chmod 755 "$S"/tidal/bin/* "$S"/tidal/sbin/*
fi
