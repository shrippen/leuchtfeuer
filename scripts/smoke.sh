#!/usr/bin/env bash
# Gerätetest nach einer Installation oder einem Update / device test after an install or update.
#   scripts/smoke.sh --ip IP [--key ~/.ssh/id_ed25519.pub] [--token lf_...] [--listen] [--report datei.md]
#
# Prüft über SSH (und mit --token über die Web-API), was die Tests auf dem Rechner nicht können: Tonkette je Quelle,
# Regler, Klang-Plugin, Firewall, Uhr, Watchdog, Mikrofone (Aufnahme von jedem Gerät, Pegel), Programme, Last.
# Mit --listen fragt es nach dem, was nur ein Mensch hört (Gong, Webradio, Überblenden, Durchsage, Raumfilter).
# Ergebnis: Liste auf dem Bildschirm und ein Bericht als Markdown (Standard: smoke-<IP>-<Datum>.md).
# Ändert nichts dauerhaft: Test-Töne, ein kurzer Sender, die Einstellungen bleiben.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
. scripts/lib.sh
IP=""; KEY=""; TOKEN=""; LISTEN=0; REPORT=""
while [ $# -gt 0 ]; do case $1 in
  --ip) IP=$2; shift 2;; --key) KEY=$2; shift 2;; --token) TOKEN=$2; shift 2;; --listen) LISTEN=1; shift;;
  --report) REPORT=$2; shift 2;; --non-interactive) INTERACTIVE=0; shift;;
  -h|--help) sed -n '2,9p' "$0"; exit 0;; *) echo "?: $1" >&2; exit 2;; esac; done
[ -n "$IP" ] || ask IP "IP address of the speaker" "IP-Adresse des Lautsprechers"
[ -n "$IP" ] || die "--ip is missing" "--ip fehlt"
[ -n "$KEY" ] || { [ "$INTERACTIVE" = 1 ] && choose_key; }
ID=()
if [ -n "$KEY" ]; then
  if [ -f "${KEY%.pub}" ]; then ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
fi
S(){ ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new "${ID[@]}" root@"$IP" "$@"; }
REPORT=${REPORT:-smoke-$IP-$(date +%Y%m%d-%H%M).md}
WEB="http://$IP"
NOK=0; NWARN=0; NFAIL=0
{ echo "# Leuchtfeuer-Gerätetest $IP"; echo; date '+%Y-%m-%d %H:%M'; echo; } > "$REPORT"
rep(){ printf '%s\n' "$*" >> "$REPORT"; }
pass(){ NOK=$((NOK + 1)); ok "$1" "${2:-}"; rep "- OK: $(t "$1" "${2:-}")"; }
soft(){ NWARN=$((NWARN + 1)); printf '  %sWARN%s  %s\n' "$C_Y" "$C_N" "$(t "$1" "${2:-}")"; rep "- WARN: $(t "$1" "${2:-}")"; }
bad(){ NFAIL=$((NFAIL + 1)); printf '  %sFAIL%s  %s\n' "$C_R" "$C_N" "$(t "$1" "${2:-}")"; rep "- FAIL: $(t "$1" "${2:-}")"; }
detail(){ rep ""; rep '```'; printf '%s\n' "$1" | head -n "${2:-40}" >> "$REPORT"; rep '```'; rep ""; }
section(){ say "$1" "${2:-}"; rep ""; rep "## $(t "$1" "${2:-}")"; }
api(){ curl -fsS -m 10 -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }
apost(){ api -X POST -d "$2" "$WEB$1" >/dev/null; }

