#!/usr/bin/env bash
# Stellt die Dateien für /data/leuchtfeuer aus build/ und device/leuchtfeuer/ zusammen (ohne Einstellungen, Schlüssel und die
# gerätebezogene BlueZ-Konfiguration). Genutzt von install.sh und tools/make-release.sh.
#   scripts/assemble.sh <Ziel> [--tidal]
# Ergebnis: <Ziel>/ mit bin/, lib/ladspa/, services/, bluez/{bin,lib}/, Systemdateien, VERSION und .remove
# (Dateien früherer Versionen, die auf dem Lautsprecher entfallen).
set -euo pipefail
cd "$(dirname "$0")/.."
S=${1:?Ziel fehlt}; TIDAL=0; [ "${2:-}" = --tidal ] && TIDAL=1
d=device/leuchtfeuer
mkdir -p "$S"/{bin,services,lib/ladspa,bluez/bin,bluez/lib}
cp "$d/boot.sh" "$d/hook.sh" "$d/migrate.sh" "$d/apply-update.sh" "$d/podium.conf" "$d/asound-music.conf" "$d/ca-certificates.crt" "$S/"
cp build/dropbear/dropbearmulti "$S/"
cp build/librespot/librespot build/gmrender/gmediarender build/sendspin/sendspin-player build/castrecv/castrecv \
   build/btagent/btagent build/leuchtfeuerd/leuchtfeuerd build/shairport/shairport-sync build/snapclient/snapclient "$S/bin/"
cp build/bluez/{bluetoothd,bluealsa,bluealsa-aplay,hciconfig,hcitool} "$S/bluez/bin/"
cp build/bluez/lib/libsbc.so.1 "$S/bluez/lib/"
# Pflicht für asound-music.conf: ohne die beiden Plugins gibt es keinen Ton
cp build/viztap/leuchtfeuer-viz-tap.so build/viztap/leuchtfeuer-eq.so "$S/lib/ladspa/"
for f in "$d"/services/*.sh; do
  case $(basename "$f") in tidal-*) [ $TIDAL = 1 ] || continue ;; esac
  cp "$f" "$S/services/"
done
if [ $TIDAL = 1 ]; then
  mkdir -p "$S/tidal"
  cp -a build/tidal/bin build/tidal/cert build/tidal/lib build/tidal/sbin "$S/tidal/"
  cp build/shim/avahi-user-shim.so "$S/tidal/lib/"
  cp "$d/tidal/avahi-daemon.conf" "$d/tidal/dbus-system.conf" "$S/tidal/"
fi
git describe --tags --always --dirty 2>/dev/null > "$S/VERSION" || echo dev > "$S/VERSION"
# entfallene Dateien älterer Versionen (volume-sync: jetzt in leuchtfeuerd; bis Oktober 2026 hieß alles "invoke")
printf '%s\n' services/volume-sync.sh services/invoked.sh bin/invoked lib/ladspa/invoke-viz-tap.so lib/ladspa/invoke-eq.so > "$S/.remove"
chmod 755 "$S"/boot.sh "$S"/hook.sh "$S"/migrate.sh "$S"/apply-update.sh "$S"/dropbearmulti "$S"/bin/* "$S"/bluez/bin/* "$S"/services/*.sh
if [ $TIDAL = 1 ]; then
  find "$S/tidal" -name '*.so*' -exec chmod 755 {} +
  chmod 755 "$S"/tidal/bin/* "$S"/tidal/sbin/*
fi
