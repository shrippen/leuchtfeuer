#!/bin/sh
# title: Spotify Connect
# group: spotify
# process: librespot
# ports: tcp 57500
# requires: librespot aplay
# default: on
# Spotify Connect (librespot 0.8, statisch). Ausgabe: aplay des Geräts -> ALSA "leuchtfeuer_spotify" (Quellen-Regler) -> "leuchtfeuer_music"
# (softvol "Leuchtfeuer Music" -> dmix -> DSP, 48 kHz; siehe asound-music.conf). Zugangsdaten nach der ersten Kopplung im Cache.
# Lautstärke: "--volume-ctrl fixed" dämpft nicht (Patch beim Build); den Regler der Spotify-App setzt leuchtfeuerd über
# audio-ui, so gelten Drehrad und App gemeinsam. --onevent meldet Wiedergabe, Titel und Lautstärke an leuchtfeuerd.
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}; PATH=$D/bin:$PATH
export ALSA_CONFIG=$D/asound-music.conf
. $D/config 2>/dev/null
NAME=${DEVICE_NAME:-${LEUCHTFEUER_NAME:-Leuchtfeuer}}
exec librespot \
  --name "$NAME" --device-type speaker --bitrate 320 \
  --backend subprocess --device "aplay -q -D leuchtfeuer_spotify -f S16_LE -r 44100 -c 2 -t raw" \
  --cache $D/librespot-cache --zeroconf-port 57500 \
  --volume-ctrl fixed --initial-volume 100 \
  --onevent "$D/bin/leuchtfeuerd -source-event spotify"
