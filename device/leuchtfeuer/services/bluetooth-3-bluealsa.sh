#!/bin/sh
# title: Bluetooth-Audio (bluealsa)
# group: bluetooth
# process: bluealsa
# ports:
# default: on
# bluez-alsa: A2DP-Empfänger (Sink) an BlueZ; --a2dp-volume: keine Software-Dämpfung, die Handy-Lautstärke
# gleicht btagent mit dem Drehrad ab. Wartet kurz auf bluetoothd; bei Misserfolg startet
# der Hook den Dienst nach 30 s neu.
B=/data/leuchtfeuer/bluez
export LD_LIBRARY_PATH=$B/lib DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket
sleep 6
exec $B/bin/bluealsa -p a2dp-sink --a2dp-volume
