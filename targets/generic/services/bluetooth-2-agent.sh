#!/bin/sh
# title: Bluetooth-Agent
# group: bluetooth
# process: btagent
# ports:
# requires: btagent bluealsa-aplay
# default: off
# uses: DEVICE_NAME
# BlueZ-Agent (src/btagent) für bluetoothd und bluealsa des Systems (Pakete bluez, bluez-alsa-utils; deren Dienste
# laufen über systemd): Kopplung ohne Rückfrage, Geräte vertrauen, Handy-Lautstärke abgleichen. Einschalten:
# Weboberfläche (Einstellungen > Dienste) oder SERVICE_BLUETOOTH="on".
D=${LEUCHTFEUER_DIR:-/opt/leuchtfeuer}; PATH=$D/bin:$PATH
. $D/config 2>/dev/null
# ohne Taste am Gerät: Kopplung über Weboberfläche/Home Assistant öffnen ("button") oder dauerhaft ("always")
exec btagent -name "${DEVICE_NAME:-${LEUCHTFEUER_NAME:-Leuchtfeuer}}" -pairing "${BLUETOOTH_PAIRING:-button}"
