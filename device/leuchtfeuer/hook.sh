#!/bin/sh
# Hook von Leuchtfeuer: läuft als root, prüft alle 30 s. Gemeinsam für alle Zielgeräte; was nur ein Gerät braucht,
# steht in $D/target.sh (Quelle: targets/<ziel>/target.sh, siehe docs/TARGETS.md). Gestartet auf dem Invoke von
# boot.sh (dnsmasq-Haken), auf "generic" von systemd (targets/generic/leuchtfeuer.service).
#  - Firewall (Kette LEUCHTFEUER vor INPUT; FIREWALL="on|off" in config, Vorgabe je Ziel): SSH, mDNS, DHCP-Antworten,
#    ICMP, bestehende Verbindungen, die Weboberfläche, die Ports der EINGESCHALTETEN Dienste (Kopfzeile "# ports:" der
#    Dienstskripte), Regeln des Ziels (target_fw_rules), eigene Ports aus $D/ports.local ("tcp 1234" / "udp 5678") und
#    die von leuchtfeuerd geschriebenen aus $D/ports.leuchtfeuerd (Sprachassistent).
#    Ändert sich etwas (Dienst an/aus, ports.local), wird die Kette neu aufgebaut, offene Ports schließen sich wieder.
#  - Uhrzeit: sobald das Netz da ist (target_net) und dann alle 6 h einmal per NTP stellen (busybox ntpd -q, Server
#    NTP_SERVER), falls kein Zeitdienst läuft. Wecker hängen an der richtigen Uhr.
#  - Dienste: jedes ausführbare $D/services/<name>.sh (endet mit exec) wird gestartet, wenn seine Gruppe
#    eingeschaltet ist (Kopfzeile "# group:", Schalter SERVICE_<GRUPPE>="on|off" in config, Vorgabe "# default:")
#    und die Programme aus "# requires:" vorhanden sind (sonst Zustand "missing"; $D/bin liegt vorn im PATH).
#    Die Dienste erben LEUCHTFEUER_DIR, LEUCHTFEUER_RUN, LEUCHTFEUER_NAME (Gerätename, solange DEVICE_NAME fehlt),
#    WIFI_IFACE, ALSA_CONFIG ($D/asound-music.conf), ALSA_CARD und LEUCHTFEUER_OUT (ALSA_OUTPUT in config, nur generic).
#    Stirbt ein Dienst kurz nach dem Start immer wieder, wartet der Hook zunehmend länger (30 s ... 30 min) und
#    meldet ihn als fehlerhaft; Zustand in $R/leuchtfeuer-svc-<name>.state ("Fehlschläge nächsterStart gestartet Neustarts").
#    Ein absichtliches Beenden (leuchtfeuerd: Neustart-Knopf) kündigt $R/leuchtfeuer-svc-<name>.expected an und zählt nicht.
#  - Protokolle: $D/log/<name>.log und hook.log werden ab 1 MiB gekürzt (die letzten 256 KiB -> .1); das geht auch
#    bei laufenden Diensten, die Datei bleibt dieselbe.
#  - Update-Rückfall: Nach einem Update ($D/update-pending) beobachtet der Hook 10 Minuten lang die Dienste;
#    fällt einer wiederholt aus, stellt apply-update.sh den vorigen Stand wieder her.
#  - Hardware-Watchdog (WATCHDOG="on" in config, falls /dev/watchdog da ist): "leuchtfeuerd -watchdog" setzt die Frist auf
#    60 s und füttert ihn nur, solange dieser Hook läuft (Lebenszeichen $R/leuchtfeuer-hook.alive je Durchlauf). Hängt das
#    System oder der Hook, startet das Gerät neu. Schutz vor einer Neustart-Schleife: Nach 3 Starts mit scharfem
#    Watchdog ohne 30 Minuten stabile Laufzeit bleibt er aus (zurücksetzen: $D/watchdog-unstable löschen).
#  - Klänge: von leuchtfeuerd ersetzte Klänge der Hersteller-Software (sounds/vendor.map: "<Original>\t<Datei>") per
#    Bind-Mount einhängen, beim Start vor target_init (das startet auf dem Invoke die Hersteller-Dienste neu).
# Zielgerät ($D/target.sh, optional): setzt TARGET_ID, TARGET_NAME, TARGET_FIREWALL, TARGET_IFACE, TARGET_ALSA_BASE
# (Grundkonfiguration der alsa-lib, wenn keine Hersteller-asound.conf ALSA_CONFIG einbindet), TARGET_DBUS (System-Bus der Dienste,
# wenn nicht der Standard) und kann definieren:
#   target_init      einmal beim Start          target_tick   in jedem Durchlauf
#   target_fw_rules  zusätzliche Firewall-Regeln target_net    Netz bereit? (DHCP-Name melden u. Ä.; alle 6 h)
# Notbremse: /data/leuchtfeuer/disable-hook anlegen -> Skript macht nichts.
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}
R=${LEUCHTFEUER_RUN:-/run}   # Laufzeit-Dateien (Tests setzen beides um)
echo $$ > $R/leuchtfeuer-hook.pid
log(){ echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >> $D/hook.log; }
[ -e $D/disable-hook ] && { log "disable-hook gesetzt – nichts zu tun"; exit 0; }
log "hook gestartet (pid $$)"

