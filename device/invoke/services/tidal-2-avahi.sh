#!/bin/sh
# title: Tidal: avahi
# group: tidal
# process: avahi-daemon
# ports:
# default: on
# avahi-daemon 0.6.32 (Debian stretch) für die mDNS-Ankündigung von Tidal Connect.
# LD_PRELOAD-Shim liefert den Benutzer "avahi" (passwd ist schreibgeschützt).
for _ in 1 2 3 4 5 6 7 8 9 10; do [ -S /run/dbus/system_bus_socket ] && break; sleep 1; done
mkdir -p /run/avahi-daemon
export LD_LIBRARY_PATH=/data/invoke/tidal/lib LD_PRELOAD=/data/invoke/tidal/lib/avahi-user-shim.so
exec /data/invoke/tidal/sbin/avahi-daemon -f /data/invoke/tidal/avahi-daemon.conf \
  --no-drop-root --no-chroot --no-rlimits
