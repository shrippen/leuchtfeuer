#!/usr/bin/env bash
# Baut ein Release-Paket für Updates aus der Weboberfläche und install.sh --prebuilt:
#   dist/leuchtfeuer-<Version>-invoke.tar.gz (+ .sha256, + .sig wenn ein Signierschlüssel da ist)
# Ohne Tidal (proprietär, nicht weiterzugeben). Vorher ./build.sh --no-tidal.
#   tools/make-release.sh [--key <geheimer-schlüssel>]   Schlüssel auch über LEUCHTFEUER_SIGNING_KEY (Inhalt, Base64)
# Schlüssel erzeugen: (cd src/relsign && go run . keygen ~/.config/leuchtfeuer/release.key); den ausgegebenen
# öffentlichen Schlüssel in docs/release-key.pub eintragen.
set -euo pipefail
cd "$(dirname "$0")/.."
KEY=""
[ "${1:-}" = --key ] && KEY=${2:?}
ver=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT
scripts/assemble.sh "$stage/invoke"
mkdir -p dist
pkg=dist/leuchtfeuer-$ver-invoke.tar.gz
tar -C "$stage/invoke" --owner=0 --group=0 --sort=name -czf "$pkg" .
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
