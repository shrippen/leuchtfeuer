#!/usr/bin/env bash
# Entfernt den Invoke-Hack wieder: Autostart-Haken aus /data/dnsmasq.conf, Neustart. Danach läuft das
# Gerät wie mit der StockRoot-Firmware (Original-sshd mit gesperrtem root, adbd wieder an).
#   ./uninstall.sh --ip IP --key ~/.ssh/id_ed25519.pub [--purge] [--yes]
#   --purge   löscht zusätzlich /data/invoke (Programme, Konfiguration, gespeicherte Bluetooth-Kopplungen, Logs)
set -euo pipefail
IP=""; KEY=""; PURGE=0; YES=0
while [ $# -gt 0 ]; do
  case $1 in
    --ip) IP=$2; shift 2 ;; --key) KEY=$2; shift 2 ;; --purge) PURGE=1; shift ;; --yes|-y) YES=1; shift ;;
    -h|--help) sed -n '2,6p' "$0"; exit 0 ;; *) echo "unbekannte Option: $1" >&2; exit 2 ;;
  esac
done
[ -n "$IP" ] || { echo "--ip fehlt" >&2; exit 2; }
ID=()
if [ -n "$KEY" ]; then
  if [ -f "${KEY%.pub}" ]; then ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
fi
S(){ ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new "${ID[@]}" root@"$IP" "$@"; }
S true || { echo "Kein SSH-Zugang zu $IP" >&2; exit 1; }
if [ $YES = 0 ]; then
  read -r -p "Invoke-Hack auf $IP entfernen${PURGE:+ (mit --purge: /data/invoke wird gelöscht)}? [j/N] " a
  [ "$a" = j ] || [ "$a" = J ] || { echo abgebrochen; exit 1; }
fi
S 'touch /data/invoke/disable-hook
   if [ -f /data/invoke/dnsmasq.conf.orig ]; then cp /data/invoke/dnsmasq.conf.orig /data/dnsmasq.conf; echo "dnsmasq.conf wiederhergestellt"
   else sed -i "/Invoke-Hack: Autostart-Haken/,\$d" /data/dnsmasq.conf; echo "Haken aus dnsmasq.conf entfernt"; fi'
[ $PURGE = 1 ] && S 'rm -rf /data/invoke; echo "/data/invoke gelöscht"'
echo "Neustart …"; S '/bin/reboot' >/dev/null 2>&1 || true
echo "Fertig. Nach dem Neustart läuft das Gerät wie mit der StockRoot-Firmware (adb auf Port 5555 ist wieder offen!)."
