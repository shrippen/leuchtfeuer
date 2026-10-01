#!/usr/bin/env bash
# Checks over SSH that Leuchtfeuer is fully running / Prüft per SSH, ob Leuchtfeuer vollständig läuft.
#   scripts/verify-install.sh [--ip IP] [--key ~/.ssh/id_ed25519.pub]
set -uo pipefail
cd "$(dirname "$0")/.."
. scripts/lib.sh
IP=""; KEY=""
while [ $# -gt 0 ]; do case $1 in --ip) IP=$2; shift 2;; --key) KEY=$2; shift 2;; *) echo "?: $1" >&2; exit 2;; esac; done
[ -n "$IP" ] || ask IP "IP address of the speaker" "IP-Adresse des Lautsprechers"
[ -n "$IP" ] || die "--ip is missing" "--ip fehlt"
[ -n "$KEY" ] || { [ "$INTERACTIVE" = 1 ] && choose_key; }
ID=()
if [ -n "$KEY" ]; then
  if [ -f "${KEY%.pub}" ]; then ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
fi
S(){ ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new "${ID[@]}" root@"$IP" "$@"; }
fail=0
bad(){ printf '  %sMISSING%s %s\n' "$C_R" "$C_N" "$(t "$1" "${2:-}")"; fail=1; }
S true 2>/dev/null || { bad "SSH access (key) does not work" "SSH-Zugang (Schlüssel) geht nicht"; exit 1; }
ok "SSH with key" "SSH mit Schlüssel"
ps=$(S 'ps' 2>/dev/null)
# nur eingeschaltete Dienste prüfen (Kopfzeilen "# group:"/"# process:" der Dienstskripte, Schalter SERVICE_<GRUPPE>)
svc=$(S 'cd /data/leuchtfeuer; for s in services/*.sh; do [ -x "$s" ] || continue
  g=$(sed -n "1,12s/^# group: *//p" "$s"); p=$(sed -n "1,12s/^# process: *//p" "$s"); d=$(sed -n "1,12s/^# default: *//p" "$s")
  G=$(echo "$g" | tr "a-z-" "A-Z_"); v=$(. ./config 2>/dev/null; eval "echo \${SERVICE_$G:-}")
  [ -z "$v" ] && [ "$g" = airplay ] && v=$(. ./config 2>/dev/null; echo "${AIRPLAY:-}")
  [ "$g" = core ] && v=on; echo "$p ${v:-$d}"; done' 2>/dev/null)
while read -r p state; do
  [ -n "$p" ] || continue
  if [ "$state" = off ]; then info "  --    $p switched off" "  --    $p ausgeschaltet"; continue; fi
  if echo "$ps" | grep -q "[/ ]$p"; then ok "process $p" "Prozess $p"; else bad "process $p is not running" "Prozess $p läuft nicht"; fi
done <<< "$svc"
if S 'cat /run/leuchtfeuer-svc-*.state 2>/dev/null | awk "\$1>=3{f=1} END{exit !f}"'; then
  bad "a service keeps failing (see web interface > Settings > Services)" "ein Dienst fällt wiederholt aus (Weboberfläche > Einstellungen > Dienste)"
fi
if echo "$ps" | grep -q "tidal_connect"; then ok "process tidal_connect_application" "Prozess tidal_connect_application"
else info "  --    Tidal Connect not installed/started (optional)" "  --    Tidal Connect nicht installiert/gestartet (optional)"; fi
hci=$(S 'LD_LIBRARY_PATH=/data/leuchtfeuer/bluez/lib /data/leuchtfeuer/bluez/bin/hciconfig hci0 2>&1')
if echo "$hci" | grep -q "UP RUNNING"; then ok "Bluetooth adapter hci0 is up" "Bluetooth-Adapter hci0 oben"; else bad "Bluetooth adapter hci0 is not up" "Bluetooth-Adapter hci0 nicht oben"; fi
if echo "$hci" | grep -q "ISCAN"; then ok "Bluetooth visible (ready to pair)" "Bluetooth sichtbar (koppelbereit)"; else bad "Bluetooth not visible" "Bluetooth nicht sichtbar"; fi
if S 'iptables -S LEUCHTFEUER 2>/dev/null | grep -q -- "--dport 22"'; then ok "firewall chain LEUCHTFEUER" "Firewall-Kette LEUCHTFEUER"; else bad "firewall chain LEUCHTFEUER is missing" "Firewall-Kette LEUCHTFEUER fehlt"; fi
for so in leuchtfeuer-viz-tap.so leuchtfeuer-eq.so; do
  if S "test -f /data/leuchtfeuer/lib/ladspa/$so"; then ok "LADSPA plugin $so" "LADSPA-Plugin $so"; else bad "LADSPA plugin $so is missing (asound-music.conf needs it: no sound without it)" "LADSPA-Plugin $so fehlt (asound-music.conf braucht es: sonst kein Ton)"; fi
done
if echo "$ps" | grep -q "[a]udio-ui"; then ok "vendor audio-ui" "Hersteller-Dienst audio-ui"; else bad "audio-ui is not running (no volume knob, no web interface volume)" "audio-ui läuft nicht (kein Drehrad, keine Lautstärke in der Weboberfläche)"; fi
if S 'amixer -c 0 sget "Leuchtfeuer Music" >/dev/null 2>&1'; then ok 'volume control "Leuchtfeuer Music"' 'Lautstärkeregler "Leuchtfeuer Music"'; else bad 'volume control "Leuchtfeuer Music" is missing' 'Regler "Leuchtfeuer Music" fehlt'; fi
if S 'amixer -c 0 sget "Quelle spotify" >/dev/null 2>&1'; then ok 'source controls ("Quelle ...")' 'Quellen-Regler ("Quelle ...")'; else bad 'source controls are missing (leuchtfeuerd creates them at start)' 'Quellen-Regler fehlen (leuchtfeuerd legt sie beim Start an)'; fi
if S 'test -f /dev/shm/leuchtfeuer-eq'; then ok "sound settings shared with the plugin" "Klang-Einstellungen für das Plugin"; else bad "/dev/shm/leuchtfeuer-eq is missing (leuchtfeuerd writes it)" "/dev/shm/leuchtfeuer-eq fehlt (schreibt leuchtfeuerd)"; fi
if S 'test -S /run/leuchtfeuer-events.sock'; then ok "event socket for Spotify/AirPlay" "Ereignis-Socket für Spotify/AirPlay"; else bad "/run/leuchtfeuer-events.sock is missing" "/run/leuchtfeuer-events.sock fehlt"; fi
if S 'ps | grep -q "[a]dbd"'; then bad "adbd is running (port 5555 = root shell without login)" "adbd läuft (Port 5555 = Root-Shell ohne Anmeldung)"; else ok "adbd is off" "adbd aus"; fi
for port in 80; do
  if (timeout 4 bash -c "echo > /dev/tcp/$IP/$port") 2>/dev/null; then ok "port $port/tcp reachable" "Port $port/tcp erreichbar"
  else bad "port $port/tcp not reachable" "Port $port/tcp nicht erreichbar"; fi
done
# Ports der eingeschalteten Dienste (aus der Firewall-Kette)
for port in $(S 'iptables -S LEUCHTFEUER 2>/dev/null' | sed -n 's/.*-p tcp .*--dport \([0-9]*\) .*/\1/p' | sort -u); do
  case $port in 22|53|443|12345) continue ;; esac
  if (timeout 4 bash -c "echo > /dev/tcp/$IP/$port") 2>/dev/null; then ok "port $port/tcp reachable" "Port $port/tcp erreichbar"
  else bad "port $port/tcp not reachable" "Port $port/tcp nicht erreichbar"; fi
done
if [ $fail = 0 ]; then info "All good." "Alles in Ordnung."
else info "There are problems (see above; logs: /data/leuchtfeuer/log/ on the speaker)." "Es gibt Probleme (siehe oben; Logs: /data/leuchtfeuer/log/ auf dem Lautsprecher)."; fi
exit $fail