# Wert aus der Konfiguration (sh-Datei) lesen, ohne sie im Hook-Prozess zu laden
cfg(){ (. $D/config 2>/dev/null; eval "printf '%s' \"\${$1:-}\""); }

# ---- Dienste: Kopfzeilen und Schalter ----
# head_val <Skript> <Feld>: Wert der Kopfzeile "# <Feld>: ..."
head_val(){ sed -n "1,12s/^# $2: *//p" "$1" | head -n 1; }

# svc_on <Skript>: Gruppe eingeschaltet? SERVICE_<GRUPPE> in config, sonst Vorgabe; "core" immer.
svc_on(){
  g=$(head_val "$1" group); [ -n "$g" ] || return 0
  [ "$g" = core ] && return 0
  G=$(echo "$g" | tr 'a-z-' 'A-Z_')
  v=$(cfg "SERVICE_$G")
  [ -z "$v" ] && [ "$g" = airplay ] && v=$(cfg AIRPLAY)   # alter Schalter
  [ -n "$v" ] || v=$(head_val "$1" default)
  [ "$v" != off ]
}

# svc_ready <Skript>: alle Programme aus "# requires:" vorhanden ($D/bin zuerst, dann PATH)?
svc_ready(){
  for p in $(head_val "$1" requires | tr ',' ' '); do
    command -v "$p" >/dev/null 2>&1 || return 1
  done
  return 0
}

