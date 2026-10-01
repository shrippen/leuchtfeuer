#!/usr/bin/env bash
# Startet die Weboberfläche von invoked mit den gemeinsamen Demodaten (Studio Weber) – intern, nur für Screenshots.
#   demo/start.sh [de|en]        Port: $PORT (Standard 8099), Adresse http://127.0.0.1:<Port>/
# Baut dafür ein eigenes Programm mit dem Build-Tag "demo" nach build-demo/ (nie Teil eines Release-Builds).
# Der Demo-Modus braucht kein Gerät: Wiedergabe, Wecker, Timer und Messwerte sind nachgebildet, nichts greift auf echte
# Server, Konten oder Dateien zu. DEMO_TODAY=YYYY-MM-DD fixiert das Datum (sonst demo/world.json screenshot_today).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
lang=${1:-en}
case $lang in de|en) ;; *) echo "Sprache: de | en" >&2; exit 2 ;; esac
cd "$ROOT/src/invoked"
go build -tags demo -o "$ROOT/build-demo/invoked-demo" .
cd "$ROOT"
export DEMO_LANG=$lang
echo "http://127.0.0.1:${PORT:-8099}/"
exec "$ROOT/build-demo/invoked-demo" -demo "$ROOT/demo/world.json" -listen "127.0.0.1:${PORT:-8099}"
