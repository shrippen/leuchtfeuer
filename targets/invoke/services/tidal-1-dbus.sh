#!/bin/sh
# title: Tidal: D-Bus
# group: tidal
# process: dbus-daemon
# ports:
# default: on
# D-Bus-Systembus (dbus-daemon des Geräts) nur für avahi-daemon/Tidal Connect.
mkdir -p /run/dbus
rm -f /run/dbus/pid /run/dbus/system_bus_socket
exec /usr/bin/dbus-daemon --config-file=/data/leuchtfeuer/tidal/dbus-system.conf --nofork --nopidfile
