#!/bin/sh
# title: Sendspin
# group: sendspin
# process: sendspin-player
# ports:
# default: on
# Sendspin-Player (sendspin-go) für Music Assistant. Der Player verbindet sich selbst zum
# Server; mit SENDSPIN_SERVER in /data/invoke/config fest "host:8927" (mDNS-Suche klappt über manche
# Router zwischen WLAN und LAN nicht), sonst mDNS. Ausgabe über das ALSA-Standardgerät, das asound-music.conf mit
# INVOKE_SRC=sendspin auf "invoke_sendspin" (Quellen-Regler) legt; höchstens 48 kHz/24 bit anbieten (DSP läuft mit 48 kHz, spart Resampling).
. /data/invoke/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
export ALSA_CONFIG=/data/invoke/asound-music.conf INVOKE_SRC=sendspin
export HOME=/data/invoke/sendspin
mkdir -p "$HOME"
cd "$HOME" || exit 1
SERVER=""; [ -n "$SENDSPIN_SERVER" ] && SERVER="--server $SENDSPIN_SERVER"
exec /data/invoke/bin/sendspin-player --daemon --name "$NAME" $SERVER \
  --max-sample-rate 48000 --max-bit-depth 24
