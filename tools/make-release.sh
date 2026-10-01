#!/usr/bin/env bash
# Baut ein Release-Paket für Updates aus der Weboberfläche und install.sh --prebuilt:
#   dist/leuchtfeuer-<Version>-<Paket>.tar.gz (+ .sha256, + .sig wenn ein Signierschlüssel da ist)
# Paket je Zielgerät: "invoke" (vorher ./build.sh --no-tidal; ohne Tidal, proprietär, nicht weiterzugeben) oder
# "generic-<arch>" (vorher tools/build-generic.sh <arch>).
#   tools/make-release.sh [--target invoke|generic] [--arch amd64|arm64|armv7] [--key <geheimer-schlüssel>]
# Schlüssel auch über LEUCHTFEUER_SIGNING_KEY (Inhalt, Base64)
# Schlüssel erzeugen: (cd src/relsign && go run . keygen ~/.config/leuchtfeuer/release.key); den ausgegebenen
# öffentlichen Schlüssel in docs/release-key.pub eintragen.
set -euo pipefail
cd "$(dirname "$0")/.."
KEY="" TARGET=invoke ARCH=""
while [ $# -gt 0 ]; do
  case $1 in
    --key) KEY=${2:?}; shift 2 ;;
    --target) TARGET=${2:?}; shift 2 ;;
    --arch) ARCH=${2:?}; shift 2 ;;
    *) echo "unbekannte Option: $1" >&2; exit 2 ;;
  esac
done
ver=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
if [ "$TARGET" = generic ]; then
  scripts/assemble.sh "$stage/data" --target generic ${ARCH:+--arch "$ARCH"}
  name=generic-$(cat "$stage/data/ARCH")
else
  scripts/assemble.sh "$stage/data" --target "$TARGET"
  name=$TARGET
fi
mkdir -p dist
pkg=dist/leuchtfeuer-$ver-$name.tar.gz
tar -C "$stage/data" --owner=0 --group=0 --sort=name -czf "$pkg" .
(cd dist && sha256sum "$(basename "$pkg")" > "$(basename "$pkg").sha256")
if [ -z "$KEY" ] && [ -n "${LEUCHTFEUER_SIGNING_KEY:-}" ]; then
  KEY=$stage/key; printf '%s\n' "$LEUCHTFEUER_SIGNING_KEY" > "$KEY"; chmod 600 "$KEY"
fi
if [ -n "$KEY" ]; then
  (cd src/relsign && go run . sign "$KEY" "$OLDPWD/$pkg")
  pub=$(grep -v "^#" docs/release-key.pub | head -n 1 || true)
  if [ -n "$pub" ]; then (cd src/relsign && go run . verify "$pub" "$OLDPWD/$pkg"); fi
else
  echo "Hinweis: ohne Signierschlüssel kein .sig (Updates aus der Weboberfläche brauchen es)" >&2
fi
ls -la dist/
