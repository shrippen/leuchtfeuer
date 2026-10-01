#!/usr/bin/env bash
# Sets the password of the speaker's web interface  afterwards, without a full install.
# Setzt nachträglich das Passwort der Weboberfläche , ohne die Installation zu wiederholen.
#
#   ./scripts/set-web-password.sh                      interactive (asks for the IP, key and password)
#   INVOKE_WEB_PASSWORD=secret ./scripts/set-web-password.sh --ip 192.168.1.50 --key ~/.ssh/id_ed25519.pub
#   ./scripts/set-web-password.sh --ip IP --key KEY --random     generate a random password and print it
#
# The password is also changeable in the web interface (Settings). Rule: at least 6 characters. The speaker stores only a salted
# PBKDF2 hash (WEB_PASSWORD_HASH in /data/invoke/config), never the password itself.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib.sh
IP=""; KEY=""; PW=${INVOKE_WEB_PASSWORD:-}; RANDOM_PW=0
while [ $# -gt 0 ]; do
  case $1 in
    --ip) IP=$2; shift 2 ;; --key) KEY=$2; shift 2 ;; --password) PW=$2; shift 2 ;; --random) RANDOM_PW=1; shift ;;
    --non-interactive|--yes|-y) INTERACTIVE=0; shift ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;; *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
[ -n "$IP" ] || ask IP "IP address of the speaker" "IP-Adresse des Lautsprechers"
[ -n "$IP" ] || die "--ip is missing" "--ip fehlt"
if [ -z "$KEY" ] && [ "$INTERACTIVE" = 1 ]; then choose_key; fi
[ -n "$KEY" ] || die "--key is missing" "--key fehlt"
if [ -f "${KEY%.pub}" ]; then ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
S(){ ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new "${ID[@]}" root@"$IP" "$@"; }
S true || die "no SSH access to $IP" "kein SSH-Zugang zu $IP"

if [ $RANDOM_PW = 1 ]; then PW=$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n'); fi
if [ -z "$PW" ] && [ "$INTERACTIVE" = 1 ]; then ask_web_password PW; fi
[ -n "$PW" ] || die "no password given" "kein Passwort angegeben"
valid_web_password "$PW" || die "the password needs at least 6 characters" "das Passwort braucht mindestens 6 Zeichen"
case $PW in *$'\n'*) die "no line breaks in the password" "keine Zeilenumbrüche im Passwort" ;; esac

# the password travels on stdin (not in the command line); invoked hashes it with a random salt and writes the hash
printf '%s\n' "$PW" | S '/data/invoke/bin/invoked -set-password && { p=$(cat /run/invoke-svc-invoked.pid 2>/dev/null); [ -n "$p" ] && kill "$p"; true; }' \
  || die "could not set the password (is the Invoke Hack installed and up to date? run ./install.sh)" \
         "Passwort konnte nicht gesetzt werden (Invoke-Hack installiert und aktuell? ./install.sh ausführen)"
ok "web password set (stored as a salted hash); the web interface restarts within 30 seconds, existing logins end" \
   "Web-Passwort gesetzt (nur als gesalzener Hash gespeichert); die Weboberfläche startet binnen 30 Sekunden neu, bestehende Anmeldungen enden"
[ $RANDOM_PW = 1 ] && info "New password: $PW" "Neues Passwort: $PW"
info "Sign in at http://$IP/" "Anmeldung unter http://$IP/"
