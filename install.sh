#!/usr/bin/env bash
# Installiert den Invoke-Hack auf einem Harman Kardon Invoke mit StockRoot-Firmware
# (Barracuda_rooted_libre-11.1842.0) und macht ihn zum Netzwerk-Lautsprecher:
#   Spotify Connect, UPnP/DLNA, Sendspin (Music Assistant), Cast (nachgebildet), Tidal Connect (optional),
#   Bluetooth (BlueZ, A2DP), SSH nur mit Schlüssel, Harman-Cloud-Dienste (Cortana, OTA, …) aus.
#
#   ./install.sh --ip 192.168.1.50 --key ~/.ssh/id_ed25519.pub [Optionen]
#
# Voraussetzung: Gerät ist geflasht (docs/INSTALL.md, Teile 1-3), im WLAN, und ./build.sh ist gelaufen.
# Erstinstallation: Verbindung per adb (Port 5555, bei StockRoot offen). Danach nur noch SSH; erneutes
# Ausführen aktualisiert das Gerät (idempotent, Konfiguration auf dem Gerät bleibt erhalten).
#
# Optionen:
#   --ip IP            IP-Adresse des Invoke im WLAN (Pflicht)
#   --key FILE         öffentlicher SSH-Schlüssel (.pub), der sich als root anmelden darf (Pflicht bei
#                      Erstinstallation); der private Schlüssel liegt daneben oder im ssh-agent
#   --config FILE      Konfiguration (Vorlage: device/invoke/config.example); sie ersetzt die Datei auf dem
#                      Gerät. Ohne Angabe: bei Erstinstallation die Vorlage, bei Update unverändert
#   --no-tidal         Tidal Connect nicht installieren (proprietäres iFi-Programm, siehe README)
#   --no-reboot        am Ende nicht neu starten
#   --dry-run          nur zeigen, was getan würde
#   --yes              keine Rückfrage
set -euo pipefail
cd "$(dirname "$0")"

IP=""; KEY=""; CONFIG=""; TIDAL=1; REBOOT=1; DRY=0; YES=0
while [ $# -gt 0 ]; do
  case $1 in
    --ip) IP=$2; shift 2 ;;
    --key) KEY=$2; shift 2 ;;
    --config) CONFIG=$2; shift 2 ;;
    --no-tidal) TIDAL=0; shift ;;
    --no-reboot) REBOOT=0; shift ;;
    --dry-run) DRY=1; shift ;;
    --yes|-y) YES=1; shift ;;
    -h|--help) sed -n '2,25p' "$0"; exit 0 ;;
    *) echo "unbekannte Option: $1" >&2; exit 2 ;;
  esac
done
[ -n "$IP" ] || { echo "--ip fehlt (siehe --help)" >&2; exit 2; }

say(){ printf '\n\033[1m== %s\033[0m\n' "$*"; }
die(){ echo "FEHLER: $*" >&2; exit 1; }
run(){ if [ $DRY = 1 ]; then echo "[dry-run] $*"; else "$@"; fi; }

# ---------------------------------------------------------------- Bauergebnisse prüfen
need=(build/dropbear/dropbearmulti build/librespot/librespot build/gmrender/gmediarender
      build/sendspin/sendspin-player build/castrecv/castrecv build/btagent/btagent
      build/bluez/bluetoothd build/bluez/bluealsa build/bluez/bluealsa-aplay build/bluez/hciconfig
      build/bluez/hcitool build/bluez/lib/libsbc.so.1 build/shim/avahi-user-shim.so)
[ $TIDAL = 1 ] && need+=(build/tidal/bin/tidal_connect_application build/tidal/cert/IfiAudio_ZenStream.dat)
for f in "${need[@]}"; do [ -e "$f" ] || die "$f fehlt – zuerst ./build.sh ausführen (ohne Tidal: ./build.sh --no-tidal und ./install.sh --no-tidal)"; done

# ---------------------------------------------------------------- SSH-Optionen
SSH_ID=()
if [ -n "$KEY" ]; then
  [ -f "$KEY" ] || die "Schlüsseldatei $KEY nicht gefunden"
  # privater Schlüssel als Datei, sonst der öffentliche (ssh wählt dann den passenden aus dem Agenten)
  if [ -f "${KEY%.pub}" ]; then SSH_ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else SSH_ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
fi
KNOWN=${INVOKE_KNOWN_HOSTS:-$HOME/.ssh/known_hosts}
ssh_opts(){ echo -o BatchMode=yes -o ConnectTimeout=6 -o UserKnownHostsFile="$KNOWN" -o StrictHostKeyChecking="${STRICT:-accept-new}"; }
sshd(){ ssh $(ssh_opts) "${SSH_ID[@]}" root@"$IP" "$@"; }

