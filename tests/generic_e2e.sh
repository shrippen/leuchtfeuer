#!/bin/sh
# Ende-zu-Ende-Test des Zielgeräts "generic" auf diesem Rechner (ohne root, ohne Soundkarte): Paket bauen
# (tools/build-generic.sh, scripts/assemble.sh --target generic), Hook starten wie die systemd-Unit, dann prüfen:
# leuchtfeuerd läuft als generic ohne Erweiterungen, Dienste ohne Programm sind "missing", der lokale Bus
# (Lautstärke, die leuchtfeuerd hier selbst führt) und die Weboberfläche antworten.
#   sh tests/generic_e2e.sh
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d)
D=$T/lf R=$T/run PORT=${LF_TEST_PORT:-18431}
fail=0
check(){ if eval "$2"; then echo "ok    $1"; else echo "FEHLER $1"; fail=1; fi; }
cleanup(){
  [ -n "${hp:-}" ] && kill "$hp" 2>/dev/null
  for pf in "$R"/leuchtfeuer-svc-*.pid; do [ -f "$pf" ] && kill "$(cat "$pf")" 2>/dev/null; done
  sleep 0.3; [ -n "${KEEP:-}" ] && echo "behalten: $T" && return; rm -rf "$T"
}
trap cleanup EXIT
arch=$(case $(uname -m) in x86_64) echo amd64 ;; aarch64) echo arm64 ;; *) echo armv7 ;; esac)
"$ROOT/tools/build-generic.sh" "$arch" >/dev/null || { echo "FEHLER Bau"; exit 1; }
"$ROOT/scripts/assemble.sh" "$D" --target generic --arch "$arch" || { echo "FEHLER assemble"; exit 1; }
mkdir -p "$R"
sed -e "s|^WEB_PORT=.*|WEB_PORT=\"$PORT\"|" -e 's|^SERVICE_CAST=.*|SERVICE_CAST="off"|' -e 's|^DEVICE_NAME=.*|DEVICE_NAME="Testraum"|' \
  "$D/config.example" > "$D/config"
printf 'geheim123\n' | LEUCHTFEUER_DIR=$D "$D/bin/leuchtfeuerd" -set-password -config "$D/config" >/dev/null
check "Passwort gesetzt (nur Hash)" 'grep -q "^WEB_PASSWORD_HASH=\"pbkdf2" "$D/config" && ! grep -q geheim123 "$D/config"'

