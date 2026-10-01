#!/bin/sh
# invoked: Weboberfläche (Port 80, Benutzer admin, Passwort WEB_PASSWORD in /data/invoke/config), WLAN-Wächter,
# Webradio, Wecker, Timer, Tastenbelegung, Home-Assistant-Anbindung (MQTT). Töne über ALSA "invoke_music".
export ALSA_CONFIG=/data/invoke/asound-music.conf
. /data/invoke/config 2>/dev/null
exec /data/invoke/bin/invoked -listen ":${WEB_PORT:-80}"
