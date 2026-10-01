#!/usr/bin/env bash
# Checks over SSH that the Invoke Hack is fully running / Prüft per SSH, ob der Invoke-Hack vollständig läuft.
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
for p in librespot gmediarender sendspin-player castrecv shairport-sync invoked volume-sync bluetoothd btagent bluealsa bluealsa-aplay; do
  if echo "$ps" | grep -q "[/ ]$p"; then ok "process $p" "Prozess $p"; else bad "process $p is not running" "Prozess $p läuft nicht"; fi
done
if echo "$ps" | grep -q "tidal_connect"; then ok "process tidal_connect_application" "Prozess tidal_connect_application"
else info "  --    Tidal Connect not installed/started (optional)" "  --    Tidal Connect nicht installiert/gestartet (optional)"; fi
hci=$(S 'LD_LIBRARY_PATH=/data/invoke/bluez/lib /data/invoke/bluez/bin/hciconfig hci0 2>&1')
if echo "$hci" | grep -q "UP RUNNING"; then ok "Bluetooth adapter hci0 is up" "Bluetooth-Adapter hci0 oben"; else bad "Bluetooth adapter hci0 is not up" "Bluetooth-Adapter hci0 nicht oben"; fi
if echo "$hci" | grep -q "ISCAN"; then ok "Bluetooth visible (ready to pair)" "Bluetooth sichtbar (koppelbereit)"; else bad "Bluetooth not visible" "Bluetooth nicht sichtbar"; fi
if S 'iptables -S INVOKE 2>/dev/null | grep -q -- "--dport 22"'; then ok "firewall chain INVOKE" "Firewall-Kette INVOKE"; else bad "firewall chain INVOKE is missing" "Firewall-Kette INVOKE fehlt"; fi
if S 'amixer -c 0 sget "Invoke Music" >/dev/null 2>&1'; then ok 'volume control "Invoke Music"' 'Lautstärkeregler "Invoke Music"'; else bad 'volume control "Invoke Music" is missing' 'Regler "Invoke Music" fehlt'; fi
if S 'ps | grep -q "[a]dbd"'; then bad "adbd is running (port 5555 = root shell without login)" "adbd läuft (Port 5555 = Root-Shell ohne Anmeldung)"; else ok "adbd is off" "adbd aus"; fi
for port in 57500 49494 8009 80 5000; do
  if (timeout 4 bash -c "echo > /dev/tcp/$IP/$port") 2>/dev/null; then ok "port $port/tcp reachable" "Port $port/tcp erreichbar"
  else bad "port $port/tcp not reachable" "Port $port/tcp nicht erreichbar"; fi
done
if [ $fail = 0 ]; then info "All good." "Alles in Ordnung."
else info "There are problems (see above; logs: /data/invoke/log/ on the speaker)." "Es gibt Probleme (siehe oben; Logs: /data/invoke/log/ auf dem Lautsprecher)."; fi
exit $fail
