#!/bin/sh
# Prüft die Logik von device/invoke/hook.sh auf dem Rechner: Dienstschalter, Firewall-Regeln, wachsende Pause bei
# abstürzenden Diensten, Kürzen der Protokolle. iptables, date und setsid sind nachgebildet.
#   sh tests/hook_test.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
export INVOKE_DIR=$T/d INVOKE_RUN=$T/r HOOK_LIB=1
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
chmod 755 $T/d/services/*.sh
printf 'tcp 9999\n# Kommentar\nunsinn 1\n' > $T/d/ports.local
: > $T/d/config

. "$ROOT/device/invoke/hook.sh"

r=$(fw_rules)
check "Port eines eingeschalteten Dienstes offen" 'echo "$r" | grep -q -- "-p tcp --dport 5000 -j RETURN"'
check "Portbereich" 'echo "$r" | grep -q -- "-p udp --dport 6001:6011 -j RETURN"'
check "ausgeschalteter Dienst (Vorgabe off) bleibt zu" '! echo "$r" | grep -q 1780'
check "ports.local wirkt, Unsinn nicht" 'echo "$r" | grep -q 9999 && ! echo "$r" | grep -q unsinn'
check "Weboberfläche Port 80" 'echo "$r" | grep -q -- "--dport 80 -j RETURN"'
check "DROP am Ende" '[ "$(echo "$r" | tail -n 1)" = "-j DROP" ]'
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
check "Firewall: neue Kette umgehängt" 'grep -q "^-E INVOKE_NEW INVOKE" $T/ipt.log'
n=$(wc -l < $T/ipt.log)
cat > $T/bin/iptables <<'S'
#!/bin/sh
echo "$*" >> "$IPTLOG"
case "$1" in -D) exit 1 ;; esac
exit 0
S
firewall
check "Firewall: unverändert -> kein Neuaufbau" '[ "$(grep -c -- "-A INVOKE_NEW" $T/ipt.log)" -lt "$n" ] && ! tail -n 1 $T/ipt.log | grep -q INVOKE_NEW'

# Backoff: shairport endet sofort mit Fehler
: > $T/d/config
for i in 1 2 3 4 5; do services; echo $(( $(cat $T/now) + 31 )) > $T/now; done
read -r fails next started restarts < $T/r/invoke-svc-shairport.state
check "Fehlschläge gezählt ($fails)" '[ "$fails" -ge 3 ]'
check "Pause wächst (nächster Start $next, jetzt $(cat $T/now))" '[ "$next" -gt "$(cat $T/now)" ]'
check "ausgeschalteter Dienst nicht gestartet" '[ "$(cat $T/r/invoke-svc-snapclient.mode 2>/dev/null)" = off ]'
touch $T/r/invoke-svc-shairport.expected
echo "0 0 $(cat $T/now) 0" > $T/r/invoke-svc-shairport.state
echo $(( $(cat $T/now) + 5 )) > $T/now
services
read -r fails next _ < $T/r/invoke-svc-shairport.state
check "absichtliches Beenden zählt nicht" '[ "$fails" = 0 ] && [ ! -e $T/r/invoke-svc-shairport.expected ]'

head -c 1100000 /dev/zero | tr '\0' 'x' > $T/d/log/big.log
rotate_logs
check "Protokoll gekürzt" '[ "$(wc -c < $T/d/log/big.log)" -lt 1000 ] && [ "$(wc -c < $T/d/log/big.log.1)" = 262144 ]'

exit $fail
