#!/bin/sh
# title: Weboberfläche (leuchtfeuerd)
# group: core
# process: leuchtfeuerd
# ports:
# requires: leuchtfeuerd
# default: on
# leuchtfeuerd: Weboberfläche (Port WEB_PORT, Standard 80; Passwort nur als Hash WEB_PASSWORD_HASH), WLAN-Wächter, Lautstärke-Abgleich,
# Webradio, Wecker, Timer, Tastenbelegung, Home-Assistant-Anbindung (MQTT). Webradio über "leuchtfeuer_radio", Wecker-/Timertöne über "leuchtfeuer_music", Durchsagen über "leuchtfeuer_announce".
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}; PATH=$D/bin:$PATH
export ALSA_CONFIG=$D/asound-music.conf
. $D/config 2>/dev/null
exec leuchtfeuerd -listen ":${WEB_PORT:-80}"
