#!/bin/sh
# title: Cast
# group: cast
# process: castrecv
# ports: tcp 8009, tcp 8008, tcp 8443
# default: on
# Cast-Empfänger (Chromecast Audio, nachgebildet; src/castrecv). Sichtbar als "HK Invoke" für
# Sender ohne Geräteprüfung durch Google (Music Assistant, Home Assistant, VLC). Ports 8009 (TLS),
# 8008/8443 (eureka_info) in /data/leuchtfeuer/ports.local freigegeben. Ausgabe wie die anderen
# Musikdienste über ALSA ("leuchtfeuer_cast", Quellen-Regler); die Lautstärke geht über audio-ui wie beim Drehrad.
. /data/leuchtfeuer/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
export ALSA_CONFIG=/data/leuchtfeuer/asound-music.conf
exec /data/leuchtfeuer/bin/castrecv -name "$NAME" -device leuchtfeuer_cast -iface wlan0
