#!/bin/sh
# Sendspin-Player (sendspin-go) für Music Assistant. Der Player verbindet sich selbst zum
# Server; mit SENDSPIN_SERVER in /data/invoke/config fest "host:8927" (mDNS-Suche klappt über manche
# Router zwischen WLAN und LAN nicht), sonst mDNS. Ausgabe über das ALSA-Standardgerät, das asound-music.conf auf
# "music" umbiegt; höchstens 48 kHz/24 bit anbieten (DSP läuft mit 48 kHz, spart Resampling).
. /data/invoke/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
export ALSA_CONFIG=/data/invoke/asound-music.conf
export HOME=/data/invoke/sendspin
mkdir -p "$HOME"
cd "$HOME"
SERVER=""; [ -n "$SENDSPIN_SERVER" ] && SERVER="--server $SENDSPIN_SERVER"
exec /data/invoke/bin/sendspin-player --daemon --name "$NAME" $SERVER \
  --max-sample-rate 48000 --max-bit-depth 24
