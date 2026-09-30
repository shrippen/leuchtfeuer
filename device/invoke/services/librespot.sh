#!/bin/sh
# Spotify Connect (librespot 0.8, statisch). Ausgabe: aplay des Geräts -> ALSA "music"
# (softvol "music" -> dmix -> DSP, 48 kHz). Zugangsdaten nach der ersten Kopplung im Cache.
exec /data/invoke/bin/librespot \
  --name "HK Invoke" --device-type speaker --bitrate 320 \
  --backend subprocess --device "aplay -q -D music -f S16_LE -r 44100 -c 2 -t raw" \
  --cache /data/invoke/librespot-cache --zeroconf-port 57500 \
  --initial-volume 50 --volume-ctrl log
