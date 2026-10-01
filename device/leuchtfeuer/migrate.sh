#!/bin/sh
# Übergang älterer Installationen (bis Oktober 2026 hieß alles "invoke"): /data/invoke -> /data/leuchtfeuer,
# invoked -> leuchtfeuerd, /run/invoke-* -> /run/leuchtfeuer-*, Autostart-Pfad in /data/dnsmasq.conf.
# Läuft bei jedem Start (boot.sh vor dem Hook) und vor jeder Installation (install.sh); ohne alte Installation tut es
# nichts. Alte Prozesse (Hook, Dienste) werden beendet: der neue Hook startet sie mit den neuen Namen (Tonkette,
# Regler) neu. Das eigene dropbear läuft weiter (seine PID-Datei bekommt nur den neuen Namen), SSH reißt nicht ab.
# /data/invoke bleibt als Verweis auf /data/leuchtfeuer: der alte Autostart-Pfad und alte Skripte gehen weiter.
OLD=${LEUCHTFEUER_OLD_DIR:-/data/invoke}
NEW=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}
R=${LEUCHTFEUER_RUN:-/run}
CONF=${LEUCHTFEUER_DNSMASQ:-/data/dnsmasq.conf}

log(){ mkdir -p "$NEW" 2>/dev/null; echo "$(date '+%Y-%m-%d %H:%M:%S') migrate: $*" >> "$NEW/hook.log"; }

# 1. Laufende alte Prozesse: Hook und Dienste beenden, dropbear behalten
if [ -e "$R/invoke-hook.pid" ]; then
  p=$(cat "$R/invoke-hook.pid" 2>/dev/null)
  [ -n "$p" ] && [ "$p" != "$$" ] && kill "$p" 2>/dev/null && log "alten Hook (pid $p) beendet"
  rm -f "$R/invoke-hook.pid"
fi
for pf in "$R"/invoke-svc-*.pid; do
  [ -f "$pf" ] || continue
  p=$(cat "$pf" 2>/dev/null)
  [ -n "$p" ] && kill "$p" 2>/dev/null && log "alten Dienst $(basename "$pf" .pid) (pid $p) beendet"
  rm -f "$pf"
done
if [ -f "$R/invoke-dropbear.pid" ] && [ ! -e "$R/leuchtfeuer-dropbear.pid" ]; then
  mv "$R/invoke-dropbear.pid" "$R/leuchtfeuer-dropbear.pid"
fi
for f in "$R"/invoke-*; do [ -e "$f" ] && rm -f "$f"; done

# 2. Verzeichnis
if [ -d "$OLD" ] && [ ! -L "$OLD" ]; then
  if [ ! -e "$NEW" ]; then
    mv "$OLD" "$NEW" && ln -s "$NEW" "$OLD" && log "$OLD -> $NEW verschoben (Verweis bleibt)"
  else
    # beide vorhanden (z. B. Installer hat schon kopiert): was im neuen fehlt, aus dem alten übernehmen
    # (Konfiguration, Schlüssel, Kopplungen, Spotify-Anmeldung), dann das alte beiseite legen
    (cd "$OLD" && find . -type f) | while read -r f; do
      [ -e "$NEW/$f" ] && continue
      mkdir -p "$(dirname "$NEW/$f")" && cp -p "$OLD/$f" "$NEW/$f"
    done
    rm -rf "$OLD.alt"; mv "$OLD" "$OLD.alt" && ln -s "$NEW" "$OLD" && log "$OLD in $NEW übernommen (Rest in $OLD.alt)"
  fi
fi
[ -d "$NEW" ] || exit 0

# 3. alte Datei- und Programmnamen
if [ -f "$NEW/invoked.json" ] && [ ! -e "$NEW/leuchtfeuerd.json" ]; then
  mv "$NEW/invoked.json" "$NEW/leuchtfeuerd.json" && log "invoked.json -> leuchtfeuerd.json"
fi
for f in services/invoked.sh bin/invoked lib/ladspa/invoke-viz-tap.so lib/ladspa/invoke-eq.so; do
  [ -e "$NEW/$f" ] && rm -f "$NEW/$f" && log "$f entfernt (heißt jetzt leuchtfeuer…)"
done

# 4. Autostart-Pfad
if grep -q "dhcp-script=$OLD/boot.sh" "$CONF" 2>/dev/null; then
  sed -e "s#dhcp-script=$OLD/boot.sh#dhcp-script=$NEW/boot.sh#" -e "s#Autostart-Haken ($OLD/boot.sh)#Autostart-Haken ($NEW/boot.sh)#" \
      -e "s#Invoke-Hack: Autostart-Haken#Leuchtfeuer: Autostart-Haken#" "$CONF" > "$CONF.new" &&
    grep -q "dhcp-script=$NEW/boot.sh" "$CONF.new" && cat "$CONF.new" > "$CONF" && log "Autostart-Pfad in $CONF angepasst"
  rm -f "$CONF.new"
fi
exit 0
