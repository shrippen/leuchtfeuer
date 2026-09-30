#!/usr/bin/env bash
# Bringt einen frisch geflashten Invoke ins Heimnetz. Der Invoke öffnet nach dem Flashen einen offenen
# WLAN-Zugangspunkt "HK Invoke_XXXXXX" (Adresse 192.168.43.1). Rechner vorher mit diesem WLAN verbinden.
#   scripts/wifi-setup.sh "<SSID>" "<Passphrase>"     (WPA2-PSK, 2,4 GHz)
set -euo pipefail
[ $# -eq 2 ] || { sed -n '2,6p' "$0"; exit 2; }
r=$(curl -sk -m 20 -X POST https://192.168.43.1/goform/HandleSACConfiguration \
      --data-urlencode "SSID=$1" --data "Security=WPA-PSK" --data-urlencode "Passphrase=$2")
echo "Antwort: $r"
echo "Bei \"continue\" verbindet sich der Invoke jetzt mit dem WLAN. Rechner wieder ins Heimnetz, die IP des"
echo "Invoke steht im Router (DHCP-Liste)."
