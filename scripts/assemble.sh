#!/usr/bin/env bash
# Stellt die Dateien des Installationsverzeichnisses (Invoke: /data/leuchtfeuer) aus build/, dem gemeinsamen Teil
# device/leuchtfeuer/ und dem Teil des Zielgeräts targets/<ziel>/ zusammen (ohne Einstellungen und Schlüssel).
# Genutzt von install.sh, tools/make-release.sh und targets/generic (Paket für beliebiges Linux).
#   scripts/assemble.sh <Ziel-Verzeichnis> [--target invoke|generic] [--arch amd64|arm64|armv7] [--tidal]
# Ergebnis: <Ziel-Verzeichnis>/ mit hook.sh, target.sh, asound-music.conf, bin/, lib/ladspa/, services/, VERSION und
# .remove (Dateien früherer Versionen, die entfallen). Gerätespezifisches legt targets/<ziel>/assemble.sh dazu.
set -euo pipefail
cd "$(dirname "$0")/.."
S=${1:?Ziel-Verzeichnis fehlt}; shift
TARGET=invoke TIDAL=0 ARCH=""
while [ $# -gt 0 ]; do
  case $1 in
    --target) TARGET=${2:?}; shift 2 ;;
    --arch) ARCH=${2:?}; shift 2 ;;
    --tidal) TIDAL=1; shift ;;
    *) echo "unbekannte Option: $1" >&2; exit 2 ;;
  esac
done
d=device/leuchtfeuer t=targets/$TARGET
[ -f "$t/target.env" ] || { echo "Zielgerät $TARGET unbekannt (targets/*)" >&2; exit 2; }
DIR="" OUT="" CARD=""
# shellcheck disable=SC1090
. "$t/target.env"
mkdir -p "$S"/{bin,services,lib/ladspa}
cp "$d/hook.sh" "$d/migrate.sh" "$d/apply-update.sh" "$t/target.sh" "$S/"
# Tonkette: Teil des Zielgeräts, dann der gemeinsame; Platzhalter aus target.env
{ cat "$t/asound-target.conf"; echo; cat "$d/asound-music.conf"; } |
  sed -e "s|@LEUCHTFEUER_DIR@|$DIR|g" -e "s|@OUT@|$OUT|g" -e "s|@CARD@|$CARD|g" > "$S/asound-music.conf"
for f in "$d"/services/*.sh "$t"/services/*.sh; do
  [ -f "$f" ] || continue
  case $(basename "$f") in tidal-*) [ $TIDAL = 1 ] || continue ;; esac
  cp "$f" "$S/services/"
done
# Gerätespezifisches (Programme, Systemdateien)
# shellcheck disable=SC1090
. "$t/assemble.sh"
git describe --tags --always --dirty 2>/dev/null > "$S/VERSION" || echo dev > "$S/VERSION"
# entfallene Dateien älterer Versionen (volume-sync: jetzt in leuchtfeuerd; bis Oktober 2026 hieß alles "invoke")
printf '%s\n' services/volume-sync.sh services/invoked.sh bin/invoked lib/ladspa/invoke-viz-tap.so lib/ladspa/invoke-eq.so > "$S/.remove"
chmod 755 "$S"/*.sh "$S"/bin/* "$S"/services/*.sh
