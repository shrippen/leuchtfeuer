#!/bin/sh
# Dauerhafte Einstellungen des Invoke (läuft als root, gestartet von boot.sh, prüft alle 30 s):
#  - eigenes dropbear (ed25519, nur Schlüssel, eigener Host-Schlüssel) statt des
#    Original-sshd (dropbear 2016.72, Host-Schlüssel auf allen Geräten gleich)
#  - authorized_keys aus /data/leuchtfeuer nach /home/root/.ssh (tmpfs über dem ro-SquashFS)
#  - Firewall (Kette LEUCHTFEUER vor INPUT): SSH, mDNS, DHCP-Antworten, ICMP, bestehende Verbindungen, die Weboberfläche
#    und die Ports der EINGESCHALTETEN Dienste (Kopfzeile "# ports:" der Dienstskripte); am Setup-AP (p2p0)
#    zusätzlich die Einrichtungs-Ports; eigene Ports aus /data/leuchtfeuer/ports.local ("tcp 1234" / "udp 5678") und die von
#    leuchtfeuerd geschriebenen aus /data/leuchtfeuer/ports.leuchtfeuerd (Sprachassistent).
#    Ändert sich etwas (Dienst an/aus, ports.local), wird die Kette neu aufgebaut, offene Ports schließen sich wieder.
#  - IPv6 aus (Kernel ohne ip6tables)
#  - adbd (Port 5555, root ohne Anmeldung) aus, sobald /data/leuchtfeuer/disable-adb existiert
#  - DHCP-Name aus DHCP_HOSTNAME (z. B. "invoke" -> invoke.lan im Router): das Libre-dhcpcd meldet "LibreSync-<nr>",
#    das der Router nicht einträgt. Deshalb nach dem Start und alle 6 h eine zusätzliche DHCP-Anfrage mit busybox
#    udhcpc (-s /bin/true: ändert nichts an der Schnittstelle, fragt nur dieselbe Adresse mit dem Namen an).
#  - Uhrzeit: nach dem Start und alle 6 h einmal per NTP stellen (busybox ntpd -q, Server NTP_SERVER), falls auf dem
#    Gerät kein ntpd läuft. Wecker hängen an der richtigen Uhr.
#  - Harman-Dienste kürzen: /data/leuchtfeuer/podium.conf per Bind-Mount über /etc/podium/podium.conf, dann init-Dienst
#    "podium" (system-manager) einmal neu starten (ohne Cortana, Harman-Spotify, OTA, Absturzbericht-Upload)
#  - Dienste: jedes ausführbare /data/leuchtfeuer/services/<name>.sh (endet mit exec) wird gestartet, wenn seine Gruppe
#    eingeschaltet ist (Kopfzeile "# group:", Schalter SERVICE_<GRUPPE>="on|off" in config, Vorgabe "# default:").
#    Stirbt ein Dienst kurz nach dem Start immer wieder, wartet der Hook zunehmend länger (30 s ... 30 min) und
#    meldet ihn als fehlerhaft; Zustand in /run/leuchtfeuer-svc-<name>.state ("Fehlschläge nächsterStart gestartet Neustarts").
#    Ein absichtliches Beenden (leuchtfeuerd: Neustart-Knopf) kündigt /run/leuchtfeuer-svc-<name>.expected an und zählt nicht.
#  - Protokolle: /data/leuchtfeuer/log/<name>.log und hook.log werden ab 1 MiB gekürzt (die letzten 256 KiB -> .1); das geht auch
#    bei laufenden Diensten, die Datei bleibt dieselbe.
#  - Update-Rückfall: Nach einem Update (/data/leuchtfeuer/update-pending) beobachtet der Hook 10 Minuten lang die Dienste;
#    fällt einer wiederholt aus, stellt apply-update.sh den vorigen Stand wieder her.
#  - Hardware-Watchdog (WATCHDOG="on" in config, falls /dev/watchdog da ist): "leuchtfeuerd -watchdog" setzt die Frist auf
#    60 s und füttert ihn nur, solange dieser Hook läuft (Lebenszeichen $R/leuchtfeuer-hook.alive je Durchlauf). Hängt das
#    System oder der Hook, startet das Gerät neu. Schutz vor einer Neustart-Schleife: Nach 3 Starts mit scharfem
#    Watchdog ohne 30 Minuten stabile Laufzeit bleibt er aus (zurücksetzen: /data/leuchtfeuer/watchdog-unstable löschen).
#  - Klänge: von leuchtfeuerd ersetzte Klänge der Hersteller-Software (sounds/vendor.map: "<Original>\t<Datei>") per
#    Bind-Mount einhängen, beim Start vor dem Neustart der Hersteller-Dienste (die öffnen die Dateien dann neu).
# Notbremse: /data/leuchtfeuer/disable-hook anlegen -> Skript macht nichts.
D=${LEUCHTFEUER_DIR:-/data/leuchtfeuer}
R=${LEUCHTFEUER_RUN:-/run}   # Laufzeit-Dateien (Tests setzen beides um)
PIDF=$R/leuchtfeuer-dropbear.pid
echo $$ > $R/leuchtfeuer-hook.pid
log(){ echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >> $D/hook.log; }
[ -e $D/disable-hook ] && { log "disable-hook gesetzt – nichts zu tun"; exit 0; }
log "hook gestartet (pid $$)"