# ---- Firewall ----
# fw_rules: gewünschte Regeln (eine je Zeile, Argumente für iptables -A LEUCHTFEUER)
fw_rules(){
  echo "-i lo -j RETURN"
  echo "-m state --state ESTABLISHED,RELATED -j RETURN"
  echo "-p tcp --dport 22 -j RETURN"
  echo "-p udp --dport 5353 -j RETURN"
  echo "-p udp --sport 67 --dport 68 -j RETURN"
  echo "-p icmp -j RETURN"
  target_fw_rules
  echo "-p tcp --dport $(cfg WEB_PORT | grep -E '^[0-9]+$' || echo 80) -j RETURN"
  [ "$(cfg WEB_TLS)" = on ] && echo "-p tcp --dport $(cfg WEB_TLS_PORT | grep -E '^[0-9]+$' || echo 443) -j RETURN"
  for s in $D/services/*.sh; do
    [ -x "$s" ] && svc_on "$s" && svc_ready "$s" || continue
    head_val "$s" ports | tr ',' '\n' | while read -r proto port _; do
      case $proto in tcp|udp) echo "-p $proto --dport $port -j RETURN" ;; esac
    done
  done
  for f in $D/ports.local $D/ports.leuchtfeuerd; do
    [ -f $f ] && grep -E '^(tcp|udp) [0-9:]+' $f | while read -r proto port _; do
      echo "-p $proto --dport $port -j RETURN"
    done
  done
  echo "-j DROP"
}

fw_on(){
  v=$(cfg FIREWALL); [ -n "$v" ] || v=$TARGET_FIREWALL
  [ "$v" = on ] && command -v iptables >/dev/null 2>&1
}

firewall(){
  if ! fw_on; then
    # ausgeschaltet: eine früher gesetzte Kette wieder entfernen
    [ -f $R/leuchtfeuer-fw.rules ] || return 0
    while iptables -D INPUT -j LEUCHTFEUER 2>/dev/null; do :; done
    iptables -F LEUCHTFEUER 2>/dev/null; iptables -X LEUCHTFEUER 2>/dev/null
    rm -f $R/leuchtfeuer-fw.rules; log "Firewall aus"
    return 0
  fi
  want=$(fw_rules | awk '!seen[$0]++')
  if iptables -C INPUT -j LEUCHTFEUER 2>/dev/null && [ "$want" = "$(cat $R/leuchtfeuer-fw.rules 2>/dev/null)" ]; then return 0; fi
  # neue Kette vollständig aufbauen, dann umhängen: es gibt keinen Moment ohne DROP am Ende
  iptables -N LEUCHTFEUER_NEW 2>/dev/null || iptables -F LEUCHTFEUER_NEW
  echo "$want" | while read -r r; do [ -n "$r" ] && iptables -A LEUCHTFEUER_NEW $r; done
  iptables -I INPUT 1 -j LEUCHTFEUER_NEW
  while iptables -D INPUT -j LEUCHTFEUER 2>/dev/null; do :; done
  iptables -F LEUCHTFEUER 2>/dev/null; iptables -X LEUCHTFEUER 2>/dev/null
  iptables -E LEUCHTFEUER_NEW LEUCHTFEUER
  # Kette der Versionen vor der Umbenennung (Oktober 2026) entfernen
  while iptables -D INPUT -j INVOKE 2>/dev/null; do :; done
  iptables -F INVOKE 2>/dev/null; iptables -X INVOKE 2>/dev/null
  echo "$want" > $R/leuchtfeuer-fw.rules
  log "Firewall gesetzt ($(echo "$want" | grep -c dport) Ports)"
}

# Uhr per NTP stellen (einmalig, im Hintergrund, höchstens 30 s); nicht, wenn das Gerät selbst einen Zeitdienst hat
time_sync(){
  # eigener Zeitdienst des Geräts (sntpd: Harman-Firmware des Invoke)
  pidof ntpd chronyd systemd-timesyncd sntpd >/dev/null 2>&1 && return 0
  busybox ntpd --help >/dev/null 2>&1 || { [ -e $R/leuchtfeuer-ntp.none ] || { log "kein busybox ntpd: Uhr wird nicht gestellt"; touch $R/leuchtfeuer-ntp.none; }; return 0; }
  s=$(cfg NTP_SERVER); s=${s:-pool.ntp.org}
  (
    busybox ntpd -n -q -p "$s" >/dev/null 2>&1 & p=$!
    i=0; while kill -0 $p 2>/dev/null && [ $i -lt 30 ]; do sleep 1; i=$((i + 1)); done
    if kill -0 $p 2>/dev/null; then kill $p; log "NTP $s: keine Antwort"
    elif wait $p; then date +%s > $R/leuchtfeuer-ntp.ok; log "Uhr per NTP ($s) gestellt"
    else log "NTP $s: Fehler"; fi
  ) &
}

# ---- Protokolle kürzen (copytruncate: laufende Dienste schreiben mit O_APPEND einfach weiter) ----
logsize(){ ls -ln "$1" 2>/dev/null | awk '{print $5}'; }
rotate_logs(){
  for f in $D/log/*.log $D/hook.log; do
    [ -f "$f" ] || continue
    [ "$(logsize "$f")" -gt 1048576 ] 2>/dev/null || continue
    tail -c 262144 "$f" > "$f.1" && : > "$f"
    echo "$(date '+%Y-%m-%d %H:%M:%S') Protokoll gekürzt, Rest in $(basename "$f").1" >> "$f"
  done
}

# ---- Dienste starten, überwachen, mit wachsender Pause neu starten ----
# Zustand je Dienst: "<Fehlschläge> <frühester Neustart> <Startzeit oder 0> <Neustarts gesamt>"
services(){
  now=$(date +%s)
  for s in $D/services/*.sh; do
    [ -x "$s" ] || continue
    n=$(basename "$s" .sh); pf=$R/leuchtfeuer-svc-$n.pid; sf=$R/leuchtfeuer-svc-$n.state; lf=$D/log/$n.log
    fails=0; next=0; started=0; restarts=0
    [ -f $sf ] && read -r fails next started restarts < $sf
    p=$(cat $pf 2>/dev/null)
    running=0; [ -n "$p" ] && kill -0 "$p" 2>/dev/null && running=1
    if ! svc_on "$s"; then
      if [ $running = 1 ]; then kill "$p" 2>/dev/null; log "Dienst $n ausgeschaltet"; fi
      rm -f $pf; echo "0 0 0 $restarts" > $sf; echo off > $R/leuchtfeuer-svc-$n.mode
      continue
    fi
    if ! svc_ready "$s"; then
      [ "$(cat $R/leuchtfeuer-svc-$n.mode 2>/dev/null)" = missing ] || log "Dienst $n: Programm fehlt ($(head_val "$s" requires))"
      echo missing > $R/leuchtfeuer-svc-$n.mode
      continue
    fi
    rm -f $R/leuchtfeuer-svc-$n.mode
    if [ $running = 1 ]; then
      # läuft stabil seit 5 Minuten: Fehlschläge vergessen
      if [ "$fails" -gt 0 ] && [ $((now - started)) -gt 300 ]; then echo "0 0 $started $restarts" > $sf; fi
      continue
    fi
    if [ "$started" -gt 0 ]; then   # gerade gestorben
      if [ -e $R/leuchtfeuer-svc-$n.expected ]; then
        rm -f $R/leuchtfeuer-svc-$n.expected; next=0
      else
        if [ $((now - started)) -lt 120 ]; then fails=$((fails + 1)); else fails=0; fi
        delay=0
        [ "$fails" -ge 2 ] && { delay=30; i=2; while [ $i -lt "$fails" ] && [ $delay -lt 1800 ]; do delay=$((delay * 2)); i=$((i + 1)); done; }
        [ $delay -gt 1800 ] && delay=1800
        next=$((now + delay))
        if [ $delay -gt 0 ]; then log "Dienst $n stirbt wiederholt ($fails x): nächster Versuch in $delay s"
        else log "Dienst $n beendet"; fi
      fi
      started=0
    fi
    if [ "$now" -lt "$next" ]; then echo "$fails $next 0 $restarts" > $sf; continue; fi
    mkdir -p $D/log
    setsid "$s" >>$lf 2>&1 </dev/null &
    echo $! > $pf
    [ -f $sf ] && restarts=$((restarts + 1))
    echo "$fails 0 $now $restarts" > $sf
    log "Dienst $n gestartet (pid $!)"
  done
}

# ---- Update beobachten: wiederholt ausfallende Dienste innerhalb von 10 Minuten -> vorigen Stand zurück ----
update_watch(){
  [ -f $D/update-pending ] || return 0
  t=$(head -n 1 $D/update-pending 2>/dev/null); now=$(date +%s)
  case $t in ''|*[!0-9]*) t=$now; echo $t > $D/update-pending ;; esac
  for sf in $R/leuchtfeuer-svc-*.state; do
    [ -f "$sf" ] || continue
    read -r fails _ < "$sf"
    if [ "${fails:-0}" -ge 3 ]; then
      n=${sf#$R/leuchtfeuer-svc-}; n=${n%.state}
      log "Update: Dienst $n fällt nach dem Update wiederholt aus – vorigen Stand wiederherstellen"
      [ -x $D/apply-update.sh ] && setsid sh $D/apply-update.sh rollback "$n" </dev/null >>$D/hook.log 2>&1 &
      return 0
    fi
  done
  if [ $((now - t)) -gt 600 ]; then rm -f $D/update-pending; log "Update bestätigt (10 Minuten ohne Ausfall)"; fi
}

# ---- Hardware-Watchdog ----
WDDEV=${LEUCHTFEUER_WATCHDOG_DEV:-/dev/watchdog}
uptime_s(){ cut -d' ' -f1 /proc/uptime | cut -d. -f1; }
watchdog_ctl(){
  pf=$R/leuchtfeuer-watchdog.pid
  p=$(cat $pf 2>/dev/null); running=0
  [ -n "$p" ] && kill -0 "$p" 2>/dev/null && running=1
  if [ "$(cfg WATCHDOG)" != on ] || [ ! -e "$WDDEV" ]; then
    if [ $running = 1 ]; then kill "$p"; log "Watchdog aus"; fi
    rm -f $pf; return 0
  fi
  if [ $running = 1 ]; then
    # 30 Minuten stabil: Zähler der unruhigen Starts zurücksetzen
    if [ "$(uptime_s)" -gt 1800 ] && [ "$(cat $D/watchdog-unstable 2>/dev/null || echo 0)" != 0 ]; then echo 0 > $D/watchdog-unstable; fi
    return 0
  fi
  n=$(cat $D/watchdog-unstable 2>/dev/null); n=${n:-0}
  case $n in ''|*[!0-9]*) n=0 ;; esac
  if [ "$n" -ge 3 ]; then
    [ -e $R/leuchtfeuer-watchdog.blocked ] || { log "Watchdog: 3 Starts ohne stabile Laufzeit - bleibt aus (zurücksetzen: rm $D/watchdog-unstable)"; touch $R/leuchtfeuer-watchdog.blocked; }
    return 0
  fi
  [ -x $D/bin/leuchtfeuerd ] || return 0
  # einmal je Gerätestart zählen (ein Neustart des Hooks oder ein Update zählt nicht)
  [ -e $R/leuchtfeuer-watchdog.boot ] || { echo $((n + 1)) > $D/watchdog-unstable; touch $R/leuchtfeuer-watchdog.boot; }
  mkdir -p $D/log
  setsid $D/bin/leuchtfeuerd -watchdog "$R/leuchtfeuer-hook.alive" -watchdog-dev "$WDDEV" >>$D/log/watchdog.log 2>&1 </dev/null &
  echo $! > $pf
  log "Watchdog scharf (pid $!)"
}

# ---- ersetzte Klänge der Hersteller-Software ----
sounds_mount(){
  m=$D/sounds/vendor.map
  [ -f "$m" ] || return 0
  tab=$(printf '\t')
  while IFS="$tab" read -r orig file; do
    [ -n "$orig" ] && [ -f "$D/sounds/$file" ] && [ -f "$orig" ] || continue
    grep -q " $orig " /proc/mounts && continue
    mount --bind "$D/sounds/$file" "$orig" && log "Klang $orig ersetzt" || log "FEHLER: Klang $orig nicht eingehängt"
  done < "$m"
}

# Watchdog ordentlich schließen (Notbremse): SIGTERM -> "V" -> kein Neustart
watchdog_stop(){
  p=$(cat $R/leuchtfeuer-watchdog.pid 2>/dev/null)
  [ -n "$p" ] && kill "$p" 2>/dev/null && log "Watchdog geschlossen"
  rm -f $R/leuchtfeuer-watchdog.pid
}

# Installationsverzeichnis in der Tonkette: assemble.sh baut den Pfad des Ziels ein (Kopfzeile "# leuchtfeuer-dir:"); liegt die
# Installation woanders (setup.sh --dir), schreibt der Hook ihn um, bevor ein Dienst die Datei liest.
asound_dir(){
  f=$D/asound-music.conf
  baked=$(sed -n '1s/^# leuchtfeuer-dir: *//p' "$f" 2>/dev/null)
  [ -n "$baked" ] && [ "$baked" != "$D" ] || return 0
  sed -e "1s|.*|# leuchtfeuer-dir: $D|" -e "2,\$s|$baked/|$D/|g" "$f" > "$f.new" && mv "$f.new" "$f" && log "Tonkette: Pfad $baked -> $D"
}

