#!/bin/sh
# title: D-Bus (Systembus)
# group: core
# process: dbus-daemon
# ports:
# default: on
# D-Bus-Systembus (dbus-daemon des Geräts, eigene Konfiguration) für bluetoothd, bluealsa, btagent und – wenn
# installiert – avahi-daemon/Tidal Connect. Die Firmware bringt keinen Systembus mit. Früher Teil des Tidal-Moduls
# (tidal-1-dbus): ohne Tidal lief dann auch Bluetooth nicht.
mkdir -p /run/dbus
# Systembus eines älteren Stands (tidal-1-dbus, nach dem Update ohne Dienstskript) ablösen
for p in $(ps -o pid,args | grep '[d]bus-daemon --config-file=/data/leuchtfeuer/' | awk '{print $1}'); do kill "$p"; done
rm -f /run/dbus/pid /run/dbus/system_bus_socket
exec /usr/bin/dbus-daemon --config-file=/data/leuchtfeuer/dbus-system.conf --nofork --nopidfile
