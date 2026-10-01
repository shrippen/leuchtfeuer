#!/usr/bin/env bash
# Puts a freshly flashed Invoke into your home Wi-Fi. After flashing, the Invoke opens an unencrypted access point
# "HK Invoke_XXXXXX" (address 192.168.43.1). Connect this computer to that Wi-Fi first.
# Bringt einen frisch geflashten Invoke ins Heimnetz (zuerst den Rechner mit dem Zugangspunkt "HK Invoke_XXXXXX" verbinden).
#   scripts/wifi-setup.sh                      interactive
#   scripts/wifi-setup.sh "<SSID>" "<passphrase>"     (WPA2-PSK, 2.4 GHz)
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib.sh
SSID=${1:-}; PASS=${2:-}
if [ -z "$SSID" ]; then
  note "Make sure this computer is connected to the Wi-Fi 'HK Invoke_XXXXXX' of the speaker (no password)." \
       "Der Rechner muss mit dem WLAN 'HK Invoke_XXXXXX' des Lautsprechers verbunden sein (ohne Passwort)."
  ask SSID "Name (SSID) of YOUR Wi-Fi" "Name (SSID) DEINES WLANs"
fi
if [ -z "$PASS" ]; then
  [ "$INTERACTIVE" = 1 ] && { read -r -s -p "$(t 'Wi-Fi passphrase (not shown): ' 'WLAN-Passphrase (wird nicht angezeigt): ')" PASS; echo; }
fi
[ -n "$SSID" ] && [ -n "$PASS" ] || die "SSID and passphrase are required" "SSID und Passphrase sind nötig"
r=$(curl -sk -m 20 -X POST https://192.168.43.1/goform/HandleSACConfiguration \
      --data-urlencode "SSID=$SSID" --data "Security=WPA-PSK" --data-urlencode "Passphrase=$PASS")
info "Answer: $r" "Antwort: $r"
info "If the answer is \"continue\", the speaker joins your Wi-Fi now. Put this computer back into your home network;
the speaker's IP address is in your router's device list." \
"Lautet die Antwort \"continue\", verbindet sich der Lautsprecher jetzt mit deinem WLAN. Rechner wieder ins Heimnetz;
die IP des Lautsprechers steht in der Geräteliste des Routers."