LEUCHTFEUER_DIR=$D LEUCHTFEUER_RUN=$R setsid sh "$D/hook.sh" </dev/null >/dev/null 2>&1 &
hp=$!
web="http://127.0.0.1:$PORT"
i=0; while [ $i -lt 60 ] && ! curl -fs -o /dev/null "$web/login.html" 2>/dev/null; do sleep 0.5; i=$((i + 1)); done
check "Hook startet leuchtfeuerd (Weboberfläche)" 'curl -fs -o /dev/null "$web/login.html"'
curl -fs -c "$T/jar" -H 'Content-Type: application/json' -d '{"password":"geheim123"}' "$web/api/login" >/dev/null
st=$(curl -fs -b "$T/jar" "$web/api/status")
j(){ printf '%s' "$st" | python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }
check "Zielgerät generic, keine Erweiterungen" '[ "$(j "d[\"device\"][\"id\"], d[\"device\"][\"capabilities\"]")" = "generic []" ]'
check "Name aus der Konfiguration" '[ "$(j "d[\"name\"]")" = Testraum ]'
check "Hook gibt Umgebung weiter (Prozess unter $D)" 'grep -q "Bus auf $R/leuchtfeuer-bus.sock" "$D/log/leuchtfeuerd.log"'
check "Dienst ohne Programm: missing" '[ "$(cat "$R/leuchtfeuer-svc-librespot.mode" 2>/dev/null)" = missing ] && grep -q "librespot: Programm fehlt" "$D/hook.log"'
check "ausgeschaltete Dienste: off" '[ "$(cat "$R/leuchtfeuer-svc-castrecv.mode" 2>/dev/null)" = off ] && [ "$(cat "$R/leuchtfeuer-svc-bluetooth-2-agent.mode" 2>/dev/null)" = off ]'
bus(){ curl -fs --unix-socket "$R/leuchtfeuer-bus.sock" "$@"; }
check "Bus: Lautstärke bekannt" 'bus http://bus/state | grep -q "\"known\":true"'
bus -H 'Content-Type: application/json' -d '{"volume":55}' http://bus/volume >/dev/null
check "Bus: Lautstärke setzen" 'bus http://bus/state | grep -q "\"volume\":55"'
check "eigene Lautstärke gespeichert" 'grep -q "55" "$D/volume.json"'
check "Firewall standardmäßig aus" '[ ! -e "$R/leuchtfeuer-fw.rules" ]'
check "Tonkette hinter alsa.conf (ALSA_CONFIG_PATH)" '[ ! -f /usr/share/alsa/alsa.conf ] || tr "\0" "\n" < /proc/$(cat "$R/leuchtfeuer-svc-leuchtfeuerd.pid")/environ | grep -q "^ALSA_CONFIG_PATH=/usr/share/alsa/alsa.conf:$D/asound-music.conf$"'
# Ausgabe: output.conf liegt da (leer), die API kennt die Wahl; die Tonkette folgt der Datei (echte alsa-lib, ohne Soundkarte:
# der Standard-Ausgang scheitert, eine Überschreibung mit "type null" in output.conf öffnet sich)
check "output.conf vom Hook angelegt (leer)" '[ -f "$D/output.conf" ] && ! grep -q "pcm.!" "$D/output.conf"'
api(){ curl -fs -b "$T/jar" -H 'Content-Type: application/json' "$@"; }
check "API: Ausgaben (Standard gewählt)" 'api "$web/api/outputs" | python3 -c "import json,sys; d=json.load(sys.stdin); sys.exit(0 if d[\"current\"]==\"default\" and d[\"outputs\"][0][\"id\"]==\"default\" else 1)"'
check "API: unbekannte Soundkarte abgelehnt" '! api -X PUT -d "{\"id\":\"card:42\"}" "$web/api/outputs" >/dev/null'
check "API: Bluetooth ohne Agent abgelehnt" '! api -d "{\"action\":\"scan\"}" "$web/api/outputs/bluetooth" >/dev/null'
if [ -f /usr/share/alsa/alsa.conf ] && python3 -c "import ctypes; ctypes.CDLL('libasound.so.2')" 2>/dev/null; then
  cat > "$T/alsaopen.py" <<'EOF_PY'
import ctypes, sys
a = ctypes.CDLL("libasound.so.2"); p = ctypes.c_void_p()
r = a.snd_pcm_open(ctypes.byref(p), sys.argv[1].encode(), 0, 0)
if r == 0: a.snd_pcm_close(p)
sys.exit(0 if r == 0 else 1)
EOF_PY
  export ALSA_CONFIG_PATH=/usr/share/alsa/alsa.conf:$D/asound-music.conf
  check "Tonkette: Standard-Ausgang ohne Soundkarte nicht offen" '! python3 "$T/alsaopen.py" leuchtfeuer_sink 2>/dev/null'
  echo 'pcm.!leuchtfeuer_sink { type null }' > "$D/output.conf"
  check "Tonkette: output.conf überschreibt die Ausgabe" 'python3 "$T/alsaopen.py" leuchtfeuer_sink 2>/dev/null'
  : > "$D/output.conf"
  unset ALSA_CONFIG_PATH
fi
[ "$fail" = 0 ] || { echo "--- hook.log"; cat "$D/hook.log"; echo "--- leuchtfeuerd.log"; tail -n 30 "$D/log/leuchtfeuerd.log"; }
exit $fail
