#!/bin/sh
# UPnP/DLNA-Renderer (gmrender-resurrect, GStreamer 1.10 des Geräts) -> ALSA "music".
# Ports: 49494/tcp (HTTP/SOAP), 1900/udp (SSDP) – in /data/invoke/ports.local freigegeben.
export GST_REGISTRY=/tmp/gst-registry.bin
cd /tmp
exec /data/invoke/bin/gmediarender -f "HK Invoke" \
  -u 4b494e56-4f4b-4500-0000-d8f710c12906 --port 49494 \
  --gstout-audiosink=alsasink --gstout-audiodevice=music
