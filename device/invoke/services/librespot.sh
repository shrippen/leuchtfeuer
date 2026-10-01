#!/bin/sh
# title: Spotify Connect
# group: spotify
# process: librespot
# ports: tcp 57500
# default: on
# Spotify Connect (librespot 0.8, statisch). Ausgabe: aplay des Geräts -> ALSA "invoke_spotify" (Quellen-Regler) -> "invoke_music"
# (softvol "Invoke Music" -> dmix -> DSP, 48 kHz; siehe asound-music.conf). Zugangsdaten nach der ersten Kopplung im Cache.
# Lautstärke: "--volume-ctrl fixed" dämpft nicht (Patch beim Build); den Regler der Spotify-App setzt invoked über
# audio-ui, so gelten Drehrad und App gemeinsam. --onevent meldet Wiedergabe, Titel und Lautstärke an invoked.
export ALSA_CONFIG=/data/invoke/asound-music.conf
. /data/invoke/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
exec /data/invoke/bin/librespot \
  --name "$NAME" --device-type speaker --bitrate 320 \
  --backend subprocess --device "aplay -q -D invoke_spotify -f S16_LE -r 44100 -c 2 -t raw" \
  --cache /data/invoke/librespot-cache --zeroconf-port 57500 \
  --volume-ctrl fixed --initial-volume 100 \
  --onevent "/data/invoke/bin/invoked -source-event spotify"
