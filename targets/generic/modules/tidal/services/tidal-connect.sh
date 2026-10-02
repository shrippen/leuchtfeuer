#!/bin/sh
# title: Tidal Connect
# group: tidal
# process: tidal_connect_a
# ports: tcp 2019
# requires: tidal-connect aplay
# default: on
# uses: DEVICE_NAME
# Tidal Connect (Modul, nur ARM): proprietäres iFi-Programm mit iFi-Gerätezertifikat, nicht lizenziert, kann gesperrt
# werden. Kommt mit dem Modul (setup.sh --module), nie mit dem Paket. Start und Ton: bin/tidal-connect.
D=${LEUCHTFEUER_DIR:-/opt/leuchtfeuer}; PATH=$D/bin:$PATH
. $D/config 2>/dev/null
exec tidal-connect "${DEVICE_NAME:-${LEUCHTFEUER_NAME:-Leuchtfeuer}}"
