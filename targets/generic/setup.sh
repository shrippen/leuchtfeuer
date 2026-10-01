#!/bin/sh
# Installiert Leuchtfeuer auf einem beliebigen Linux mit systemd und ALSA (Zielgerät "generic"), aus dem entpackten
# Paket leuchtfeuer-<Version>-generic-<arch>.tar.gz:
#   mkdir lf && tar -xzf leuchtfeuer-*-generic-*.tar.gz -C lf && sudo sh lf/setup.sh
#   sudo sh lf/setup.sh --dir /opt/leuchtfeuer --password geheim --port 8080 --name Küche
#   sudo sh @DIR@/setup.sh --uninstall          (Einstellungen bleiben; mit --purge auch die)
# Ein zweiter Aufruf mit einem neueren Paket aktualisiert (Einstellungen, Schlüssel und Kopplungen bleiben).
# Musikdienste anderer Projekte kommen aus den Paketen des Systems, z. B. Debian/Raspberry Pi OS:
#   apt install alsa-utils librespot shairport-sync gmediarender snapclient bluez-alsa-utils
# Fehlt eines, zeigt die Oberfläche den Dienst als "nicht installiert".
set -eu
src=$(cd "$(dirname "$0")" && pwd)
DIR=@DIR@ PW="" PORT="" NAME="" MODE=install
while [ $# -gt 0 ]; do
  case $1 in
    --dir) DIR=$2; shift 2 ;;
    --password) PW=$2; shift 2 ;;
    --port) PORT=$2; shift 2 ;;
    --name) NAME=$2; shift 2 ;;
    --uninstall) MODE=uninstall; shift ;;
    --purge) MODE=purge; shift ;;
    -h|--help) sed -n '2,11p' "$0"; exit 0 ;;
    *) echo "unbekannte Option: $1" >&2; exit 2 ;;
  esac
done
[ "$(id -u)" = 0 ] || { echo "bitte als root (sudo)" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd fehlt (systemctl)" >&2; exit 1; }
UNIT=/etc/systemd/system/leuchtfeuer.service

if [ $MODE != install ]; then
  systemctl disable --now leuchtfeuer.service 2>/dev/null || true
  rm -f $UNIT; systemctl daemon-reload
  if [ $MODE = purge ]; then rm -rf "$DIR"; echo "Leuchtfeuer entfernt (mit Einstellungen)"
  else
    for f in hook.sh target.sh migrate.sh apply-update.sh setup.sh asound-music.conf leuchtfeuer.service config.example VERSION ARCH .remove; do rm -f "$DIR/$f"; done
    rm -rf "${DIR:?}/bin" "${DIR:?}/lib" "${DIR:?}/services"
    echo "Leuchtfeuer entfernt; Einstellungen liegen noch in $DIR (--purge löscht sie)"
  fi
  exit 0
fi

[ -f "$src/hook.sh" ] && [ -f "$src/target.sh" ] || { echo "$src ist kein entpacktes Leuchtfeuer-Paket" >&2; exit 1; }
arch=$(cat "$src/ARCH" 2>/dev/null || echo ?)
case $(uname -m)/$arch in x86_64/amd64|aarch64/arm64|armv7l/armv7|armv8l/armv7|aarch64/armv7) ;; *)
  echo "Paket für $arch, dieser Rechner ist $(uname -m)" >&2; exit 1 ;; esac
command -v aplay >/dev/null || echo "Hinweis: aplay fehlt (Paket alsa-utils) - Spotify und Snapcast brauchen es"

first=0; [ -f "$DIR/config" ] || first=1
running=0; systemctl is-active -q leuchtfeuer.service 2>/dev/null && running=1
[ $running = 1 ] && systemctl stop leuchtfeuer.service
mkdir -p "$DIR"
if [ "$src" != "$(cd "$DIR" && pwd)" ]; then
  # Programme und Skripte ersetzen, Einstellungen bleiben
  (cd "$src" && tar -cf - --exclude=./config --exclude=./authorized_keys .) | (cd "$DIR" && tar -xf -)
  if [ -f "$src/.remove" ]; then sed 's|^/*||' "$src/.remove" | while read -r f; do [ -n "$f" ] && rm -f "$DIR/$f"; done; fi
fi
if [ $first = 1 ]; then
  sed -e "s|^DEVICE_NAME=.*|DEVICE_NAME=\"${NAME:-Leuchtfeuer}\"|" -e "s|^WEB_PORT=.*|WEB_PORT=\"${PORT:-8080}\"|" \
    "$DIR/config.example" > "$DIR/config"
  tz=$(timedatectl show -p Timezone --value 2>/dev/null || true)
  [ -n "$tz" ] && sed -i "s|^TIMEZONE=.*|TIMEZONE=\"$tz\"|" "$DIR/config"
  chmod 600 "$DIR/config"
else
  [ -n "$NAME" ] && sed -i "s|^DEVICE_NAME=.*|DEVICE_NAME=\"$NAME\"|" "$DIR/config"
  [ -n "$PORT" ] && sed -i "s|^WEB_PORT=.*|WEB_PORT=\"$PORT\"|" "$DIR/config"
fi
if [ -n "$PW" ]; then
  printf '%s\n' "$PW" | LEUCHTFEUER_DIR="$DIR" "$DIR/bin/leuchtfeuerd" -set-password -config "$DIR/config"
elif [ $first = 1 ]; then
  echo "Kein Passwort angegeben: leuchtfeuerd erzeugt eines und schreibt es ins Protokoll ($DIR/log/leuchtfeuerd.log)."
fi
sed "s|^Environment=LEUCHTFEUER_DIR=.*|Environment=LEUCHTFEUER_DIR=$DIR|; s|^ExecStart=.*|ExecStart=/bin/sh $DIR/hook.sh|" \
  "$DIR/leuchtfeuer.service" > $UNIT
systemctl daemon-reload
systemctl enable --now leuchtfeuer.service
port=$(sed -n 's/^WEB_PORT="\{0,1\}\([0-9]*\).*/\1/p' "$DIR/config" | head -n 1)
echo "Leuchtfeuer $(cat "$DIR/VERSION" 2>/dev/null) läuft: http://$(hostname):${port:-80}/"
