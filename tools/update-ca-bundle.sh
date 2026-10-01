#!/usr/bin/env bash
# Lädt den aktuellen Mozilla-CA-Bestand (curl.se) nach targets/invoke/ca-certificates.crt.
# Der Invoke hat nur den Bestand von 2018 (ohne Let's-Encrypt-Wurzeln ISRG X1/X2); hook.sh legt
# die Datei per Bind-Mount über /etc/ssl/certs/ca-certificates.crt. Danach aufs Gerät kopieren:
#   ssh invoke.lan 'cat > /data/leuchtfeuer/ca-certificates.crt' < targets/invoke/ca-certificates.crt
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
curl -fsSL https://curl.se/ca/cacert.pem -o "$here/targets/invoke/ca-certificates.crt"
grep -c BEGIN "$here/targets/invoke/ca-certificates.crt"
