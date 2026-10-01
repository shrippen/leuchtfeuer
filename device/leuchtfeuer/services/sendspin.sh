#!/bin/sh
# title: Sendspin
# group: sendspin
# process: sendspin-player
# ports:
# requires: sendspin-player
# default: on
# uses: DEVICE_NAME SENDSPIN_SERVER
# Sendspin-Player (sendspin-go) für Music Assistant. Der Player verbindet sich selbst zum
# Server; mit SENDSPIN_SERVER in $LEUCHTFEUER_DIR/config fest "host:8927" (mDNS-Suche klappt über manche
# Router zwischen WLAN und LAN nicht), sonst mDNS. Ausgabe über das ALSA-Standardgerät, das asound-music.conf mit
# LEUCHTFEUER_SRC=sendspin auf "leuchtfeuer_sendspin" (Quellen-Regler) legt; höchstens 48 kHz/24 bit anbieten (DSP läuft mit 48 kHz, spart Resampling).
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}; PATH=$D/bin:$PATH
. $D/config 2>/dev/null
NAME=${DEVICE_NAME:-${LEUCHTFEUER_NAME:-Leuchtfeuer}}
export ALSA_CONFIG=$D/asound-music.conf LEUCHTFEUER_SRC=sendspin
export HOME=$D/sendspin
mkdir -p "$HOME"
cd "$HOME" || exit 1
SERVER=""; [ -n "$SENDSPIN_SERVER" ] && SERVER="--server $SENDSPIN_SERVER"
exec sendspin-player --daemon --name "$NAME" $SERVER \
  --max-sample-rate 48000 --max-bit-depth 24