# Wert aus der Konfiguration (sh-Datei) lesen, ohne sie im Hook-Prozess zu laden
cfg(){ (. $D/config 2>/dev/null; eval "printf '%s' \"\${$1:-}\""); }

setup_home(){
  grep -q ' /home/root tmpfs ' /proc/mounts || mount -t tmpfs -o mode=700,size=1m tmpfs /home/root
  mkdir -p /home/root/.ssh && chmod 700 /home/root/.ssh
  cmp -s $D/authorized_keys /home/root/.ssh/authorized_keys || {
    cp $D/authorized_keys /home/root/.ssh/authorized_keys; chmod 600 /home/root/.ssh/authorized_keys
    log "authorized_keys aktualisiert"; }
}

ours_up(){ p=$(cat $PIDF 2>/dev/null); [ -n "$p" ] && kill -0 "$p" 2>/dev/null; }

ssh_up(){
  ours_up && return 0
  stop sshd; sleep 1
  $D/dropbearmulti dropbear -p 22 -r $D/host_ed25519 -r $D/host_ecdsa -P $PIDF 2>>$D/hook.log
  sleep 2
  if ours_up; then log "eigenes dropbear läuft (pid $(cat $PIDF))"; return 0; fi
  log "FEHLER: eigenes dropbear startet nicht – Original-sshd und adbd wieder an"
  start sshd; start adbd; return 1
}

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

# ---- Firewall ----
# fw_rules: gewünschte Regeln (eine je Zeile, Argumente für iptables -A LEUCHTFEUER)
fw_rules(){
  echo "-i lo -j RETURN"
  echo "-m state --state ESTABLISHED,RELATED -j RETURN"
  echo "-p tcp --dport 22 -j RETURN"
  echo "-p udp --dport 5353 -j RETURN"
  echo "-p udp --sport 67 --dport 68 -j RETURN"
  echo "-p icmp -j RETURN"
  for p in 443 12345 53; do echo "-i p2p0 -p tcp --dport $p -j RETURN"; done
  for p in 67 53 48301; do echo "-i p2p0 -p udp --dport $p -j RETURN"; done
  echo "-p tcp --dport $(cfg WEB_PORT | grep -E '^[0-9]+$' || echo 80) -j RETURN"
  [ "$(cfg WEB_TLS)" = on ] && echo "-p tcp --dport $(cfg WEB_TLS_PORT | grep -E '^[0-9]+$' || echo 443) -j RETURN"
  for s in $D/services/*.sh; do
    [ -x "$s" ] && svc_on "$s" || continue
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

firewall(){
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

ipv6_off(){
  for f in /proc/sys/net/ipv6/conf/*/disable_ipv6; do
    [ "$(cat $f)" = 1 ] || echo 1 > $f
  done
}

