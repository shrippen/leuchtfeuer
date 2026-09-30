#!/bin/sh
# Cast-Empfänger (Chromecast Audio, nachgebildet; src/castrecv). Sichtbar als "HK Invoke" für
# Sender ohne Geräteprüfung durch Google (Music Assistant, Home Assistant, VLC). Ports 8009 (TLS),
# 8008/8443 (eureka_info) in /data/invoke/ports.local freigegeben. Ausgabe wie die anderen
# Musikdienste über ALSA "invoke_music"; die Lautstärke setzt den Regler "system" (Drehrad).
. /data/invoke/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
export ALSA_CONFIG=/data/invoke/asound-music.conf
exec /data/invoke/bin/castrecv -name "$NAME" -device invoke_music -iface wlan0
