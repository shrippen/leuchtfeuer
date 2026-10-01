#!/bin/sh
# AirPlay-Empfänger (shairport-sync 3.3.9, AirPlay 1): iPhone, iPad und Mac spielen auf "HK Invoke". Ausgabe über ALSA
# "invoke_music" (wie alle Musikdienste), mDNS über das eingebaute tinysvcmdns. Ports: 5000/tcp, 6001-6011/udp
# (ports.local). Abschalten in der Weboberfläche oder mit AIRPLAY="off" in /data/invoke/config.
. /data/invoke/config 2>/dev/null
[ "${AIRPLAY:-on}" = off ] && exec sleep 2147483647
NAME=$(printf '%s' "${DEVICE_NAME:-HK Invoke}" | tr -d '"\\')
export ALSA_CONFIG=/data/invoke/asound-music.conf
cat > /run/shairport-sync.conf <<CFG
general = { name = "$NAME"; mdns_backend = "tinysvcmdns"; };
alsa = { output_device = "invoke_music"; };
CFG
exec /data/invoke/bin/shairport-sync -c /run/shairport-sync.conf
