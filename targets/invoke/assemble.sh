# Teil von scripts/assemble.sh für den Invoke (Variablen S, d, t, TIDAL). Programme für ARMv7 aus ./build.sh.
# shellcheck shell=bash
mkdir -p "$S"/bluez/bin "$S"/bluez/lib
cp "$t/boot.sh" "$t/podium.conf" "$t/ca-certificates.crt" "$S/"
cp build/dropbear/dropbearmulti "$S/"
cp build/librespot/librespot build/gmrender/gmediarender build/sendspin/sendspin-player build/castrecv/castrecv \
   build/btagent/btagent build/leuchtfeuerd/leuchtfeuerd build/shairport/shairport-sync build/snapclient/snapclient "$S/bin/"
cp build/bluez/{bluetoothd,bluealsa,bluealsa-aplay,hciconfig,hcitool} "$S/bluez/bin/"
cp build/bluez/lib/libsbc.so.1 "$S/bluez/lib/"
# Pflicht für asound-music.conf: ohne die beiden Plugins gibt es keinen Ton
cp build/viztap/leuchtfeuer-viz-tap.so build/viztap/leuchtfeuer-eq.so "$S/lib/ladspa/"
chmod 755 "$S"/dropbearmulti "$S"/bluez/bin/*
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
  cp "$t/tidal/avahi-daemon.conf" "$t/tidal/dbus-system.conf" "$S/tidal/"
  find "$S/tidal" -name '*.so*' -exec chmod 755 {} +
  chmod 755 "$S"/tidal/bin/* "$S"/tidal/sbin/*
fi