# ---------------------------------------------------------------- Staging: Dateibaum wie auf dem Gerät
STAGE=$(mktemp -d); trap 'rm -rf "$STAGE"' EXIT
assemble(){
  local S=$STAGE/invoke; mkdir -p "$S"/{bin,services,bluez/bin,bluez/lib,bluez/etc/bluetooth,bluez/var,tidal}
  local d=device/invoke
  cp "$d/boot.sh" "$d/hook.sh" "$d/podium.conf" "$d/ports.local" "$d/asound-music.conf" "$d/ca-certificates.crt" "$S/"
  cp build/dropbear/dropbearmulti "$S/"
  cp build/librespot/librespot build/gmrender/gmediarender build/sendspin/sendspin-player \
     build/castrecv/castrecv build/btagent/btagent "$S/bin/"
  cp build/bluez/{bluetoothd,bluealsa,bluealsa-aplay,hciconfig,hcitool} "$S/bluez/bin/"
  cp build/bluez/lib/libsbc.so.1 "$S/bluez/lib/"
  cp "$d/services/"{librespot,gmrender,sendspin,castrecv,volume-sync,bluetooth-1-bluetoothd,bluetooth-2-agent,bluetooth-3-bluealsa,bluetooth-4-aplay}.sh "$S/services/"
  if [ $TIDAL = 1 ]; then
    cp -a build/tidal/bin build/tidal/cert build/tidal/lib build/tidal/sbin "$S/tidal/"
    cp build/shim/avahi-user-shim.so "$S/tidal/lib/"
    cp "$d/tidal/avahi-daemon.conf" "$d/tidal/dbus-system.conf" "$S/tidal/"
    cp "$d/services/"tidal-{1-dbus,2-avahi,3-connect}.sh "$S/services/"
  fi
  # Name im BlueZ-main.conf passend zur Konfiguration
  local name
  name=$( [ -n "$CONFIG" ] && . "$CONFIG" >/dev/null 2>&1 && echo "${DEVICE_NAME:-}" || true )
  cp "$d/bluez/main.conf" "$S/bluez/etc/bluetooth/main.conf"
  [ -n "$name" ] && sed -i "s/^Name = .*/Name = $name/" "$S/bluez/etc/bluetooth/main.conf"
  [ -n "$CONFIG" ] && cp "$CONFIG" "$S/config"
  [ -n "$KEY" ] && cp "$KEY" "$S/authorized_keys"
  chmod 755 "$S"/boot.sh "$S"/hook.sh "$S"/dropbearmulti "$S"/bin/* "$S"/bluez/bin/* "$S"/services/*.sh
  find "$S/tidal" -name '*.so*' -exec chmod 755 {} + 2>/dev/null || true
  chmod 755 "$S"/tidal/bin/* "$S"/tidal/sbin/* 2>/dev/null || true
}
assemble

# ---------------------------------------------------------------- Verbindung: Update (ssh) oder Erstinstallation (adb)
say "Verbindung zu $IP"
MODE=""
if sshd true 2>/dev/null; then MODE=update; echo "SSH mit Schlüssel geht: Update"; else MODE=first; echo "kein SSH-Zugang: Erstinstallation über adb"; fi

if [ "$MODE" = first ]; then
  [ -n "$KEY" ] || die "Erstinstallation braucht --key (öffentlicher SSH-Schlüssel)"
  command -v adb >/dev/null || die "adb fehlt (Android platform-tools)"
  [ -n "$CONFIG" ] || CONFIG=device/invoke/config.example
  [ -f "$CONFIG" ] || die "Konfiguration $CONFIG nicht gefunden"
  cp "$CONFIG" "$STAGE/invoke/config"
  A="$IP:5555"
  run adb connect "$A" >/dev/null
  adbsh(){ adb -s "$A" shell "$@" | tr -d '\r'; }
  if [ $DRY = 0 ]; then
    adbsh id | grep -q 'uid=0' || die "adb auf $A liefert keine root-Shell (StockRoot geflasht? Port 5555 erreichbar?)"
    fw=$(adbsh 'cat /etc/distro_version 2>/dev/null')
    case $fw in
      *rooted_libre-11.1842*) echo "Firmware: $fw" ;;
      *) die "unerwartete Firmware '$fw' (erwartet StockRoot Barracuda_rooted_libre-11.1842.0)" ;;
    esac
    free=$(adbsh "df /data | tail -1" | awk '{print $(NF-2)}')
    [ "${free:-0}" -gt 60000 ] || die "zu wenig Platz auf /data (${free:-?} KiB frei, nötig ~60 MB)"
    echo "Platz auf /data: $((free / 1024)) MiB frei"
  fi
  if [ $YES = 0 ] && [ $DRY = 0 ]; then
    echo; echo "Es wird Software auf $IP installiert; SSH-Zugang (nur Schlüssel) ersetzt die Original-Dienste."
    echo "Die Werks-Sicherung des NAND (docs/INSTALL.md, Teil 1) sollte vorhanden sein."
    read -r -p "Fortfahren? [j/N] " a; [ "$a" = j ] || [ "$a" = J ] || die "abgebrochen"
  fi
  say "Bootstrap: SSH, Autostart-Haken"
  if [ $DRY = 0 ]; then
    adbsh 'mkdir -p /data/invoke/log'
    for f in dropbearmulti boot.sh hook.sh authorized_keys config ports.local; do
      adb -s "$A" push "$STAGE/invoke/$f" "/data/invoke/$f" >/dev/null
    done
    adbsh 'chmod 755 /data/invoke/dropbearmulti /data/invoke/boot.sh /data/invoke/hook.sh; chmod 600 /data/invoke/authorized_keys'
    # eigene Host-Schlüssel (pro Gerät), nur anlegen wenn noch nicht vorhanden
    for t in ed25519 ecdsa; do
      adbsh "[ -s /data/invoke/host_$t ] || /data/invoke/dropbearmulti dropbearkey -t $t -f /data/invoke/host_$t >/dev/null 2>&1"
    done
    HOSTPUB=$(adbsh '/data/invoke/dropbearmulti dropbearkey -y -f /data/invoke/host_ed25519' | awk '/^ssh-ed25519/{print $1" "$2}')
    [ -n "$HOSTPUB" ] || die "Host-Schlüssel konnte nicht gelesen werden"
    # Haken in dnsmasq.conf (Original sichern, nur einmal anhängen)
    adbsh '[ -f /data/invoke/dnsmasq.conf.orig ] || cp /data/dnsmasq.conf /data/invoke/dnsmasq.conf.orig'
    if ! adbsh 'grep -q "Invoke-Hack: Autostart-Haken" /data/dnsmasq.conf && echo ja' | grep -q ja; then
      adbsh "printf '\n# --- Invoke-Hack: Autostart-Haken (/data/invoke/boot.sh) ---\n# dhcp-script wird wegen leasefile-ro beim Start mit \"init\" aufgerufen. Die dhcp-range liegt\n# in keinem vorhandenen Netz, dnsmasq verteilt dadurch nirgends Adressen.\ndhcp-script=/data/invoke/boot.sh\nleasefile-ro\ndhcp-range=10.254.254.10,10.254.254.20,1h\n' >> /data/dnsmasq.conf"
    fi
    # Host-Schlüssel dem Rechner bekannt machen (vor dem ersten SSH), damit kein blindes Vertrauen nötig ist
    mkdir -p "$(dirname "$KNOWN")"; touch "$KNOWN"
    ssh-keygen -R "$IP" -f "$KNOWN" >/dev/null 2>&1 || true
    echo "$IP $HOSTPUB" >> "$KNOWN"
    adbsh 'sh /data/invoke/boot.sh init' || true
    echo "Haken gestartet, warte auf SSH …"
    STRICT=yes
    for i in $(seq 1 40); do sshd true 2>/dev/null && break; sleep 3; done
    sshd true 2>/dev/null || die "SSH kommt nicht hoch (Log: adb shell cat /data/invoke/hook.log)"
    echo "SSH läuft (Host-Schlüssel geprüft)"
  fi
fi

# ---------------------------------------------------------------- Dateien übertragen
say "Dateien übertragen"
if [ $DRY = 1 ]; then
  (cd "$STAGE/invoke" && find . -type f | sort | head -70); echo "[dry-run] … übertragen per tar über SSH"
else
  # Konfiguration nur überschreiben, wenn --config gegeben oder noch keine da ist
  if [ -z "$CONFIG" ] && sshd 'test -s /data/invoke/config' 2>/dev/null; then rm -f "$STAGE/invoke/config"; fi
  # authorized_keys nur ersetzen, wenn --key gegeben
  # nur geänderte/neue Dateien übertragen (Prüfsummenvergleich): schneller, und bei schwachem WLAN nötig
  (cd "$STAGE/invoke" && find . -type f | sort | while read -r f; do sha256sum "$f"; done) > "$STAGE/local.sha"
  sshd 'cd /data/invoke 2>/dev/null && find . -type f ! -path "./log/*" ! -path "./librespot-cache/*" ! -path "./sendspin/*" ! -path "./bluez/var/*" ! -path "./.stage/*" | sort | while read -r f; do sha256sum "$f"; done' > "$STAGE/remote.sha" 2>/dev/null || : > "$STAGE/remote.sha"
  awk 'NR==FNR{r[$2]=$1; next} !($2 in r) || r[$2]!=$1 {print $2}' "$STAGE/remote.sha" "$STAGE/local.sha" > "$STAGE/changed.txt"
  n=$(wc -l < "$STAGE/changed.txt"); echo "$n von $(wc -l < "$STAGE/local.sha") Dateien geändert oder neu"
  sshd 'rm -rf /data/invoke/.stage; mkdir -p /data/invoke/.stage'
  if [ "$n" -gt 0 ]; then
    tar -C "$STAGE/invoke" -cf - -T "$STAGE/changed.txt" | sshd 'tar -xf - -C /data/invoke/.stage'
  fi
  say "Dateien in Kraft setzen"
  sshd 'sh -s' <<'REMOTE'
set -e
cd /data/invoke/.stage
# podium.conf und CA-Bestand sind per Bind-Mount eingebunden: in place schreiben (gleicher Inode),
# sonst sieht der Mount die neue Datei nicht
PODIUM_CHANGED=0
if [ -f podium.conf ]; then
  cmp -s podium.conf /data/invoke/podium.conf 2>/dev/null || PODIUM_CHANGED=1
  cat podium.conf > /data/invoke/podium.conf; rm podium.conf
fi
[ -f ca-certificates.crt ] && { cat ca-certificates.crt > /data/invoke/ca-certificates.crt; rm ca-certificates.crt; }
# alle übrigen Dateien per mv (neuer Inode, laufende Programme stören nicht: kein "Text file busy")
find . -type f | while read -r f; do
  d=/data/invoke/$(dirname "$f"); mkdir -p "$d"; mv -f "$f" "/data/invoke/$f"
done
cd /data/invoke && rm -rf .stage
chmod 600 authorized_keys 2>/dev/null || true
echo "$PODIUM_CHANGED" > /run/invoke-podium-changed
REMOTE
  # nicht mehr gewünschte Tidal-Dienste entfernen
  if [ $TIDAL = 0 ]; then sshd 'rm -f /data/invoke/services/tidal-*.sh'; fi
  # alte Test-/Altlasten aus früheren Ständen
  sshd 'rm -f /data/invoke/bin/sendspin-player.old /data/invoke/podium.conf.vor-bluez'
  # Hook neu starten (neuer Code), podium ggf. neu starten
  say "Hook neu starten"
  sshd 'sh -s' <<'REMOTE'
pid=$(cat /run/invoke-hook.pid 2>/dev/null); [ -n "$pid" ] && kill "$pid" 2>/dev/null
sleep 1
if grep -q " /etc/podium/podium.conf " /proc/mounts && [ "$(cat /run/invoke-podium-changed 2>/dev/null)" = 1 ]; then
  stop podium; sleep 3; start podium
fi
sh /data/invoke/boot.sh init
REMOTE
  # adbd dauerhaft aus (Port 5555 = root-Shell ohne Anmeldung)
  sshd 'touch /data/invoke/disable-adb'
fi

# ---------------------------------------------------------------- Neustart und Prüfung
if [ $REBOOT = 1 ] && [ $DRY = 0 ]; then
  say "Neustart"
  sshd '/bin/reboot' >/dev/null 2>&1 || true
  sleep 40
  for i in $(seq 1 60); do sshd true 2>/dev/null && break; sleep 5; done
  sleep 70   # der Haken startet die Dienste in den ersten ~60-90 s
fi
if [ $DRY = 0 ]; then
  say "Prüfung"
  ./scripts/verify-install.sh --ip "$IP" ${KEY:+--key "$KEY"} || true
fi
say "Fertig"
cat <<EOF
Anmelden:   ssh ${KEY:+-i ${KEY%.pub} }root@$IP
Bluetooth:  Gerät am Handy koppeln (Name aus der Konfiguration, Standard "HK Invoke")
Config:     /data/invoke/config auf dem Gerät (Name, Sendspin-Server), danach Dienste neu starten/booten
Notbremse:  ssh root@$IP 'touch /data/invoke/disable-hook' und neu starten (Original-Verhalten)
EOF
