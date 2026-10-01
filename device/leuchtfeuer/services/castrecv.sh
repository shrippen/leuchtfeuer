#!/bin/sh
# title: Cast
# group: cast
# process: castrecv
# ports: tcp 8009, tcp 8008, tcp 8443
# requires: castrecv
# default: on
# Cast-Empfänger (Chromecast Audio, nachgebildet; src/castrecv). Sichtbar unter dem Gerätenamen für
# Sender ohne Geräteprüfung durch Google (Music Assistant, Home Assistant, VLC). Ports 8009 (TLS),
# 8008/8443 (eureka_info) in $LEUCHTFEUER_DIR/ports.local freigegeben. Ausgabe wie die anderen
# Musikdienste über ALSA ("leuchtfeuer_cast", Quellen-Regler); die Lautstärke führt leuchtfeuerd (lokaler Bus).
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}; PATH=$D/bin:$PATH
. $D/config 2>/dev/null
NAME=${DEVICE_NAME:-${LEUCHTFEUER_NAME:-Leuchtfeuer}}
export ALSA_CONFIG=$D/asound-music.conf
exec castrecv -name "$NAME" -device leuchtfeuer_cast -iface "${WIFI_IFACE:-wlan0}"
