#!/bin/sh
# title: AirPlay
# group: airplay
# process: shairport-sync
# ports: tcp 5000, udp 6001:6011
# requires: shairport-sync
# default: on
# AirPlay-Empfänger (shairport-sync 3.3.9, AirPlay 1): iPhone, iPad und Mac spielen auf Leuchtfeuer. Ausgabe über ALSA
# "leuchtfeuer_airplay" (Quellen-Regler, dann wie alle Musikdienste), mDNS über das eingebaute tinysvcmdns.
# Abschalten: Weboberfläche (Einstellungen > Dienste) oder SERVICE_AIRPLAY="off" (alt: AIRPLAY="off"); dann startet der
# Hook den Dienst nicht und schließt seine Ports.
# Lautstärke: shairport dämpft nicht selbst (ignore_volume_control); den Regler des iPhones meldet es an leuchtfeuerd, das
# ihn über audio-ui setzt (wie Drehrad und Bluetooth). Wiedergabe-Beginn/-Ende und Titel (Metadaten-Pipe) gehen
# ebenfalls an leuchtfeuerd ("Läuft gerade", Quellen-Regel).
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}; PATH=$D/bin:$PATH
. $D/config 2>/dev/null
NAME=$(printf '%s' "${DEVICE_NAME:-${LEUCHTFEUER_NAME:-Leuchtfeuer}}" | tr -d '"\\')
EV=$D/bin/leuchtfeuerd
export ALSA_CONFIG=$D/asound-music.conf
cat > ${LEUCHTFEUER_RUN:-/run}/shairport-sync.conf <<CFG
general = {
  name = "$NAME";
  mdns_backend = "tinysvcmdns";
  ignore_volume_control = "yes";
  run_this_when_volume_is_set = "$EV -source-event airplay volume";
};
sessioncontrol = {
  run_this_before_play_begins = "$EV -source-event airplay playing";
  run_this_after_play_ends = "$EV -source-event airplay idle";
  wait_for_completion = "no";
};
metadata = {
  enabled = "yes";
  include_cover_art = "no";
  pipe_name = "${LEUCHTFEUER_RUN:-/run}/shairport-sync-metadata";
};
alsa = { output_device = "leuchtfeuer_airplay"; };
CFG
exec shairport-sync -c ${LEUCHTFEUER_RUN:-/run}/shairport-sync.conf
