#!/usr/bin/env bash
# Baut den Cast-Empfänger (src/castrecv, reines Go, ohne cgo) für den Invoke (ARMv7).
#   tools/build-castrecv.sh   -> build/castrecv/castrecv
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$here/build/castrecv"
cd "$here/src/castrecv"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w" -o "$here/build/castrecv/castrecv" .
file "$here/build/castrecv/castrecv"
