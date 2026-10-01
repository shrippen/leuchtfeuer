#!/bin/sh
# title: Weboberfläche (leuchtfeuerd)
# group: core
# process: leuchtfeuerd
# ports:
# default: on
# leuchtfeuerd: Weboberfläche (Port WEB_PORT, Standard 80; Passwort nur als Hash WEB_PASSWORD_HASH), WLAN-Wächter, Lautstärke-Abgleich,
# Webradio, Wecker, Timer, Tastenbelegung, Home-Assistant-Anbindung (MQTT). Webradio über "leuchtfeuer_radio", Wecker-/Timertöne über "leuchtfeuer_music", Durchsagen über "leuchtfeuer_announce".
export ALSA_CONFIG=/data/leuchtfeuer/asound-music.conf
. /data/leuchtfeuer/config 2>/dev/null
exec /data/leuchtfeuer/bin/leuchtfeuerd -listen ":${WEB_PORT:-80}"
