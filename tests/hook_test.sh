#!/bin/sh
# Prüft die Logik von device/leuchtfeuer/hook.sh auf dem Rechner: Dienstschalter, fehlende Programme, Firewall-Regeln,
# wachsende Pause bei abstürzenden Diensten, Kürzen der Protokolle, Zielgeräte (targets/invoke, targets/generic).
# iptables, date und setsid sind nachgebildet.
#   sh tests/hook_test.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export LEUCHTFEUER_DIR=$T/d LEUCHTFEUER_RUN=$T/r HOOK_LIB=1
mkdir -p $T/d/services $T/d/log $T/r $T/bin
echo 1000 > $T/now
cat > $T/bin/date <<'S'
#!/bin/sh
case "${1:-}" in +%s) cat "$NOWFILE" ;; *) echo "2026-10-01 12:00:00" ;; esac
S
cat > $T/bin/iptables <<'S'
#!/bin/sh
echo "$*" >> "$IPTLOG"
case "$1" in -C|-D) exit 1 ;; esac
exit 0
S
# setsid: Dienst gleich ausführen (er endet sofort) – der Hook sieht ihn danach als gestorben
cat > $T/bin/setsid <<'S'
#!/bin/sh
"$@"
S
chmod 755 $T/bin/*
export PATH=$T/bin:$PATH NOWFILE=$T/now IPTLOG=$T/ipt.log
fail=0
check(){ if eval "$2"; then echo "ok    $1"; else echo "FEHLER $1"; fail=1; fi; }

printf '#!/bin/sh\n# title: A\n# group: airplay\n# process: a\n# ports: tcp 5000, udp 6001:6011\n# default: on\nexit 1\n' > $T/d/services/shairport.sh
printf '#!/bin/sh\n# title: S\n# group: snapcast\n# process: s\n# ports: tcp 1780\n# default: off\nexit 0\n' > $T/d/services/snapclient.sh
printf '#!/bin/sh\n# title: L\n# group: spotify\n# process: l\n# ports: tcp 57500\n# requires: gibtsnicht-lf aplay\n# default: on\nexit 0\n' > $T/d/services/librespot.sh
chmod 755 $T/d/services/*.sh
cp "$ROOT/targets/invoke/target.sh" $T/d/target.sh
printf 'tcp 9999\n# Kommentar\nunsinn 1\n' > $T/d/ports.local
printf '# von leuchtfeuerd\ntcp 10700 sprachassistent\n' > $T/d/ports.leuchtfeuerd
: > $T/d/config

. "$ROOT/device/leuchtfeuer/hook.sh"

r=$(fw_rules)
check "Port eines eingeschalteten Dienstes offen" 'echo "$r" | grep -q -- "-p tcp --dport 5000 -j RETURN"'
check "Portbereich" 'echo "$r" | grep -q -- "-p udp --dport 6001:6011 -j RETURN"'
check "ausgeschalteter Dienst (Vorgabe off) bleibt zu" '! echo "$r" | grep -q 1780'
check "ports.local wirkt, Unsinn nicht" 'echo "$r" | grep -q 9999 && ! echo "$r" | grep -q unsinn'
check "ports.leuchtfeuerd (Sprachassistent) wirkt" 'echo "$r" | grep -q -- "-p tcp --dport 10700 -j RETURN"'
check "Weboberfläche Port 80" 'echo "$r" | grep -q -- "--dport 80 -j RETURN"'
check "DROP am Ende" '[ "$(echo "$r" | tail -n 1)" = "-j DROP" ]'
check "Invoke: Einrichtungs-Ports am Setup-AP" 'echo "$r" | grep -q -- "-i p2p0 -p udp --dport 48301 -j RETURN"'
check "Dienst ohne Programm: Port bleibt zu" '! echo "$r" | grep -q 57500'
check "Umgebung der Dienste" '[ -z "${ALSA_CONFIG_PATH:-}" ] && [ "$LEUCHTFEUER_TARGET" = invoke ] && [ "$LEUCHTFEUER_NAME" = "HK Invoke" ] && [ "$WIFI_IFACE" = wlan0 ] && [ "$ALSA_CONFIG" = $T/d/asound-music.conf ]'
echo 'AIRPLAY="off"' > $T/d/config
check "alter Schalter AIRPLAY=off schließt den Port" '! fw_rules | grep -q 5000'
echo 'AIRPLAY="off"
SERVICE_AIRPLAY="on"
SERVICE_SNAPCAST="on"
WEB_TLS="on"' > $T/d/config
r=$(fw_rules)
check "SERVICE_* hat Vorrang, Snapcast an" 'echo "$r" | grep -q 5000 && echo "$r" | grep -q 1780'
check "HTTPS-Port 443" 'echo "$r" | grep -q -- "-p tcp --dport 443 -j RETURN$"'

: > $T/ipt.log
firewall
check "Firewall: neue Kette umgehängt" 'grep -q "^-E LEUCHTFEUER_NEW LEUCHTFEUER" $T/ipt.log'
n=$(wc -l < $T/ipt.log)
cat > $T/bin/iptables <<'S'
#!/bin/sh
echo "$*" >> "$IPTLOG"
case "$1" in -D) exit 1 ;; esac
exit 0
S
firewall
check "Firewall: unverändert -> kein Neuaufbau" '[ "$(grep -c -- "-A LEUCHTFEUER_NEW" $T/ipt.log)" -lt "$n" ] && ! tail -n 1 $T/ipt.log | grep -q LEUCHTFEUER_NEW'

# Backoff: shairport endet sofort mit Fehler
: > $T/d/config
for i in 1 2 3 4 5; do services; echo $(( $(cat $T/now) + 31 )) > $T/now; done
read -r fails next started restarts < $T/r/leuchtfeuer-svc-shairport.state
check "Fehlschläge gezählt ($fails)" '[ "$fails" -ge 3 ]'
check "Pause wächst (nächster Start $next, jetzt $(cat $T/now))" '[ "$next" -gt "$(cat $T/now)" ]'
check "ausgeschalteter Dienst nicht gestartet" '[ "$(cat $T/r/leuchtfeuer-svc-snapclient.mode 2>/dev/null)" = off ]'
check "Dienst ohne Programm nicht gestartet (missing)" '[ "$(cat $T/r/leuchtfeuer-svc-librespot.mode 2>/dev/null)" = missing ] && [ ! -e $T/r/leuchtfeuer-svc-librespot.pid ] && grep -q "librespot: Programm fehlt" $T/d/hook.log'
touch $T/r/leuchtfeuer-svc-shairport.expected
echo "0 0 $(cat $T/now) 0" > $T/r/leuchtfeuer-svc-shairport.state
echo $(( $(cat $T/now) + 5 )) > $T/now
services
read -r fails next _ < $T/r/leuchtfeuer-svc-shairport.state
check "absichtliches Beenden zählt nicht" '[ "$fails" = 0 ] && [ ! -e $T/r/leuchtfeuer-svc-shairport.expected ]'

head -c 1100000 /dev/zero | tr '\0' 'x' > $T/d/log/big.log
rotate_logs
check "Protokoll gekürzt" '[ "$(wc -c < $T/d/log/big.log)" -lt 1000 ] && [ "$(wc -c < $T/d/log/big.log.1)" = 262144 ]'

# ersetzte Klänge: einhängen, was fehlt; Eingehängtes nicht doppelt
cat > $T/bin/mount <<'S'
#!/bin/sh
echo "$*" >> "$MOUNTLOG"
S
chmod 755 $T/bin/mount
export MOUNTLOG=$T/mount.log
mkdir -p $T/d/sounds $T/sys
: > $T/sys/start.wav; : > $T/sys/fehler.wav; : > $T/d/sounds/v-1.wav
printf '%s\tv-1.wav\n%s\tfehlt.wav\n' "$T/sys/start.wav" "$T/sys/fehler.wav" > $T/d/sounds/vendor.map
sounds_mount
check "Klang eingehängt, fehlende Datei übersprungen" 'grep -q -- "--bind $T/d/sounds/v-1.wav $T/sys/start.wav" $T/mount.log && ! grep -q fehler $T/mount.log'

# Watchdog: nur mit WATCHDOG=on und Gerät; nach 3 unruhigen Starts bleibt er aus
mkdir -p $T/d/bin
printf '#!/bin/sh\necho "$@" > %s/wd.args\nexec sleep 30\n' "$T" > $T/d/bin/leuchtfeuerd
chmod 755 $T/d/bin/leuchtfeuerd
cat > $T/bin/setsid <<'S'
#!/bin/sh
exec "$@"
S
chmod 755 $T/bin/setsid
# hook.sh ist schon geladen: WDDEV direkt setzen, sonst greift ein echtes /dev/watchdog des Rechners
export LEUCHTFEUER_WATCHDOG_DEV=$T/watchdog; WDDEV=$T/watchdog
: > $T/d/config
watchdog_ctl
check "Watchdog aus ohne WATCHDOG=on" '[ ! -e $T/r/leuchtfeuer-watchdog.pid ]'
echo 'WATCHDOG="on"' > $T/d/config
watchdog_ctl
check "Watchdog aus ohne Gerät" '[ ! -e $T/r/leuchtfeuer-watchdog.pid ]'
: > $T/watchdog
WDDEV=$T/watchdog
watchdog_ctl; sleep 0.3
check "Watchdog scharf, Lebenszeichen-Datei übergeben" '[ -s $T/r/leuchtfeuer-watchdog.pid ] && grep -q -- "-watchdog $T/r/leuchtfeuer-hook.alive" $T/wd.args && [ "$(cat $T/d/watchdog-unstable)" = 1 ]'
kill "$(cat $T/r/leuchtfeuer-watchdog.pid)"; sleep 0.2
watchdog_ctl; kill "$(cat $T/r/leuchtfeuer-watchdog.pid)"; sleep 0.2
check "Neustart des Hooks zählt nicht als Gerätestart" '[ "$(cat $T/d/watchdog-unstable)" = 1 ]'
rm -f $T/r/leuchtfeuer-watchdog.boot; watchdog_ctl; kill "$(cat $T/r/leuchtfeuer-watchdog.pid)"; sleep 0.2
rm -f $T/r/leuchtfeuer-watchdog.boot; watchdog_ctl; kill "$(cat $T/r/leuchtfeuer-watchdog.pid)"; sleep 0.2
rm -f $T/r/leuchtfeuer-watchdog.pid $T/r/leuchtfeuer-watchdog.boot
watchdog_ctl
check "nach 3 unruhigen Starts bleibt er aus" '[ ! -e $T/r/leuchtfeuer-watchdog.pid ] && [ -e $T/r/leuchtfeuer-watchdog.blocked ]'
echo 'WATCHDOG="off"' > $T/d/config
watchdog_ctl

# Zielgerät generic: keine Invoke-Regeln, Firewall standardmäßig aus (eine alte Kette wird entfernt), FIREWALL=on schaltet ein
cp "$ROOT/targets/generic/target.sh" $T/d/target.sh
: > $T/d/config
. "$ROOT/device/leuchtfeuer/hook.sh"
check "generic: Umgebung" '[ "$LEUCHTFEUER_TARGET" = generic ] && [ "$LEUCHTFEUER_NAME" = Leuchtfeuer ] && { [ ! -f /usr/share/alsa/alsa.conf ] || [ "$ALSA_CONFIG_PATH" = /usr/share/alsa/alsa.conf:$T/d/asound-music.conf ]; }'
check "generic: keine Setup-AP-Regeln" '! fw_rules | grep -q p2p0'
: > $T/ipt.log
firewall
check "generic: Firewall aus, alte Kette entfernt" 'grep -q "^-X LEUCHTFEUER" $T/ipt.log && ! grep -q -- "-A LEUCHTFEUER_NEW" $T/ipt.log && [ ! -e $T/r/leuchtfeuer-fw.rules ]'
: > $T/ipt.log
firewall
check "generic: aus bleibt aus (kein iptables-Aufruf)" '[ ! -s $T/ipt.log ]'
echo 'FIREWALL="on"' > $T/d/config
firewall
check "generic: FIREWALL=on baut die Kette" 'grep -q "^-E LEUCHTFEUER_NEW LEUCHTFEUER" $T/ipt.log'

# Tonkette: Installationspfad umschreiben, wenn die Installation woanders liegt (idempotent)
printf '# leuchtfeuer-dir: /opt/leuchtfeuer\npcm_type.bluealsa { lib "/opt/leuchtfeuer/bluez/lib/x.so" }\n</opt/leuchtfeuer/output.conf>\n' > $T/d/asound-music.conf
asound_dir; asound_dir
check "Tonkette: Pfad auf die Installation umgeschrieben" 'grep -q "^# leuchtfeuer-dir: $T/d$" $T/d/asound-music.conf && grep -q "lib \"$T/d/bluez/lib/x.so\"" $T/d/asound-music.conf && grep -q "^<$T/d/output.conf>$" $T/d/asound-music.conf && ! grep -q /opt/leuchtfeuer $T/d/asound-music.conf'

exit $fail