adb_off(){
  [ -e $D/disable-adb ] || return 0
  # getprop braucht ANDROID_PROPERTY_WORKSPACE (fehlt hier), stop geht über den init-Socket
  pidof adbd >/dev/null 2>&1 && { stop adbd; sleep 1; log "adbd gestoppt"; }
}

dhcp_name(){
  n=$(cfg DHCP_HOSTNAME)
  [ -n "$n" ] || n=$(cat $D/hostname 2>/dev/null)
  [ -n "$n" ] || return 0
  ip=$(ip -4 addr show wlan0 2>/dev/null | awk '/inet /{sub(/\/.*/,"",$2); print $2; exit}')
  [ -n "$ip" ] || return 1
  if busybox udhcpc -i wlan0 -f -q -n -t 4 -T 3 -r "$ip" -x hostname:"$n" -F "$n" -s /bin/true >/dev/null 2>&1; then
    log "DHCP-Name $n für $ip gemeldet"; return 0
  fi
  log "DHCP-Name $n: keine Antwort"; return 1
}

# Uhr per NTP stellen (einmalig, im Hintergrund, höchstens 30 s); nicht, wenn das Gerät selbst einen ntpd hat
time_sync(){
  pidof ntpd >/dev/null 2>&1 && return 0
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

podium_trim(){
  [ -s $D/podium.conf ] || return 0
  grep -q ' /etc/podium/podium.conf ' /proc/mounts && return 0
  mount --bind $D/podium.conf /etc/podium/podium.conf || { log "FEHLER: Bind-Mount podium.conf"; return 1; }
  stop podium; sleep 3; start podium
  log "Harman-Dienste mit gekürzter podium.conf neu gestartet"
}

# Aktueller CA-Bestand (der eingebaute ist von 2018 und kennt Let's Encrypt nicht): Musikdienste
# wie gmrender laden sonst https-Adressen (Navidrome u. a.) nicht. Quelle: tools/update-ca-bundle.sh
ca_bundle(){
  [ -s $D/ca-certificates.crt ] || return 0
  grep -q ' /etc/ssl/certs/ca-certificates.crt ' /proc/mounts && return 0
  mount --bind $D/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt || { log "FEHLER: Bind-Mount CA-Bestand"; return 1; }
  log "CA-Bestand aus $D eingebunden"
}

# Watchdog ordentlich schließen (Notbremse): SIGTERM -> "V" -> kein Neustart
watchdog_stop(){
  p=$(cat $R/leuchtfeuer-watchdog.pid 2>/dev/null)
  [ -n "$p" ] && kill "$p" 2>/dev/null && log "Watchdog geschlossen"
  rm -f $R/leuchtfeuer-watchdog.pid
}

# Nur beim Laden als Bibliothek (Tests: HOOK_LIB=1) hier aufhören
[ -n "${HOOK_LIB:-}" ] && return 0 2>/dev/null

sounds_mount
podium_trim
ca_bundle
tick=0
while :; do
  [ -e $D/disable-hook ] && { watchdog_stop; log "disable-hook gesetzt – Ende"; exit 0; }
  setup_home
  ssh_up && adb_off
  firewall
  ipv6_off
  uptime_s > $R/leuchtfeuer-hook.alive
  sounds_mount
  services
  update_watch
  watchdog_ctl
  # beim ersten Durchlauf mit Adresse, danach alle 6 h (720 x 30 s); Protokolle alle 5 Minuten
  if [ $tick -le 0 ]; then dhcp_name && { time_sync; tick=720; }; fi
  [ $((tick % 10)) = 0 ] && rotate_logs
  tick=$((tick - 1))
  sleep 30
done