# ---- Zielgerät ----
TARGET_ID=generic TARGET_NAME=Leuchtfeuer TARGET_FIREWALL=off TARGET_IFACE='' TARGET_ALSA_BASE='' TARGET_DBUS=''
target_init(){ :; }
target_tick(){ :; }
target_fw_rules(){ :; }
target_net(){ :; }
# shellcheck disable=SC1091
[ -f $D/target.sh ] && . $D/target.sh

# Umgebung der Dienste (und von leuchtfeuerd)
iface=$(cfg WIFI_IFACE); [ -n "$iface" ] || iface=$TARGET_IFACE
[ -n "$iface" ] || iface=$(ip route 2>/dev/null | awk '/^default/{for(i=1;i<NF;i++) if($i=="dev"){print $(i+1); exit}}')
card=$(cfg ALSA_CARD)
export LEUCHTFEUER_DIR=$D LEUCHTFEUER_RUN=$R LEUCHTFEUER_TARGET=$TARGET_ID LEUCHTFEUER_NAME="$TARGET_NAME" \
  WIFI_IFACE="${iface:-wlan0}" ALSA_CONFIG=$D/asound-music.conf PATH="$D/bin:$PATH"
[ -n "$card" ] && export ALSA_CARD="$card"
[ -n "$TARGET_DBUS" ] && export DBUS_SYSTEM_BUS_ADDRESS="$TARGET_DBUS"
# ohne Hersteller-asound.conf, die ALSA_CONFIG einbindet: die Tonkette hinter die Grundkonfiguration der alsa-lib hängen
[ -n "$TARGET_ALSA_BASE" ] && [ -f "$TARGET_ALSA_BASE" ] && export ALSA_CONFIG_PATH="$TARGET_ALSA_BASE:$D/asound-music.conf"
out=$(cfg ALSA_OUTPUT)
[ -n "$out" ] && export LEUCHTFEUER_OUT="$out"

