#!/bin/sh
# Setzt ein Update in Kraft und kann es zurücknehmen (genutzt von install.sh, der Weboberfläche und vom Hook).
#
#   apply-update.sh apply <Stufe> [norestart] [nowatch]
#                                               Dateien aus <Stufe> (Verzeichnis, Pfade wie unter /data/invoke) an
#                                               ihren Platz bringen. Vorher werden die ersetzten Dateien nach
#                                               /data/invoke/.prev gesichert; <Stufe>/.remove listet Dateien, die
#                                               entfallen. Danach update-pending setzen: der Hook beobachtet die
#                                               Dienste 10 Minuten und nimmt das Update bei Ausfällen zurück
#                                               (nowatch: nicht, z. B. bei der Erstinstallation).
#   apply-update.sh rollback [Grund]            vorigen Stand aus .prev wiederherstellen, Dienste neu starten.
#
# podium.conf und ca-certificates.crt sind per Bind-Mount eingehängt: sie werden an Ort und Stelle überschrieben
# (gleiche Inode), sonst sähe der Mount die neue Datei nicht.
set -e
D=${INVOKE_DIR:-/data/invoke}
R=${INVOKE_RUN:-/run}
P=$D/.prev
log(){ echo "$(date '+%Y-%m-%d %H:%M:%S') update: $*" >> $D/hook.log; echo "$*"; }

inplace(){ case $1 in ./podium.conf|./ca-certificates.crt) return 0 ;; esac; return 1; }

restart_all(){
  # Hook beenden, laufende Dienste beenden (sie werden vom neuen Hook gestartet), Hook neu starten
  pid=$(cat $R/invoke-hook.pid 2>/dev/null); [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  sleep 1
  for pf in $R/invoke-svc-*.pid; do
    [ -f "$pf" ] || continue
    n=${pf#"$R"/invoke-svc-}; n=${n%.pid}
    touch "$R/invoke-svc-$n.expected"
    kill "$(cat "$pf")" 2>/dev/null || true
  done
  if grep -q " /etc/podium/podium.conf " /proc/mounts && [ "$(cat $R/invoke-podium-changed 2>/dev/null)" = 1 ]; then
    stop podium; sleep 3; start podium
  fi
  rm -f $R/invoke-podium-changed $R/invoke-fw.rules $R/invoke-svc-*.state
  sh $D/boot.sh init
}

apply(){
  S=$1
  [ -d "$S" ] || { echo "Stufe $S fehlt" >&2; exit 1; }
  cd "$S"
  rm -rf "$P"; mkdir -p "$P"; : > "$P/.new-files"
  # Sicherung der Dateien, die ersetzt oder entfernt werden
  { find . -type f ! -name .remove ! -name VERSION; [ -f .remove ] && sed 's|^/*|./|' .remove; } | while read -r f; do
    if [ -f "$D/$f" ]; then mkdir -p "$P/$(dirname "$f")"; cp -p "$D/$f" "$P/$f"
    else echo "$f" >> "$P/.new-files"; fi
  done
  cat $D/VERSION > "$P/VERSION" 2>/dev/null || true
  PODIUM=0
  [ -f podium.conf ] && ! cmp -s podium.conf $D/podium.conf 2>/dev/null && PODIUM=1
  echo $PODIUM > $R/invoke-podium-changed
  find . -type f ! -name .remove | while read -r f; do
    if inplace "$f" && [ -f "$D/$f" ]; then cat "$f" > "$D/$f"; rm -f "$f"; continue; fi
    mkdir -p "$D/$(dirname "$f")"; mv -f "$f" "$D/$f"
  done
  if [ -f .remove ]; then sed 's|^/*|./|' .remove | while read -r f; do rm -f "$D/$f"; done; fi
  cd $D; rm -rf "$S"
  chmod 600 $D/authorized_keys 2>/dev/null || true
  chmod 755 $D/boot.sh $D/hook.sh $D/apply-update.sh $D/bin/* $D/services/*.sh 2>/dev/null || true
  [ "${WATCH:-1}" = 1 ] && date +%s > $D/update-pending
  rm -f $D/update-rolledback
  log "angewendet ($(cat $D/VERSION 2>/dev/null || echo '?'))"
}

rollback(){
  [ -d "$P" ] || { log "kein voriger Stand (.prev) vorhanden"; rm -f $D/update-pending; exit 1; }
  cd "$P"
  find . -type f ! -name .new-files ! -name VERSION | while read -r f; do
    if inplace "$f" && [ -f "$D/$f" ]; then cat "$f" > "$D/$f"; else mkdir -p "$D/$(dirname "$f")"; cp -p "$f" "$D/$f"; fi
  done
  [ -f .new-files ] && while read -r f; do [ -n "$f" ] && rm -f "$D/$f"; done < .new-files
  [ -f VERSION ] && cp VERSION $D/VERSION
  cd $D; rm -rf "$P"
  rm -f $D/update-pending
  echo "$(date +%s) ${1:-}" > $D/update-rolledback
  echo 1 > $R/invoke-podium-changed
  log "zurückgenommen (${1:-ohne Grund}), voriger Stand wiederhergestellt"
}

case ${1:-} in
  apply)
    WATCH=1; RESTART=1
    for o in "${3:-}" "${4:-}"; do case $o in nowatch) WATCH=0 ;; norestart) RESTART=0 ;; esac; done
    apply "${2:?Stufe fehlt}"; [ $RESTART = 0 ] || restart_all ;;
  rollback) rollback "${2:-}"; restart_all ;;
  *) sed -n '2,12p' "$0"; exit 2 ;;
esac
