#!/usr/bin/env bash
# Kopiert das gebaute Kante-Designsystem (shrippen.css, shrippen.js, fonts.css, fonts/) in die Weboberfläche von
# leuchtfeuerd (src/leuchtfeuerd/web/kante). Nie von Hand bearbeiten: Änderungen gehören nach Kante
# (Repo Kante auf git.arianw.de, dort ./build.sh), danach dieses Skript.
#   tools/sync-kante.sh [Pfad zum Kante-Repo]    (Standard: ../Kante, sonst ../shrippen.github.io, sonst Download)
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/src/leuchtfeuerd/web/kante
src=${1:-$here/../Kante}/docs/v1
[ -f "$src/shrippen.css" ] || [ -n "${1:-}" ] || src=$here/../shrippen.github.io/docs/v1
mkdir -p "$out/fonts"
if [ -f "$src/shrippen.css" ]; then
  cp "$src/shrippen.css" "$src/shrippen.js" "$src/fonts.css" "$out/"
  cp "$src"/fonts/*.ttf "$src/fonts/OFL.txt" "$out/fonts/"
else
  base=https://shrippen.github.io/v1
  for f in shrippen.css shrippen.js fonts.css; do curl -fsSL "$base/$f" -o "$out/$f"; done
  for f in JetBrainsMono-400.ttf JetBrainsMono-500.ttf Rajdhani-500.ttf Rajdhani-600.ttf Rajdhani-700.ttf OFL.txt; do
    curl -fsSL "$base/fonts/$f" -o "$out/fonts/$f"; done
fi
du -sh "$out"; head -c 80 "$out/shrippen.css"; echo
