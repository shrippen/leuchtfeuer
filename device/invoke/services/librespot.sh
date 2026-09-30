#!/bin/sh
# Spotify Connect (librespot 0.8, statisch). Ausgabe: aplay des Geräts -> ALSA "invoke_music"
# (softvol "Invoke Music" -> dmix -> DSP, 48 kHz; siehe asound-music.conf). Zugangsdaten nach der ersten Kopplung im Cache.
export ALSA_CONFIG=/data/invoke/asound-music.conf
. /data/invoke/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
exec /data/invoke/bin/librespot \
  --name "$NAME" --device-type speaker --bitrate 320 \
  --backend subprocess --device "aplay -q -D invoke_music -f S16_LE -r 44100 -c 2 -t raw" \
  --cache /data/invoke/librespot-cache --zeroconf-port 57500 \
  --initial-volume 50 --volume-ctrl log
