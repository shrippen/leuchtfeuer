#!/bin/sh
# Tidal Connect (iFi tidal_connect_application 1.1.3, iFi-Zertifikat; siehe tools/build-tidal-bundle.sh).
# Websocket 2019/tcp (ports.local). Ausgabe: portaudio -> ALSA default -> "music".
for i in 1 2 3 4 5 6 7 8 9 10; do pidof avahi-daemon >/dev/null && break; sleep 2; done
export LD_LIBRARY_PATH=/data/invoke/tidal/lib ALSA_CONFIG=/data/invoke/asound-music.conf PA_ALSA_PLUGHW=1
cd /data/invoke/tidal
exec ./bin/tidal_connect_application \
  --tc-certificate-path /data/invoke/tidal/cert/IfiAudio_ZenStream.dat \
  --netif-for-deviceid wlan0 -f "HK Invoke" --model-name "HK Invoke" \
  --codec-mpegh true --codec-mqa false --enable-mqa-passthrough false \
  --disable-app-security false --disable-web-security false \
  --playback-device default --log-level 3 --enable-websocket-log 0
