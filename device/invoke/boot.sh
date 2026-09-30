#!/bin/sh
# Autostart-Haken: dnsmasq (/etc/dnsmasq.conf -> /data/dnsmasq.conf) ruft dieses Skript als
# dhcp-script auf; wegen "leasefile-ro" beim Start mit "init". dnsmasq erwartet auf stdout
# eine Lease-Liste -> nichts ausgeben, sofort zurückkehren, eigentliche Arbeit in hook.sh.
[ "$1" = init ] || exit 0
pid=$(cat /run/invoke-hook.pid 2>/dev/null)
[ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && exit 0
setsid /data/invoke/hook.sh </dev/null >/dev/null 2>&1 &
exit 0
