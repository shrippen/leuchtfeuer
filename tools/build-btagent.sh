#!/usr/bin/env bash
# Baut den BlueZ-Agenten (src/btagent, reines Go, ohne cgo) für den Invoke (ARMv7).
#   tools/build-btagent.sh   -> build/btagent/btagent
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$here/build/btagent"
cd "$here/src/btagent"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w" -o "$here/build/btagent/btagent" .
file "$here/build/btagent/btagent"