section "Access" "Zugang"
S true 2>/dev/null || { bad "SSH with key does not work" "SSH mit Schlüssel geht nicht"; echo; info "Report: $REPORT" "Bericht: $REPORT"; exit 1; }
pass "SSH with key" "SSH mit Schlüssel"
if S 'p=$(cat /run/invoke-hook.pid 2>/dev/null); [ -n "$p" ] && kill -0 $p'; then pass "hook is running" "Hook läuft"; else bad "hook is not running" "Hook läuft nicht"; fi
if S 'ps | grep -q "[a]dbd"'; then bad "adbd is running" "adbd läuft"; else pass "adbd is off" "adbd aus"; fi
if S 'iptables -S INVOKE | tail -n 1 | grep -q -- "-j DROP"'; then pass "firewall ends with DROP" "Firewall endet mit DROP"; else bad "firewall chain INVOKE incomplete" "Firewall-Kette INVOKE unvollständig"; fi
if [ -n "$TOKEN" ]; then
  if api "$WEB/api/status" >/dev/null; then pass "web API with key" "Web-API mit Schlüssel"; else bad "web API with key fails" "Web-API mit Schlüssel geht nicht"; TOKEN=""; fi
  if [ -n "$TOKEN" ] && api "$WEB/metrics" | grep -q '^leuchtfeuer_info'; then pass "/metrics" "/metrics"; else [ -n "$TOKEN" ] && bad "/metrics without values" "/metrics ohne Werte"; fi
  h=$(curl -fsSI -m 10 -H "Authorization: Bearer $TOKEN" "$WEB/api/status" 2>/dev/null | tr -d '\r')
  if echo "$h" | grep -qi '^content-security-policy:'; then pass "security headers" "Sicherheits-Header"; else bad "security headers missing" "Sicherheits-Header fehlen"; fi
  code=$(curl -s -o /dev/null -w '%{http_code}' -m 10 -X POST -H 'Origin: http://evil.example' -H 'Content-Type: application/json' -d '{"password":"x"}' "$WEB/api/login")
  [ "$code" = 403 ] && pass "foreign origin rejected" "fremde Herkunft abgewiesen" || bad "foreign origin not rejected ($code)" "fremde Herkunft nicht abgewiesen ($code)"
else
  soft "no --token: API checks skipped (create a key under System > Access)" "kein --token: API-Prüfungen übersprungen (Schlüssel unter System > Zugang anlegen)"
fi

