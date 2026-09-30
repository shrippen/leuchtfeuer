#!/usr/bin/env bash
# Prüft per SSH, ob der Invoke-Hack vollständig läuft.
#   scripts/verify-install.sh --ip IP [--key ~/.ssh/id_ed25519.pub]
set -uo pipefail
IP=""; KEY=""
while [ $# -gt 0 ]; do case $1 in --ip) IP=$2; shift 2;; --key) KEY=$2; shift 2;; *) echo "?: $1" >&2; exit 2;; esac; done
[ -n "$IP" ] || { echo "--ip fehlt" >&2; exit 2; }
ID=()
if [ -n "$KEY" ]; then
  if [ -f "${KEY%.pub}" ]; then ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
fi
S(){ ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new "${ID[@]}" root@"$IP" "$@"; }
fail=0
ok(){ printf '  \033[32mOK\033[0m   %s\n' "$*"; }
bad(){ printf '  \033[31mFEHLT\033[0m %s\n' "$*"; fail=1; }
S true 2>/dev/null || { bad "SSH-Zugang (Schlüssel) geht nicht"; exit 1; }
ok "SSH mit Schlüssel"
ps=$(S 'ps' 2>/dev/null)
for p in librespot gmediarender sendspin-player castrecv volume-sync bluetoothd btagent bluealsa bluealsa-aplay; do
  echo "$ps" | grep -q "[/ ]$p" && ok "Prozess $p" || bad "Prozess $p läuft nicht"
done
echo "$ps" | grep -q "tidal_connect" && ok "Prozess tidal_connect_application" || echo "  --   Tidal Connect nicht installiert/gestartet (optional)"
hci=$(S 'LD_LIBRARY_PATH=/data/invoke/bluez/lib /data/invoke/bluez/bin/hciconfig hci0 2>&1')
echo "$hci" | grep -q "UP RUNNING" && ok "Bluetooth-Adapter hci0 oben" || bad "Bluetooth-Adapter hci0 nicht oben"
echo "$hci" | grep -q "ISCAN" && ok "Bluetooth sichtbar (koppelbereit)" || bad "Bluetooth nicht sichtbar"
S 'iptables -S INVOKE 2>/dev/null | grep -q -- "--dport 22"' && ok "Firewall-Kette INVOKE" || bad "Firewall-Kette INVOKE fehlt"
S 'amixer -c 0 sget "Invoke Music" >/dev/null 2>&1' && ok "Lautstärkeregler \"Invoke Music\"" || bad "Regler \"Invoke Music\" fehlt"
S 'ps | grep -q "[a]dbd"' && bad "adbd läuft (Port 5555 = root ohne Anmeldung)" || ok "adbd aus"
for port in 57500 49494 8009; do
  (timeout 4 bash -c "echo > /dev/tcp/$IP/$port") 2>/dev/null && ok "Port $port/tcp erreichbar" || bad "Port $port/tcp nicht erreichbar"
done
[ $fail = 0 ] && echo "Alles in Ordnung." || echo "Es gibt Probleme (siehe oben; Logs: /data/invoke/log/ auf dem Gerät)."
exit $fail
