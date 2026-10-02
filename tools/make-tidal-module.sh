#!/usr/bin/env bash
# Baut das Tidal-Connect-Modul für das Zielgerät "generic" (ARM: armv7, arm64 mit armhf-Multiarch):
#   tools/make-tidal-module.sh     -> dist/leuchtfeuer-tidal-<Version>-armhf.tar.gz (+ .sha256)
# Installieren: sudo sh /opt/leuchtfeuer/setup.sh --module leuchtfeuer-tidal-*.tar.gz; entfernen: --remove-module tidal.
# Nur für die eigene Nutzung: tidal_connect_application ist proprietär (iFi) und nicht weiterzugeben; das Modul gehört
# nie in ein Release und nie auf eine öffentliche Seite.
# Inhalt: MODULE, bin/tidal-connect, services/tidal-connect.sh (targets/generic/modules/tidal) und tidal/ aus
# tools/build-tidal-bundle.sh generic.
set -euo pipefail
cd "$(dirname "$0")/.."
[ -x build/tidal-generic/bin/tidal_connect_application ] || tools/build-tidal-bundle.sh generic
ver=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
S=$(mktemp -d); trap 'rm -rf "$S"' EXIT
cp -a targets/generic/modules/tidal/. "$S/"
mkdir -p "$S/tidal" && cp -a build/tidal-generic/bin build/tidal-generic/cert build/tidal-generic/lib build/tidal-generic/SYSTEM-LIBS "$S/tidal/"
printf 'name=tidal\nversion=%s\narch=armhf\ncheck=bin/tidal-connect --check\n' "$ver" > "$S/MODULE"
find "$S" -type l | grep -q . && { echo "Symlinks im Modul" >&2; exit 1; }
mkdir -p dist
pkg=dist/leuchtfeuer-tidal-$ver-armhf.tar.gz
tar -C "$S" --owner=0 --group=0 -czf "$pkg" .
(cd dist && sha256sum "$(basename "$pkg")" > "$(basename "$pkg").sha256")
ls -la "$pkg"