# Nur beim Laden als Bibliothek (Tests: HOOK_LIB=1) hier aufhören
[ -n "${HOOK_LIB:-}" ] && return 0 2>/dev/null

asound_dir
sounds_mount
target_init
# Wake-up von leuchtfeuerd (SIGUSR1) nach einem Wechsel der Ausgabe: Dienste sofort neu starten statt in bis zu 30 s
trap ':' USR1
tick=0
while :; do
  [ -e $D/disable-hook ] && { watchdog_stop; log "disable-hook gesetzt – Ende"; exit 0; }
  [ -f $D/output.conf ] || : > $D/output.conf   # von asound-music.conf eingebunden (Ausgabe), darf nie fehlen
  target_tick
  firewall
  uptime_s > $R/leuchtfeuer-hook.alive
  sounds_mount
  services
  update_watch
  watchdog_ctl
  # beim ersten Durchlauf mit Adresse, danach alle 6 h (720 x 30 s); Protokolle alle 5 Minuten
  if [ $tick -le 0 ]; then target_net && { time_sync; tick=720; }; fi
  [ $((tick % 10)) = 0 ] && rotate_logs
  tick=$((tick - 1))
  sleep 30 & sp=$!
  wait $sp 2>/dev/null; kill $sp 2>/dev/null
done
