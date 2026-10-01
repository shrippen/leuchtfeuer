#!/bin/sh
# title: Spotify Connect
# group: spotify
# process: librespot
# ports: tcp 57500
# default: on
# Spotify Connect (librespot 0.8, statisch). Ausgabe: aplay des Geräts -> ALSA "leuchtfeuer_spotify" (Quellen-Regler) -> "leuchtfeuer_music"
# (softvol "Leuchtfeuer Music" -> dmix -> DSP, 48 kHz; siehe asound-music.conf). Zugangsdaten nach der ersten Kopplung im Cache.
# Lautstärke: "--volume-ctrl fixed" dämpft nicht (Patch beim Build); den Regler der Spotify-App setzt leuchtfeuerd über
# audio-ui, so gelten Drehrad und App gemeinsam. --onevent meldet Wiedergabe, Titel und Lautstärke an leuchtfeuerd.
export ALSA_CONFIG=/data/leuchtfeuer/asound-music.conf
. /data/leuchtfeuer/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
exec /data/leuchtfeuer/bin/librespot \
  --name "$NAME" --device-type speaker --bitrate 320 \
  --backend subprocess --device "aplay -q -D leuchtfeuer_spotify -f S16_LE -r 44100 -c 2 -t raw" \
  --cache /data/leuchtfeuer/librespot-cache --zeroconf-port 57500 \
  --volume-ctrl fixed --initial-volume 100 \
  --onevent "/data/leuchtfeuer/bin/leuchtfeuerd -source-event spotify"
