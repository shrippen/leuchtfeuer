#!/bin/sh
# Sendspin-Player (sendspin-go) für Music Assistant. Der Player verbindet sich selbst zum
# Server; mDNS-Suche klappt über den Router (WLAN <-> LAN) nicht, daher fest ploetze.lan:8927
# (Music Assistant 2.10). Ausgabe über das ALSA-Standardgerät, das asound-music.conf auf
# "music" umbiegt; höchstens 48 kHz/24 bit anbieten (DSP läuft mit 48 kHz, spart Resampling).
export ALSA_CONFIG=/data/invoke/asound-music.conf
export HOME=/data/invoke/sendspin
mkdir -p "$HOME"
cd "$HOME"
exec /data/invoke/bin/sendspin-player --daemon --name "HK Invoke" \
  --server ploetze.lan:8927 --max-sample-rate 48000 --max-bit-depth 24
