#!/usr/bin/env bash
# Removes Leuchtfeuer again: autostart hook from /data/dnsmasq.conf, reboot. Afterwards the speaker behaves like
# the plain StockRoot firmware (vendor sshd with locked root, adbd on again).
# Entfernt Leuchtfeuer wieder: Autostart-Haken aus /data/dnsmasq.conf, Neustart.
#   ./uninstall.sh                      interactive (asks what it needs)
#   ./uninstall.sh --non-interactive --ip IP --key ~/.ssh/id_ed25519.pub [--purge]
#   --purge   also deletes /data/invoke (programs, settings, saved Bluetooth pairings, logs)
set -euo pipefail
cd "$(dirname "$0")"
. scripts/lib.sh
IP=""; KEY=""; PURGE=0
while [ $# -gt 0 ]; do
  case $1 in
    --ip) IP=$2; shift 2 ;; --key) KEY=$2; shift 2 ;; --purge) PURGE=1; shift ;;
    --non-interactive|--yes|-y) INTERACTIVE=0; shift ;;
    -h|--help) sed -n '2,9p' "$0"; exit 0 ;; *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
say "Remove Leuchtfeuer" "Leuchtfeuer entfernen"
info "This switches off the autostart hook and restarts the speaker. Afterwards the speaker is back to the plain StockRoot
firmware: the vendor SSH server (root locked) and the open adb root shell on port 5555 are active again, and Harman's
services (Cortana, OTA, vendor Bluetooth) run again. Your Bluetooth pairings are kept unless you purge." \
"Das schaltet den Autostart-Haken ab und startet den Lautsprecher neu. Danach ist er wieder die reine StockRoot-Firmware:
der Hersteller-SSH-Server (root gesperrt) und die offene adb-Root-Shell auf Port 5555 sind wieder aktiv, und die
Harman-Dienste (Cortana, OTA, Hersteller-Bluetooth) laufen wieder. Bluetooth-Kopplungen bleiben erhalten, außer bei --purge."
[ -n "$IP" ] || ask IP "IP address of the speaker" "IP-Adresse des Lautsprechers"
[ -n "$IP" ] || die "--ip is missing" "--ip fehlt"
if [ -z "$KEY" ] && [ "$INTERACTIVE" = 1 ]; then choose_key; fi
[ -n "$KEY" ] || die "--key is missing" "--key fehlt"
if [ -f "${KEY%.pub}" ]; then ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
S(){ ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new "${ID[@]}" root@"$IP" "$@"; }
S true || die "no SSH access to $IP" "kein SSH-Zugang zu $IP"
if [ $PURGE = 0 ] && [ "$INTERACTIVE" = 1 ]; then
  note "--purge also deletes /data/invoke: programs, settings and saved Bluetooth pairings." "--purge löscht auch /data/invoke: Programme, Einstellungen und gespeicherte Bluetooth-Kopplungen."
  ask_yn "Also delete /data/invoke (purge)?" "Auch /data/invoke löschen (purge)?" n && PURGE=1
fi
ask_yn "Remove Leuchtfeuer from $IP now?" "Leuchtfeuer jetzt von $IP entfernen?" y || die "aborted" "abgebrochen"
S 'touch /data/invoke/disable-hook
   if [ -f /data/invoke/dnsmasq.conf.orig ]; then cp /data/invoke/dnsmasq.conf.orig /data/dnsmasq.conf; echo "dnsmasq.conf restored"
   else sed -i "/Invoke-Hack: Autostart-Haken/,\$d" /data/dnsmasq.conf; echo "hook removed from dnsmasq.conf"; fi'
if [ $PURGE = 1 ]; then S 'rm -rf /data/invoke; echo "/data/invoke deleted"'; fi
info "Restarting ..." "Neustart ..."; S '/bin/reboot' >/dev/null 2>&1 || true
info "Done. After the restart the speaker behaves like StockRoot (adb on port 5555 is open again!)." \
     "Fertig. Nach dem Neustart verhält sich das Gerät wie StockRoot (adb auf Port 5555 ist wieder offen!)."
