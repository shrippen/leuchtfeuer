#!/bin/sh
# title: Tidal Connect
# group: tidal
# process: tidal_connect_a
# ports: tcp 2019
# default: on
# Tidal Connect (iFi tidal_connect_application 1.1.3, iFi-Zertifikat; siehe tools/build-tidal-bundle.sh).
# Websocket 2019/tcp (ports.local). Ausgabe: portaudio -> ALSA default -> "invoke_tidal" (INVOKE_SRC, Quellen-Regler).
. /data/invoke/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
for _ in 1 2 3 4 5 6 7 8 9 10; do pidof avahi-daemon >/dev/null && break; sleep 2; done
export LD_LIBRARY_PATH=/data/invoke/tidal/lib ALSA_CONFIG=/data/invoke/asound-music.conf PA_ALSA_PLUGHW=1 INVOKE_SRC=tidal
cd /data/invoke/tidal || exit 1
exec ./bin/tidal_connect_application \
  --tc-certificate-path /data/invoke/tidal/cert/IfiAudio_ZenStream.dat \
  --netif-for-deviceid wlan0 -f "$NAME" --model-name "$NAME" \
  --codec-mpegh true --codec-mqa false --enable-mqa-passthrough false \
  --disable-app-security false --disable-web-security false \
  --playback-device default --log-level 3 --enable-websocket-log 0
