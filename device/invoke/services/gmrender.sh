#!/bin/sh
# UPnP/DLNA-Renderer (gmrender-resurrect, GStreamer 1.10 des Geräts) -> ALSA "invoke_music" (asound-music.conf).
# Ports: 49494/tcp (HTTP/SOAP), 1900/udp (SSDP) – in /data/invoke/ports.local freigegeben.
. /data/invoke/config 2>/dev/null
NAME=${DEVICE_NAME:-HK Invoke}
# UPnP-UUID aus der WLAN-MAC: pro Gerät eindeutig
MAC=$(tr -d : < /sys/class/net/wlan0/address)
export ALSA_CONFIG=/data/invoke/asound-music.conf
export GST_REGISTRY=/tmp/gst-registry.bin
cd /tmp
exec /data/invoke/bin/gmediarender -f "$NAME" \
  -u 4b494e56-4f4b-4500-0000-$MAC --port 49494 \
  --gstout-audiosink=alsasink --gstout-audiodevice=invoke_music
