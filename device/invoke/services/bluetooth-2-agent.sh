#!/bin/sh
# BlueZ-Agent (src/btagent): Kopplung ohne Rückfrage, Geräte vertrauen, Adapter sichtbar halten.
. /data/invoke/config 2>/dev/null
export DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket
exec /data/invoke/bin/btagent -name "${DEVICE_NAME:-HK Invoke}"