section "Services" "Dienste"
st=$(S 'for f in /run/invoke-svc-*.state; do [ -f "$f" ] || continue; n=${f#/run/invoke-svc-}; n=${n%.state}
  m=$(cat /run/invoke-svc-$n.mode 2>/dev/null); p=$(cat /run/invoke-svc-$n.pid 2>/dev/null); r=0; [ -n "$p" ] && kill -0 $p 2>/dev/null && r=1
  echo "$n ${m:-on} $r $(cat $f)"; done' 2>/dev/null)
while read -r n mode run fails _next _started restarts; do
  [ -n "$n" ] || continue
  if [ "$mode" = off ]; then rep "- --: $n $(t off aus)"; continue; fi
  if [ "$run" = 1 ] && [ "${fails:-0}" -lt 2 ]; then pass "$n running (restarts: ${restarts:-0})" "$n läuft (Neustarts: ${restarts:-0})"
  elif [ "$run" = 1 ]; then soft "$n running, but failed $fails times before" "$n läuft, fiel aber vorher $fails-mal aus"
  else bad "$n not running (failures: ${fails:-?})" "$n läuft nicht (Ausfälle: ${fails:-?})"; detail "$(S "tail -n 30 /data/invoke/log/$n.log" 2>/dev/null)"; fi
done <<< "$st"
v=$(S '/data/invoke/bin/snapclient --version 2>&1 | head -n 1')
[ -n "$v" ] && pass "snapclient starts ($v)" "snapclient startet ($v)" || soft "snapclient does not start (old kernel? PIE?)" "snapclient startet nicht (alter Kernel? PIE?)"

section "Sound chain" "Tonkette"
A='export ALSA_CONFIG=/data/invoke/asound-music.conf;'
for p in music announce radio spotify upnp cast airplay bluetooth sendspin tidal snapcast; do
  if S "$A aplay -q -D invoke_$p -d 1 -f S16_LE -r 44100 -c 2 /dev/zero" 2>/dev/null; then pass "PCM invoke_$p opens (44.1 kHz)" "PCM invoke_$p öffnet (44,1 kHz)"
  else bad "PCM invoke_$p does not open" "PCM invoke_$p öffnet nicht"; detail "$(S "$A aplay -D invoke_$p -d 1 -f S16_LE -r 44100 -c 2 /dev/zero 2>&1")"; fi
done
ctl=$(S 'amixer -c 0 scontrols' 2>/dev/null)
for c in "Invoke Music" "Invoke Announce" "Quelle spotify" "Quelle radio" "Quelle bluetooth"; do
  echo "$ctl" | grep -q "'$c'" && pass "control \"$c\"" "Regler \"$c\"" || bad "control \"$c\" missing" "Regler \"$c\" fehlt"
done
m=$(S 'head -c 4 /dev/shm/invoke-eq 2>/dev/null')
[ "$m" = IEQ2 ] && pass "sound settings (IEQ2) for the plugin" "Klang-Einstellungen (IEQ2) für das Plugin" || bad "/dev/shm/invoke-eq missing or old ($m)" "/dev/shm/invoke-eq fehlt oder alt ($m)"
S 'test -f /dev/shm/invoke-viz' && pass "visualizer tap active" "Visualizer-Abgriff aktiv" || soft "no visualizer tap yet (play something first)" "noch kein Visualizer-Abgriff (erst etwas abspielen)"
# CPU-Last der Kette: 10 s Wiedergabe über invoke_music, Last vorher/nachher
l0=$(S 'cut -d" " -f1 /proc/loadavg')
cpu=$(S "$A (aplay -q -D invoke_music -d 10 -f S16_LE -r 44100 -c 2 /dev/urandom >/dev/null 2>&1 &) ; sleep 5; top -b -n 1 2>/dev/null | grep -m1 '[a]play' || ps | grep -m1 '[a]play'")
rep "- $(t 'aplay on invoke_music (resampling, EQ, viz):' 'aplay über invoke_music (Wandlung, Klang, Visualizer):') \`${cpu:-?}\` (load ${l0})"
info "  aplay: ${cpu:-?}"

section "Clock and watchdog" "Uhr und Watchdog"
if S 'busybox ntpd --help >/dev/null 2>&1'; then pass "busybox ntpd available" "busybox ntpd vorhanden"; else soft "no busybox ntpd: clock is not set (alarms depend on it)" "kein busybox ntpd: Uhr wird nicht gestellt (Wecker hängen daran)"; fi
S 'test -f /run/invoke-ntp.ok' && pass "clock set by NTP since boot" "Uhr seit dem Start per NTP gestellt" || soft "clock not (yet) set by NTP" "Uhr (noch) nicht per NTP gestellt"
dev=$(date -u +%s); spk=$(S 'date -u +%s'); d=$((spk - dev)); [ ${d#-} -le 3 ] && pass "clock deviation ${d} s (vs. this computer)" "Abweichung der Uhr ${d} s (zu diesem Rechner)" || soft "clock deviation ${d} s" "Abweichung der Uhr ${d} s"
wd=$(S 'ls -l /dev/watchdog* 2>/dev/null; for p in /proc/[0-9]*; do ls -l $p/fd 2>/dev/null | grep -q watchdog && echo "offen von $(cat $p/comm 2>/dev/null) (pid ${p#/proc/})"; done')
if echo "$wd" | grep -q /dev/watchdog; then
  pass "hardware watchdog present" "Hardware-Watchdog vorhanden"; detail "$wd"
  if echo "$wd" | grep -q "offen von" && ! echo "$wd" | grep -q "offen von invoked"; then soft "watchdog is held by another process (WATCHDOG=on will not work)" "Watchdog ist von einem anderen Prozess belegt (WATCHDOG=on geht nicht)"; fi
else soft "no /dev/watchdog: WATCHDOG=on has no effect" "kein /dev/watchdog: WATCHDOG=on wirkt nicht"; fi

section "Microphones" "Mikrofone"
cards=$(S 'arecord -l 2>&1; echo; cat /proc/asound/pcm 2>/dev/null')
detail "$cards" 60
devs=$(echo "$cards" | sed -n 's/^card \([0-9]*\):.*device \([0-9]*\):.*/\1,\2/p' | sort -u)
[ -n "$devs" ] || soft "arecord lists no capture device" "arecord findet kein Aufnahmegerät"
level(){ python3 -c '
import sys, struct, math
b = sys.stdin.buffer.read()
n = len(b) // 2
if n == 0: print("leer"); sys.exit()
s = struct.unpack("<%dh" % n, b[:2 * n])
rms = math.sqrt(sum(x * x for x in s) / n) or 1e-9
pk = max(abs(x) for x in s) or 1e-9
print("%d Werte, RMS %.1f dBFS, Spitze %.1f dBFS" % (n, 20 * math.log10(rms / 32768), 20 * math.log10(pk / 32768)))'; }
for d in $devs; do
  [ "$LISTEN" = 1 ] && info "  plughw:$d - $(t 'speak for 3 seconds now ...' 'jetzt 3 Sekunden sprechen ...')"
  r=$(S "arecord -q -D plughw:$d -f S16_LE -r 16000 -c 1 -d 3 -t raw" 2>/dev/null | level)
  if [ -n "$r" ] && [ "$r" != leer ]; then pass "capture plughw:$d: $r" "Aufnahme plughw:$d: $r"; else soft "plughw:$d records nothing" "plughw:$d nimmt nichts auf"; fi
done
rep ""; rep "$(t 'The device with a clear level difference between silence and speech is the one for Settings > Voice (plughw:X,Y).' 'Das Gerät mit deutlichem Unterschied zwischen Stille und Sprache gehört in Home Assistant > Sprachassistent (plughw:X,Y).')"

section "Resources" "Ressourcen"
res=$(S 'cat /proc/loadavg; free 2>/dev/null | head -n 2; df /data | tail -n 1; cat /sys/class/hwmon/hwmon0/device/tsen_temp 2>/dev/null')
detail "$res"
t=$(echo "$res" | tail -n 1); case $t in ''|*[!0-9]*) ;; *) [ "$t" -lt 85 ] && pass "SoC temperature $t °C" "SoC-Temperatur $t °C" || soft "SoC temperature $t °C (vendor shuts down at 95 °C)" "SoC-Temperatur $t °C (Abschaltung bei 95 °C)";; esac

if [ "$LISTEN" = 1 ] && [ -n "$TOKEN" ]; then
  section "Listening checks" "Hörprüfungen"
  hear(){ if ask_yn "$1" "$2" y; then pass "$1" "$2"; else bad "$1" "$2"; fi; }
  apost /api/announce '{"tone":"chime"}'; sleep 2
  hear "Did you hear the chime?" "War der Gong zu hören?"
  apost /api/radio/play '{"index":0}'; sleep 8
  hear "Is the first web radio station playing?" "Spielt der erste Webradio-Sender?"
  apost /api/announce '{"tone":"bell"}'; sleep 3
  hear "Did the music get quieter during the door bell and come back?" "Wurde die Musik während der Türklingel leiser und kam zurück?"
  info "  $(t 'Now start Spotify or Bluetooth on your phone ...' 'Jetzt Spotify oder Bluetooth am Handy starten ...')"; wait_enter "Enter when it plays" "Enter, wenn es spielt"
  hear "Did the radio stop softly (fade, no click) when the phone started?" "Hörte das Radio weich auf (ausgeblendet, kein Knacken), als das Handy begann?"
  apost /api/radio/stop '{}'
  apost /api/briefing/start '{}'; sleep 15
  hear "Did the briefing start with a chime (and speech/news, if set up)?" "Begann das Briefing mit einem Gong (und Sprache/Nachrichten, falls eingerichtet)?"
  apost /api/briefing/stop '{}'
fi

echo
rep ""; rep "**$(t 'Result' 'Ergebnis'): $NOK OK, $NWARN WARN, $NFAIL FAIL**"
info "$NOK OK, $NWARN WARN, $NFAIL FAIL - $(t 'report' 'Bericht'): $REPORT"
[ "$NFAIL" = 0 ]
