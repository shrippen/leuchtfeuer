#!/bin/sh
# title: Bluetooth-Agent
# group: bluetooth
# process: btagent
# ports:
# default: on
# uses: DEVICE_NAME
# BlueZ-Agent (src/btagent): Kopplung ohne Rückfrage, Geräte vertrauen, Adapter sichtbar halten.
. /data/leuchtfeuer/config 2>/dev/null
export DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket
# Kopplung nur nach Druck auf den Bluetooth-Knopf (BLUETOOTH_PAIRING="button", Standard) oder dauerhaft ("always")
exec /data/leuchtfeuer/bin/btagent -name "${DEVICE_NAME:-HK Invoke}" -pairing "${BLUETOOTH_PAIRING:-button}"
