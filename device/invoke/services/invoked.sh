#!/bin/sh
# title: Weboberfläche (invoked)
# group: core
# process: invoked
# ports:
# default: on
# invoked: Weboberfläche (Port WEB_PORT, Standard 80; Passwort nur als Hash WEB_PASSWORD_HASH), WLAN-Wächter, Lautstärke-Abgleich,
# Webradio, Wecker, Timer, Tastenbelegung, Home-Assistant-Anbindung (MQTT). Webradio über "invoke_radio", Wecker-/Timertöne über "invoke_music", Durchsagen über "invoke_announce".
export ALSA_CONFIG=/data/invoke/asound-music.conf
. /data/invoke/config 2>/dev/null
exec /data/invoke/bin/invoked -listen ":${WEB_PORT:-80}"
