#!/bin/sh
# title: Snapcast
# group: snapcast
# process: snapclient
# ports:
# requires: snapclient aplay
# default: off
# Snapcast-Client (Multiroom, synchron mit anderen Lautsprechern): verbindet sich mit dem snapserver SNAPCAST_SERVER
# (Adresse oder Name; ohne avahi keine automatische Suche). Der Datei-Player schreibt den Ton (Format des Servers,
# erwartet wird 48000:16:2) in eine Pipe, aplay spielt ihn über "leuchtfeuer_snapcast" (Quellen-Regler) ab.
# Einschalten: Weboberfläche (Einstellungen > Dienste) oder SERVICE_SNAPCAST="on". Ausgleich der Verzögerung von aplay:
# in snapweb/snapserver beim Client "Latenz" einstellen (etwa 100 ms).
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}; PATH=$D/bin:$PATH
. $D/config 2>/dev/null
if [ -z "$SNAPCAST_SERVER" ]; then
  echo "SNAPCAST_SERVER ist nicht gesetzt (Einstellungen): Snapcast wartet"
  exec sleep 2147483647
fi
export ALSA_CONFIG=$D/asound-music.conf
MAC=$(tr -d : < /sys/class/net/${WIFI_IFACE:-wlan0}/address)
F=${LEUCHTFEUER_RUN:-/run}/snapclient.pcm
rm -f $F; mkfifo $F
aplay -q -D leuchtfeuer_snapcast -f S16_LE -r 48000 -c 2 -t raw < $F &
exec snapclient --host "${SNAPCAST_SERVER%:*}" --port "$(case $SNAPCAST_SERVER in *:*) echo "${SNAPCAST_SERVER##*:}" ;; *) echo 1704 ;; esac)" \
  --hostID "$MAC" --player file:filename=$F --logsink stderr
