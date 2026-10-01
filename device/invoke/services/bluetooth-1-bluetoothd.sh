#!/bin/sh
# title: Bluetooth (bluetoothd)
# group: bluetooth
# process: bluetoothd
# ports:
# default: on
# BlueZ 5.50 (bluetoothd) statt des Harman-Bluedroid-Stacks (aus podium.conf entfernt).
# Lädt selbst den Treiber (das tat vorher bluetooth.sh); Kopplungen liegen unter
# /data/invoke/bluez/var und überstehen Neustarts. Konfiguration: bluez/etc/bluetooth/main.conf.
B=/data/invoke/bluez
export LD_LIBRARY_PATH=$B/lib DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket
i=0; while [ ! -S /run/dbus/system_bus_socket ] && [ $i -lt 60 ]; do sleep 1; i=$((i + 1)); done
lsmod | grep -q '^bt8xxx ' || insmod /lib/modules/linux_ver/kernel/arch/arm/mach-berlin/modules/bt_sd8887/bt8xxx.ko \
  fw_name=mrvl/sd8887_bt_a2_new.bin bt_fw_serial=0
# Adapter vorher hochfahren: beim Kaltstart lädt der Chip seine Firmware, das dauert länger, als
# bluetoothd wartet ("Loading LTKs timed out", Adapter bleibt DOWN)
i=0; while [ $i -lt 10 ]; do
  $B/bin/hciconfig hci0 up 2>/dev/null
  $B/bin/hciconfig hci0 | grep -q "UP RUNNING" && break
  sleep 2; i=$((i + 1))
done
exec $B/bin/bluetoothd -n
