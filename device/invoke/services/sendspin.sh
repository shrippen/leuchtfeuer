#!/bin/sh
# Sendspin-Player (sendspin-go) für Music Assistant. Kündigt sich per mDNS an
# (_sendspin._tcp, Port 8927/tcp in ports.local). Ausgabe über das ALSA-Standardgerät,
# das asound-music.conf auf "music" umbiegt.
export ALSA_CONFIG=/data/invoke/asound-music.conf
export HOME=/data/invoke/sendspin
mkdir -p "$HOME"
cd "$HOME"
exec /data/invoke/bin/sendspin-player --daemon --name "HK Invoke"
