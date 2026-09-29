#!/usr/bin/env bash
# Holt die Original-Assets (nicht im Git). Idempotent.
set -e
cd "$(dirname "$0")/../firmware"
dl(){ [ -f "$2" ] && echo "vorhanden: $2" || gh release download "$1" --repo coggy9/HKHacking --pattern "$3" -O "$2"; }
dl HarmanFlash Harman.Kardon.INVOKE.Flashing.zip "Harman.Kardon.INVOKE.Flashing.zip"
dl StockRoot   83_IMAGE                          "83_IMAGE"
dl FinalOTA    Harman.Kardon.INVOKE.Driver.OTA2.zip "Harman.Kardon.INVOKE.Driver.OTA2.zip"
