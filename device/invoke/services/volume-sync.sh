#!/bin/sh
# Lautstärke: das Drehrad des Invoke setzt die Regler "system" und "voice" (Harman audio-ui).
# audio-ui dämpft dabei "music" bei jedem Schritt kurz auf 0, darum spielen die Musikdienste
# über einen eigenen Regler "Invoke Music" (asound-music.conf: PCM invoke_music), den
# audio-ui nicht kennt. Dieser Dienst legt ihn auf den Wert von "system".
export ALSA_CONFIG=/data/invoke/asound-music.conf
vol(){ amixer -c 0 sget "$1" 2>/dev/null | sed -n 's/.*Front Left: \([0-9]*\) .*/\1/p' | head -n1; }
# softvol legt den Regler erst beim ersten Öffnen des PCM an
[ -n "$(vol 'Invoke Music')" ] || aplay -q -D invoke_music -d 1 -f S16_LE -r 48000 -c 2 /dev/zero
while :; do
  v=$(vol system); m=$(vol 'Invoke Music')
  [ -n "$v" ] && [ "$v" != "$m" ] && amixer -c 0 -q sset 'Invoke Music' "$v"
  sleep 0.2
done
