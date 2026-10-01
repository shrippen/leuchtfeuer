#!/bin/sh
# title: Bluetooth-Wiedergabe
# group: bluetooth
# process: bluealsa-aplay
# ports:
# requires: bluealsa-aplay
# default: off
# Spielt die Bluetooth-Audiodaten über ALSA "leuchtfeuer_bluetooth" (Quellen-Regler, Klang, "Leuchtfeuer Music") ab.
D=${LEUCHTFEUER_DIR:-/opt/leuchtfeuer}; PATH=$D/bin:$PATH
export ALSA_CONFIG=$D/asound-music.conf
exec bluealsa-aplay -D leuchtfeuer_bluetooth
