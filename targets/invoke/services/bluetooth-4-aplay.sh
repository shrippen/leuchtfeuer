#!/bin/sh
# title: Bluetooth-Wiedergabe
# group: bluetooth
# process: bluealsa-aplay
# ports:
# default: on
# Spielt die Bluetooth-Audiodaten über ALSA "leuchtfeuer_bluetooth" (Quellen-Regler, dann wie alle Musikdienste: Regler
# "Leuchtfeuer Music" -> dmix -> DSP) ab.
B=/data/leuchtfeuer/bluez
export LD_LIBRARY_PATH=$B/lib ALSA_CONFIG=/data/leuchtfeuer/asound-music.conf DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket
sleep 10
exec $B/bin/bluealsa-aplay -D leuchtfeuer_bluetooth
