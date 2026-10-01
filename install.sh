#!/usr/bin/env bash
# Installs Leuchtfeuer on a Harman Kardon Invoke running the StockRoot firmware
# (Barracuda_rooted_libre-11.1842.0) and turns it into a network speaker:
#   Spotify Connect, UPnP/DLNA, Sendspin (Music Assistant), Cast (emulated), Tidal Connect (optional),
#   Bluetooth (BlueZ, A2DP), SSH with key only, Harman cloud services (Cortana, OTA, ...) off.
#
#   ./install.sh                         interactive: asks and explains every step (default)
#   ./install.sh --non-interactive --ip 192.168.1.50 --key ~/.ssh/id_ed25519.pub [options]
#
# Prerequisite: the speaker is flashed (docs/INSTALL.md parts 1-3) and in your Wi-Fi.
# First install talks to the speaker over adb (port 5555, open on StockRoot) and from then on over SSH;
# running it again updates the speaker (idempotent, only changed files are transferred, the speaker's
# configuration is kept).
#
# Options (all optional in interactive mode: the script asks for what is missing):
#   --ip IP              IP address of the speaker in your Wi-Fi
#   --key FILE           public SSH key (.pub) that may log in as root; the private key must be in your
#                        ssh-agent or next to it
#   --config FILE        settings file (template: device/leuchtfeuer/config.example); replaces the one on the speaker
#   --web-password PW    password of the web interface (user admin); better: LEUCHTFEUER_WEB_PASSWORD=PW in the environment
#                        (not visible in the process list). Stored on the speaker only as a salted hash. Without it a
#                        random one is generated on first install and the existing one is kept on updates.
#                        Change it later with scripts/set-web-password.sh.
#   --tidal / --no-tidal install / skip Tidal Connect (proprietary iFi program, see README)
#   --prebuilt [TAG]     use the release package from Gitea instead of building (no Docker needed; without Tidal).
#                        TAG = release tag, default the latest. The package is checked against its .sha256 and,
#                        when docs/release-key.pub holds a key and Go is installed, against its signature.
#   --no-reboot          do not reboot at the end
#   --dry-run            only show what would be done
#   --non-interactive    never ask: use the options and defaults (aliases: --yes, -y)
#
# Messages are English or German depending on $LANG (override with LEUCHTFEUER_LANG=en|de).
set -euo pipefail
cd "$(dirname "$0")"
# shellcheck source=scripts/lib.sh
. scripts/lib.sh

IP=""; KEY=""; CONFIG=""; TIDAL=""; REBOOT=""; DRY=0; WEBPASS=${LEUCHTFEUER_WEB_PASSWORD:-}; PREBUILT=""
RELEASES=${LEUCHTFEUER_RELEASES:-https://git.arianw.de/api/v1/repos/shrippen/leuchtfeuer/releases}
while [ $# -gt 0 ]; do
  case $1 in
    --ip) IP=$2; shift 2 ;;
    --key) KEY=$2; shift 2 ;;
    --config) CONFIG=$2; shift 2 ;;
    --web-password) WEBPASS=$2; shift 2 ;;
    --tidal) TIDAL=1; shift ;;
    --no-tidal) TIDAL=0; shift ;;
    --prebuilt) PREBUILT=latest; shift; case ${1:-} in v*|[0-9]*) PREBUILT=$1; shift ;; esac ;;
    --no-reboot) REBOOT=0; shift ;;
    --dry-run) DRY=1; shift ;;
    --non-interactive|--yes|-y) INTERACTIVE=0; shift ;;
    -h|--help) sed -n '2,34p' "$0"; exit 0 ;;
    *) echo "unknown option: $1 (see --help)" >&2; exit 2 ;;
  esac
done
run(){ if [ $DRY = 1 ]; then echo "[dry-run] $*"; else "$@"; fi; }
# value of KEY="value" from a settings text (never executes anything)
cfgval(){ printf '%s\n' "$2" | sed -n "s/^$1=\"\(.*\)\"\$/\1/p" | head -1; }

STAGE=$(mktemp -d); trap 'rm -rf "$STAGE"' EXIT

# ====================================================================== welcome
if [ "$INTERACTIVE" = 1 ]; then
  say "Leuchtfeuer installer" "Leuchtfeuer-Installer"
  info "This turns your Harman Kardon Invoke into a network speaker (Spotify, UPnP/DLNA, Sendspin, Cast, Bluetooth,
optionally Tidal). I will ask a few questions and explain what happens before each step.
Nothing is flashed here: only the writable /data partition of the speaker is changed, and everything can be
removed again with ./uninstall.sh. Press Ctrl+C at any time to stop." \
"Das macht aus deinem Harman Kardon Invoke einen Netzwerk-Lautsprecher (Spotify, UPnP/DLNA, Sendspin, Cast,
Bluetooth, optional Tidal). Ich stelle ein paar Fragen und erkläre vor jedem Schritt, was passiert.
Hier wird nichts geflasht: nur die beschreibbare Partition /data des Lautsprechers wird geändert, und mit
./uninstall.sh lässt sich alles wieder entfernen. Mit Strg+C kannst du jederzeit abbrechen."
else
  say "Leuchtfeuer installer (non-interactive)" "Leuchtfeuer-Installer (nicht interaktiv)"
fi

# ====================================================================== tools
say "Step 1: checking this computer" "Schritt 1: Rechner prüfen"
note "I always need ssh and tar, and adb for the first installation (a speaker that has no SSH access yet)." \
     "Ich brauche immer ssh und tar, und adb für die Erstinstallation (Lautsprecher ohne SSH-Zugang)."
for c in ssh tar awk; do command -v $c >/dev/null || die "$c is missing" "$c fehlt"; done
ok "ssh, tar"
if command -v adb >/dev/null; then ok "adb"; else
  warn "adb not found (only needed for the first installation: Android platform-tools)" \
       "adb nicht gefunden (nur für die Erstinstallation nötig: Android platform-tools)"
fi

# ====================================================================== speaker address
say "Step 2: which speaker?" "Schritt 2: Welcher Lautsprecher?"
if [ -z "$IP" ]; then
  note "The speaker must be in your Wi-Fi. Find its IP address in your router (device list, name 'HK Invoke_...'
or 'LibreSync-...')." \
       "Der Lautsprecher muss in deinem WLAN sein. Die IP-Adresse steht im Router (Geräteliste, Name 'HK Invoke_...'
oder 'LibreSync-...')."
  ask IP "IP address of the speaker" "IP-Adresse des Lautsprechers"
fi
[ -n "$IP" ] || die "--ip is required in non-interactive mode" "--ip ist im nicht interaktiven Modus nötig"
case $IP in *[!0-9.]*) die "'$IP' is not an IPv4 address" "'$IP' ist keine IPv4-Adresse" ;; esac
if ping -c 2 -W 2 "$IP" >/dev/null 2>&1; then ok "$IP answers" "$IP antwortet"
else
  warn "$IP does not answer to ping (wrong address, speaker off, or Wi-Fi problem)" \
       "$IP antwortet nicht auf Ping (falsche Adresse, Lautsprecher aus oder WLAN-Problem)"
  ask_yn "Continue anyway?" "Trotzdem fortfahren?" n || die "aborted" "abgebrochen"
fi

# ====================================================================== SSH key
say "Step 3: SSH key" "Schritt 3: SSH-Schlüssel"
if [ -z "$KEY" ] && [ "$INTERACTIVE" = 1 ]; then choose_key; fi
[ -n "$KEY" ] && [ -f "$KEY" ] || die "no public SSH key (--key FILE)" "kein öffentlicher SSH-Schlüssel (--key DATEI)"
ok "$KEY"
if [ -f "${KEY%.pub}" ]; then SSH_ID=(-i "${KEY%.pub}" -o IdentitiesOnly=yes); else SSH_ID=(-i "$KEY" -o IdentitiesOnly=yes); fi
KNOWN=${LEUCHTFEUER_KNOWN_HOSTS:-$HOME/.ssh/known_hosts}
STRICT=accept-new
ssh_opts(){ echo -o BatchMode=yes -o ConnectTimeout=6 -o UserKnownHostsFile="$KNOWN" -o StrictHostKeyChecking="$STRICT"; }
# shellcheck disable=SC2046
sshd(){ ssh $(ssh_opts) "${SSH_ID[@]}" root@"$IP" "$@"; }

# ====================================================================== first install or update?
say "Step 4: what is on the speaker?" "Schritt 4: Was ist auf dem Lautsprecher?"
if sshd true 2>/dev/null; then
  MODE=update
  ok "SSH with your key works: Leuchtfeuer is already installed, this will be an update." \
     "SSH mit deinem Schlüssel geht: Leuchtfeuer ist schon installiert, das wird ein Update."
else
  MODE=first
  info "No SSH access with this key yet: this is a first installation. I will connect over adb (port 5555), set up
SSH and the autostart hook, and then continue over SSH." \
"Noch kein SSH-Zugang mit diesem Schlüssel: Das ist eine Erstinstallation. Ich verbinde mich über adb (Port 5555),
richte SSH und den Autostart-Haken ein und mache dann über SSH weiter."
  command -v adb >/dev/null || die "adb is required for the first installation" "adb ist für die Erstinstallation nötig"
fi

# ====================================================================== settings
if [ "$MODE" = first ] && [ "$INTERACTIVE" = 1 ]; then
  say "Before you start: your NAND backup" "Vorher: deine NAND-Sicherung"
  info "The backup from docs/INSTALL.md part 1 (backup/<time>/ in this folder) is the only copy of your speaker's
factory_setting partition (certificates, MAC, calibration). If this computer's disk fails, it is gone. Copy the
backup folder to a second place now (USB stick, NAS, cloud drive)." \
"Die Sicherung aus docs/INSTALL.md Teil 1 (backup/<zeit>/ in diesem Ordner) ist die einzige Kopie der Partition
factory_setting deines Lautsprechers (Zertifikate, MAC, Kalibrierung). Geht die Platte dieses Rechners kaputt, ist sie
weg. Kopiere den Ordner backup jetzt an einen zweiten Ort (USB-Stick, NAS, Cloud-Speicher)."
  ls -d backup/*/ 2>/dev/null | sed 's/^/    /' || true
  ask_yn "Is there a second copy of the NAND backup outside this computer?" "Gibt es eine zweite Kopie der NAND-Sicherung außerhalb dieses Rechners?" n \
    || wait_enter "Copy it now, then press Enter to continue." "Kopiere sie jetzt und drücke dann Enter."
fi

say "Step 5: settings" "Schritt 5: Einstellungen"
d_srv=""
host_tz(){ local t; t=$(timedatectl show -p Timezone --value 2>/dev/null || true); [ -n "$t" ] || t=$(cat /etc/timezone 2>/dev/null || true)
  [ -n "$t" ] || t=$(readlink /etc/localtime 2>/dev/null | sed 's|.*/zoneinfo/||'); echo "${t:-Europe/Berlin}"; }
if [ -n "$CONFIG" ]; then
  [ -f "$CONFIG" ] || die "settings file $CONFIG not found" "Einstellungsdatei $CONFIG nicht gefunden"
  ok "using $CONFIG" "verwende $CONFIG"
else
  d_name="HK Invoke"; d_srv=""; d_host="invoke"; d_bt="button"; d_tz=$(host_tz); d_air="on"; keep=0
  if [ "$MODE" = update ]; then
    cur=$(sshd 'cat /data/leuchtfeuer/config' 2>/dev/null || true)
    if [ -n "$cur" ]; then
      info "Current settings on the speaker:" "Aktuelle Einstellungen auf dem Lautsprecher:"
      printf '%s\n' "$cur" | sed 's/^/    /'
      d_name=$(cfgval DEVICE_NAME "$cur"); d_name=${d_name:-HK Invoke}
      d_srv=$(cfgval SENDSPIN_SERVER "$cur")
      d_host=$(cfgval DHCP_HOSTNAME "$cur"); d_host=${d_host:-invoke}
      d_bt=$(cfgval BLUETOOTH_PAIRING "$cur"); d_bt=${d_bt:-button}
      t=$(cfgval TIMEZONE "$cur"); d_tz=${t:-$d_tz}
      t=$(cfgval AIRPLAY "$cur"); d_air=${t:-on}
      if [ "$INTERACTIVE" = 0 ] || ask_yn "Keep these settings?" "Diese Einstellungen behalten?" y; then keep=1; fi
    else
      [ "$INTERACTIVE" = 0 ] && keep=1
    fi
  fi
  if [ $keep = 1 ]; then
    ok "settings stay as they are" "Einstellungen bleiben unverändert"
  elif [ "$INTERACTIVE" = 0 ]; then
    CONFIG="$STAGE/config.chosen"; sed "s|^TIMEZONE=.*|TIMEZONE=\"$d_tz\"|" device/leuchtfeuer/config.example > "$CONFIG"
    ok "defaults (device/leuchtfeuer/config.example, time zone $d_tz)" "Standardwerte (device/leuchtfeuer/config.example, Zeitzone $d_tz)"
  else
    note "Name shown in Spotify, UPnP/DLNA apps, Cast, Music Assistant and in the Bluetooth list." \
         "Name, unter dem der Lautsprecher in Spotify, UPnP/DLNA-Apps, Cast, Music Assistant und der Bluetooth-Liste erscheint."
    ask s_name "Speaker name" "Name des Lautsprechers" "$d_name"
    note "Sendspin lets Music Assistant stream to the speaker. Finding the server by mDNS often fails between Wi-Fi and
LAN (many routers do not forward multicast). If you use Music Assistant, enter its address as host:8927
(e.g. 192.168.1.10:8927). Leave empty for automatic discovery, or if you do not use Music Assistant." \
         "Mit Sendspin streamt Music Assistant auf den Lautsprecher. Die Suche per mDNS scheitert oft zwischen WLAN und LAN
(viele Router leiten Multicast nicht weiter). Wenn du Music Assistant nutzt, trage seine Adresse als host:8927 ein
(z. B. 192.168.1.10:8927). Leer lassen für automatische Suche oder wenn du Music Assistant nicht nutzt."
    ask s_srv "Music Assistant (Sendspin) address host:8927, empty = auto" "Music-Assistant-Adresse (Sendspin) host:8927, leer = automatisch" "$d_srv"
    note "The host name your router lists for the speaker (e.g. 'invoke' makes it reachable as invoke.lan)." \
         "Der Hostname, unter dem der Router den Lautsprecher einträgt (z. B. 'invoke' ergibt invoke.lan)."
    ask s_host "DHCP host name" "DHCP-Hostname" "$d_host"
    note "Bluetooth pairing: 'button' = the speaker is only visible and ready to pair for 2 minutes after a short press
on its Bluetooth button (already paired phones always reconnect by themselves). 'always' = permanently visible,
anyone in range can pair." \
         "Bluetooth-Kopplung: 'button' = der Lautsprecher ist nur 2 Minuten nach einem kurzen Druck auf den Bluetooth-Knopf
sichtbar und koppelbereit (schon gekoppelte Handys verbinden sich immer von selbst). 'always' = dauerhaft sichtbar,
jeder in Reichweite kann koppeln."
    ask s_bt "Bluetooth pairing (button/always)" "Bluetooth-Kopplung (button/always)" "$d_bt"
    case $s_bt in button|always) ;; *) warn "unknown value, using 'button'" "unbekannter Wert, nehme 'button'"; s_bt=button ;; esac
    note "Alarms and timers use this time zone (IANA name, e.g. Europe/Berlin). The speaker itself runs on Pacific time." \
         "Wecker und Timer nutzen diese Zeitzone (IANA-Name, z. B. Europe/Berlin). Der Lautsprecher selbst läuft auf Pacific Time."
    ask s_tz "Time zone" "Zeitzone" "$d_tz"
    if ask_yn "Enable the AirPlay receiver (iPhone, iPad, Mac)?" "AirPlay-Empfänger aktivieren (iPhone, iPad, Mac)?" "$([ "$d_air" = off ] && echo n || echo y)"; then s_air=on; else s_air=off; fi
    CONFIG="$STAGE/config.chosen"
    printf 'DEVICE_NAME="%s"\nSENDSPIN_SERVER="%s"\nDHCP_HOSTNAME="%s"\nBLUETOOTH_PAIRING="%s"\nTIMEZONE="%s"\nAIRPLAY="%s"\n' "$s_name" "$s_srv" "$s_host" "$s_bt" "$s_tz" "$s_air" > "$CONFIG"
  fi
fi

# ---- web interface password (stored on the speaker only as a salted hash; set after the files are deployed)
if [ -n "$WEBPASS" ] && ! valid_web_password "$WEBPASS"; then
  die "the web password needs at least 6 characters" "das Web-Passwort braucht mindestens 6 Zeichen"
fi
if [ -z "$WEBPASS" ] && [ "$INTERACTIVE" = 1 ]; then
  note "The web interface (http://$IP/) is protected by a login page. The speaker stores only a salted hash of the
password. If you set none, a random one is generated on the first install and shown at the end; on an update the
existing one is kept. You can change it later in the web interface (Settings) or with scripts/set-web-password.sh." \
       "Die Weboberfläche (http://$IP/) ist durch eine Anmeldeseite geschützt. Der Lautsprecher speichert nur einen gesalzenen
Hash des Passworts. Ohne Eingabe wird bei der Erstinstallation ein zufälliges erzeugt und am Ende angezeigt; bei einem
Update bleibt das vorhandene. Du kannst es später in der Weboberfläche (Einstellungen) oder mit
scripts/set-web-password.sh ändern."
  ask_web_password WEBPASS
fi
WEBPASS_SHOW=0
if [ -z "$WEBPASS" ] && [ "$MODE" = first ]; then WEBPASS=$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n'); WEBPASS_SHOW=1; fi
# a replaced settings file would lose the existing hash: carry it over
if [ "$MODE" = update ] && [ -n "$CONFIG" ]; then
  oldhash=$(cfgval WEB_PASSWORD_HASH "$(sshd 'cat /data/leuchtfeuer/config' 2>/dev/null || true)")
  if [ -n "$oldhash" ] && ! grep -q '^WEB_PASSWORD_HASH=' "$CONFIG"; then
    [ "$CONFIG" = "$STAGE/config.chosen" ] || { cp "$CONFIG" "$STAGE/config.chosen"; CONFIG="$STAGE/config.chosen"; }
    printf 'WEB_PASSWORD_HASH="%s"\n' "$oldhash" >> "$CONFIG"
  fi
fi

# ====================================================================== Tidal
has_tidal=0
if [ "$MODE" = update ] && sshd 'test -f /data/leuchtfeuer/services/tidal-3-connect.sh' 2>/dev/null; then has_tidal=1; fi
if [ -z "$TIDAL" ]; then
  if [ "$INTERACTIVE" = 1 ]; then
    say "Step 6: Tidal Connect (optional)" "Schritt 6: Tidal Connect (optional)"
    info "Tidal Connect lets the Tidal app play on the speaker. The only available program is a proprietary one from
iFi audio and it logs in to Tidal with iFi's device certificate. That is NOT licensed for this speaker and
Tidal can block it at any time. It also needs a Tidal subscription. Spotify, UPnP, Cast and Bluetooth work
without it." \
"Mit Tidal Connect spielt die Tidal-App auf dem Lautsprecher. Das einzige verfügbare Programm ist ein proprietäres
von iFi audio und meldet sich mit dem Gerätezertifikat von iFi bei Tidal an. Das ist für diesen Lautsprecher NICHT
lizenziert, und Tidal kann es jederzeit sperren. Außerdem ist ein Tidal-Abo nötig. Spotify, UPnP, Cast und
Bluetooth funktionieren auch ohne."
    if [ $has_tidal = 1 ]; then
      info "Tidal Connect is currently installed on the speaker. Answering no removes it." \
           "Tidal Connect ist derzeit auf dem Lautsprecher installiert. Mit Nein wird es entfernt."
      if ask_yn "Keep Tidal Connect (and update it)?" "Tidal Connect behalten (und aktualisieren)?" y; then TIDAL=1; else TIDAL=0; fi
    else
      if ask_yn "Install Tidal Connect?" "Tidal Connect installieren?" n; then TIDAL=1; else TIDAL=0; fi
    fi
  elif [ $has_tidal = 1 ] || { [ "$MODE" = first ] && [ -e build/tidal/bin/tidal_connect_application ]; }; then TIDAL=1
  else TIDAL=0; fi
fi

# ====================================================================== build
if [ -n "$PREBUILT" ]; then
  [ "$TIDAL" = 1 ] && warn "Tidal Connect is not part of the release package; skipped" "Tidal Connect ist nicht im Release-Paket; übersprungen"
  TIDAL=0
fi
need=(build/dropbear/dropbearmulti build/librespot/librespot build/gmrender/gmediarender
      build/sendspin/sendspin-player build/castrecv/castrecv build/btagent/btagent
      build/bluez/bluetoothd build/bluez/bluealsa build/bluez/bluealsa-aplay build/bluez/hciconfig
      build/bluez/hcitool build/bluez/lib/libsbc.so.1 build/shim/avahi-user-shim.so build/leuchtfeuerd/leuchtfeuerd build/shairport/shairport-sync build/viztap/leuchtfeuer-viz-tap.so
      build/viztap/leuchtfeuer-eq.so build/snapclient/snapclient)
if [ "$TIDAL" = 1 ]; then need+=(build/tidal/bin/tidal_connect_application build/tidal/cert/IfiAudio_ZenStream.dat); fi
missing=(); [ -n "$PREBUILT" ] || for f in "${need[@]}"; do [ -e "$f" ] || missing+=("$f"); done
if [ ${#missing[@]} -gt 0 ]; then
  say "Step 7: building the programs" "Schritt 7: Programme bauen"
  info "These programs have not been built yet: ${missing[*]}" "Diese Programme sind noch nicht gebaut: ${missing[*]}"
  note "They are cross-compiled for the speaker (ARMv7) with Docker and Go (./build.sh). This takes 20-60 minutes the
first time and needs a few GB of disk space; finished parts are skipped next time." \
       "Sie werden mit Docker und Go für den Lautsprecher (ARMv7) gebaut (./build.sh). Das dauert beim ersten Mal 20-60 Minuten
und braucht einige GB Platz; fertige Teile werden beim nächsten Mal übersprungen."
  if ask_yn "Build now?" "Jetzt bauen?" y; then
    if [ $DRY = 1 ]; then echo "[dry-run] ./build.sh"
    elif [ "$TIDAL" = 1 ]; then ./build.sh; else ./build.sh --no-tidal; fi
  else
    die "build first with ./build.sh (add --no-tidal if you skip Tidal)" "zuerst mit ./build.sh bauen (ohne Tidal: --no-tidal)"
  fi
  for f in "${need[@]}"; do [ "$DRY" = 1 ] || [ -e "$f" ] || die "$f is still missing" "$f fehlt weiterhin"; done
fi

# ====================================================================== staging (file tree as on the speaker)
# fetch_prebuilt <Ziel>: Release-Paket laden, prüfen und auspacken
fetch_prebuilt(){
  local dst=$1 api tag url sha sig pub
  command -v curl >/dev/null || die "curl is missing" "curl fehlt"
  if [ "$PREBUILT" = latest ]; then api="$RELEASES/latest"; else api="$RELEASES/tags/$PREBUILT"; fi
  say "Step 7: release package" "Schritt 7: Release-Paket"
  local js; js=$(curl -fsSL "$api") || die "release not found ($api)" "Release nicht gefunden ($api)"
  tag=$(printf '%s' "$js" | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])')
  url=$(printf '%s' "$js" | python3 -c 'import json,sys; print(next(a["browser_download_url"] for a in json.load(sys.stdin)["assets"] if a["name"].endswith("-invoke.tar.gz")))') \
    || die "release $tag has no package" "Release $tag hat kein Paket"
  info "Release $tag" "Release $tag"
  curl -fsSL -o "$STAGE/pkg.tar.gz" "$url" || die "download failed" "Download fehlgeschlagen"
  curl -fsSL -o "$STAGE/pkg.sha256" "$url.sha256" || die "checksum missing" "Prüfsumme fehlt"
  sha=$(awk '{print $1}' "$STAGE/pkg.sha256")
  [ "$(sha256sum "$STAGE/pkg.tar.gz" | awk '{print $1}')" = "$sha" ] || die "checksum does not match" "Prüfsumme stimmt nicht"
  ok "checksum" "Prüfsumme"
  pub=$(grep -v '^#' docs/release-key.pub 2>/dev/null | head -n 1 || true)
  if [ -n "$pub" ] && command -v go >/dev/null; then
    curl -fsSL -o "$STAGE/pkg.tar.gz.sig" "$url.sig" || die "signature missing" "Signatur fehlt"
    (cd src/relsign && go run . verify "$pub" "$STAGE/pkg.tar.gz") || die "signature does not match" "Signatur stimmt nicht"
    ok "signature" "Signatur"
  else
    warn "signature not checked (no key in docs/release-key.pub or no Go)" "Signatur nicht geprüft (kein Schlüssel in docs/release-key.pub oder kein Go)"
  fi
  mkdir -p "$dst"; tar -xzf "$STAGE/pkg.tar.gz" -C "$dst"
}

assemble(){
  local S=$STAGE/data d=device/leuchtfeuer name=""
  if [ "$DRY" = 1 ] && [ -z "$PREBUILT" ] && [ ! -e build/dropbear/dropbearmulti ]; then
    mkdir -p "$S"; cp "$d/boot.sh" "$d/hook.sh" "$d/apply-update.sh" "$S/"; return 0
  fi
  if [ -n "$PREBUILT" ]; then fetch_prebuilt "$S"
  elif [ "$TIDAL" = 1 ]; then scripts/assemble.sh "$S" --tidal
  else scripts/assemble.sh "$S"; fi
  # gerätebezogen: BlueZ-Konfiguration mit dem Namen aus den Einstellungen
  mkdir -p "$S/bluez/etc/bluetooth" "$S/bluez/var"
  cp "$d/bluez/main.conf" "$S/bluez/etc/bluetooth/main.conf"
  if [ -n "$CONFIG" ]; then
    name=$(cfgval DEVICE_NAME "$(cat "$CONFIG")")   # speaker name in BlueZ main.conf matches the settings
    [ -n "$name" ] && sed -i "s/^Name = .*/Name = $name/" "$S/bluez/etc/bluetooth/main.conf"
    cp "$CONFIG" "$S/config"
  fi
  # eigene Firewall-Ports: nur bei der Erstinstallation die (leere) Vorlage, sonst bleibt die Datei des Nutzers
  [ "$MODE" = first ] && cp "$d/ports.local" "$S/ports.local"
  [ "$MODE" = first ] && cp "$KEY" "$S/authorized_keys"
  # Updates aus der Weboberfläche: öffentlicher Release-Schlüssel, falls einer im Repo steht und die config neu ist
  local pub; pub=$(grep -v '^#' docs/release-key.pub 2>/dev/null | head -n 1 || true)
  if [ -n "$pub" ] && [ -f "$S/config" ] && ! grep -q '^UPDATE_PUBKEY=' "$S/config"; then
    printf 'UPDATE_PUBKEY="%s"\n' "$pub" >> "$S/config"
  fi
  return 0
}
assemble
if [ "$MODE" = first ]; then
  [ -n "$CONFIG" ] || { CONFIG=device/leuchtfeuer/config.example; cp "$CONFIG" "$STAGE/data/config"; }
fi

# ====================================================================== summary and confirmation
[ -z "$REBOOT" ] && REBOOT=1
say "Summary" "Zusammenfassung"
modetxt=$( [ "$MODE" = first ] && t 'first installation over adb' 'Erstinstallation über adb' || t 'update over SSH' 'Update über SSH' )
tidaltxt=$( [ "$TIDAL" = 1 ] && t yes ja || t no nein )
cfgtxt=$( [ -n "$CONFIG" ] && echo "$CONFIG" || t 'unchanged (on the speaker)' 'unverändert (auf dem Lautsprecher)' )
info "Speaker:   $IP ($modetxt)
SSH key:   $KEY
Tidal:     $tidaltxt
Settings:  $cfgtxt" \
"Lautsprecher: $IP ($modetxt)
SSH-Schlüssel: $KEY
Tidal:         $tidaltxt
Einstellungen: $cfgtxt"
if [ "$MODE" = first ]; then
  note "On a first installation I will: set up SSH with a new host key and your key (the vendor sshd and the open adb
root shell on port 5555 are switched off), add a few lines to /data/dnsmasq.conf (the original is saved as
dnsmasq.conf.orig) as autostart hook, switch off Harman's cloud services (Cortana, OTA, crash upload) and the vendor
Bluetooth stack, and copy about 55 MB of programs to /data/leuchtfeuer." \
       "Bei einer Erstinstallation: SSH mit neuem Host-Schlüssel und deinem Schlüssel einrichten (der Hersteller-sshd und die
offene adb-Root-Shell auf Port 5555 werden abgeschaltet), ein paar Zeilen als Autostart-Haken an /data/dnsmasq.conf
anhängen (das Original wird als dnsmasq.conf.orig gesichert), die Harman-Clouddienste (Cortana, OTA, Absturzberichte)
und den Hersteller-Bluetooth-Stack abschalten und etwa 55 MB Programme nach /data/leuchtfeuer kopieren."
fi
ask_yn "Proceed?" "Fortfahren?" y || die "aborted" "abgebrochen"

# ====================================================================== first installation: bootstrap over adb
if [ "$MODE" = first ]; then
  A="$IP:5555"
  say "Step 8: connecting over adb" "Schritt 8: Verbindung über adb"
  run adb connect "$A" >/dev/null
  adbsh(){ adb -s "$A" shell "$@" | tr -d '\r'; }
  if [ $DRY = 0 ]; then
    adbsh id | grep -q 'uid=0' || die "adb on $A does not give a root shell (is the StockRoot image flashed? is port 5555 reachable?)" \
                                       "adb auf $A liefert keine Root-Shell (StockRoot geflasht? Port 5555 erreichbar?)"
    fw=$(adbsh 'cat /etc/distro_version 2>/dev/null')
    case $fw in
      *rooted_libre-11.1842*) ok "firmware: $fw" "Firmware: $fw" ;;
      *) warn "unexpected firmware '$fw' (expected StockRoot Barracuda_rooted_libre-11.1842.0)" \
              "unerwartete Firmware '$fw' (erwartet StockRoot Barracuda_rooted_libre-11.1842.0)"
         ask_yn "Continue anyway? (not recommended)" "Trotzdem fortfahren? (nicht empfohlen)" n || die "aborted" "abgebrochen" ;;
    esac
    free=$(adbsh "df /data | tail -1" | awk '{print $(NF-2)}')
    [ "${free:-0}" -gt 60000 ] || die "not enough space on /data (${free:-?} KiB free, need about 60 MB)" \
                                       "zu wenig Platz auf /data (${free:-?} KiB frei, nötig ca. 60 MB)"
    ok "free space on /data: $((free / 1024)) MiB" "Platz auf /data: $((free / 1024)) MiB frei"
  fi

  say "Step 9: setting up SSH and the autostart hook" "Schritt 9: SSH und Autostart-Haken einrichten"
  note "Now the speaker gets its own SSH server (key login only), a firewall and the supervisor script that starts and
watches all services. A host key is generated on the speaker so I can check I am talking to the right device." \
       "Jetzt bekommt der Lautsprecher seinen eigenen SSH-Server (nur Schlüssel), eine Firewall und das Überwachungsskript, das
alle Dienste startet und beobachtet. Auf dem Lautsprecher wird ein Host-Schlüssel erzeugt, damit ich prüfen kann, dass
ich mit dem richtigen Gerät spreche."
  if [ $DRY = 0 ]; then
    adbsh 'mkdir -p /data/leuchtfeuer/log'
    for f in dropbearmulti boot.sh hook.sh apply-update.sh authorized_keys config ports.local; do
      adb -s "$A" push "$STAGE/data/$f" "/data/leuchtfeuer/$f" >/dev/null
    done
    adbsh 'chmod 755 /data/leuchtfeuer/dropbearmulti /data/leuchtfeuer/boot.sh /data/leuchtfeuer/hook.sh /data/leuchtfeuer/apply-update.sh; chmod 600 /data/leuchtfeuer/authorized_keys'
    for k in ed25519 ecdsa; do
      adbsh "[ -s /data/leuchtfeuer/host_$k ] || /data/leuchtfeuer/dropbearmulti dropbearkey -t $k -f /data/leuchtfeuer/host_$k >/dev/null 2>&1"
    done
    HOSTPUB=$(adbsh '/data/leuchtfeuer/dropbearmulti dropbearkey -y -f /data/leuchtfeuer/host_ed25519' | awk '/^ssh-ed25519/{print $1" "$2}')
    [ -n "$HOSTPUB" ] || die "could not read the host key" "Host-Schlüssel konnte nicht gelesen werden"
    adbsh '[ -f /data/leuchtfeuer/dnsmasq.conf.orig ] || cp /data/dnsmasq.conf /data/leuchtfeuer/dnsmasq.conf.orig'
    if ! adbsh 'grep -q "Autostart-Haken (/data/leuchtfeuer/boot.sh)" /data/dnsmasq.conf && echo yes' | grep -q yes; then
      adbsh "printf '\n# --- Leuchtfeuer: Autostart-Haken (/data/leuchtfeuer/boot.sh) ---\n# dhcp-script wird wegen leasefile-ro beim Start mit \"init\" aufgerufen. Die dhcp-range liegt\n# in keinem vorhandenen Netz, dnsmasq verteilt dadurch nirgends Adressen.\ndhcp-script=/data/leuchtfeuer/boot.sh\nleasefile-ro\ndhcp-range=10.254.254.10,10.254.254.20,1h\n' >> /data/dnsmasq.conf"
    fi
    mkdir -p "$(dirname "$KNOWN")"; touch "$KNOWN"
    ssh-keygen -R "$IP" -f "$KNOWN" >/dev/null 2>&1 || true
    echo "$IP $HOSTPUB" >> "$KNOWN"
    adbsh 'sh /data/leuchtfeuer/boot.sh init' || true
    info "Hook started, waiting for SSH ..." "Haken gestartet, warte auf SSH ..."
    STRICT=yes
    for _ in $(seq 1 40); do sshd true 2>/dev/null && break; sleep 3; done
    sshd true 2>/dev/null || die "SSH does not come up (log: adb shell cat /data/leuchtfeuer/hook.log)" "SSH kommt nicht hoch (Log: adb shell cat /data/leuchtfeuer/hook.log)"
    ok "SSH is up, host key verified" "SSH läuft, Host-Schlüssel geprüft"
  fi
fi

# ====================================================================== transfer files
say "Step 10: copying files" "Schritt 10: Dateien kopieren"
note "Only files that differ from the speaker are sent (checksum comparison), so a slow or flaky Wi-Fi is no problem
and you can simply run the installer again after an interruption." \
     "Es werden nur Dateien gesendet, die sich vom Lautsprecher unterscheiden (Prüfsummenvergleich); ein langsames oder
wackeliges WLAN ist daher kein Problem, und nach einer Unterbrechung kann man den Installer einfach nochmal starten."
if [ $DRY = 1 ]; then
  (cd "$STAGE/data" && find . -type f | sort | head -70); echo "[dry-run] ... transfer via tar over SSH"
else
  # ältere Installationen (/data/invoke, invoked): erst umziehen, dann vergleichen
  sshd 'sh -s' < device/leuchtfeuer/migrate.sh || true
  (cd "$STAGE/data" && find . -type f | sort | while read -r f; do sha256sum "$f"; done) > "$STAGE/local.sha"
  sshd 'cd /data/leuchtfeuer 2>/dev/null && find . -type f ! -path "./log/*" ! -path "./librespot-cache/*" ! -path "./sendspin/*" ! -path "./bluez/var/*" ! -path "./.stage/*" ! -path "./.prev/*" ! -name sessions.json ! -name leuchtfeuerd.json ! -name "web-tls.*" ! -name update-pending ! -name update-rolledback | sort | while read -r f; do sha256sum "$f"; done' > "$STAGE/remote.sha" 2>/dev/null || : > "$STAGE/remote.sha"
  awk 'NR==FNR{r[$2]=$1; next} !($2 in r) || r[$2]!=$1 {print $2}' "$STAGE/remote.sha" "$STAGE/local.sha" > "$STAGE/changed.txt"
  n=$(wc -l < "$STAGE/changed.txt"); total=$(wc -l < "$STAGE/local.sha")
  info "$n of $total files are new or changed" "$n von $total Dateien sind neu oder geändert"
  sshd 'rm -rf /data/leuchtfeuer/.stage; mkdir -p /data/leuchtfeuer/.stage'
  if [ "$n" -gt 0 ]; then
    tar -C "$STAGE/data" -cf - -T "$STAGE/changed.txt" | sshd 'tar -xf - -C /data/leuchtfeuer/.stage'
  fi
  say "Step 11: activating the files" "Schritt 11: Dateien in Kraft setzen"
  note "New files are moved into place (running programs are not disturbed), the autostart hook is restarted, and the
adb root shell is closed for good." \
       "Neue Dateien werden an ihren Platz verschoben (laufende Programme werden nicht gestört), der Autostart-Haken wird neu
gestartet, und die adb-Root-Shell wird dauerhaft geschlossen."
  # apply-update.sh (aus der Stufe, sonst das installierte) sichert den alten Stand nach .prev, setzt die neuen Dateien
  # in Kraft und meldet das Update dem Hook: fällt danach ein Dienst wiederholt aus, kommt der alte Stand zurück.
  WATCHARG=""; [ "$MODE" = first ] && WATCHARG=nowatch
  sshd "sh -s -- $WATCHARG" <<'REMOTE'
set -e
A=/data/leuchtfeuer/.stage/apply-update.sh; [ -f "$A" ] || A=/data/leuchtfeuer/apply-update.sh
if [ -f "$A" ]; then sh "$A" apply /data/leuchtfeuer/.stage norestart ${1:-}
else
  # sehr alte Installation ohne apply-update.sh: Dateien direkt verschieben
  cd /data/leuchtfeuer/.stage
  [ -f podium.conf ] && { cmp -s podium.conf /data/leuchtfeuer/podium.conf 2>/dev/null || echo 1 > /run/leuchtfeuer-podium-changed; cat podium.conf > /data/leuchtfeuer/podium.conf; rm podium.conf; }
  [ -f ca-certificates.crt ] && { cat ca-certificates.crt > /data/leuchtfeuer/ca-certificates.crt; rm ca-certificates.crt; }
  find . -type f | while read -r f; do d=/data/leuchtfeuer/$(dirname "$f"); mkdir -p "$d"; mv -f "$f" "/data/leuchtfeuer/$f"; done
  cd /data/leuchtfeuer && rm -rf .stage
fi
chmod 600 /data/leuchtfeuer/authorized_keys 2>/dev/null || true
REMOTE
  # update: make sure the key is among the authorized keys (append, never remove other keys)
  if [ "$MODE" = update ]; then
    sshd 'k=$(cat); grep -qxF "$k" /data/leuchtfeuer/authorized_keys 2>/dev/null || echo "$k" >> /data/leuchtfeuer/authorized_keys' < "$KEY"
  fi
  if [ "$TIDAL" = 0 ]; then sshd 'rm -f /data/leuchtfeuer/services/tidal-*.sh'; fi
  # ports.local älterer Versionen enthielt die Ports aller Dienste: durch die leere Vorlage ersetzen (der Hook öffnet
  # die Ports jetzt je nach eingeschalteten Diensten). Eigene Änderungen bleiben.
  sshd 'f=/data/leuchtfeuer/ports.local; [ -f $f ] && [ "$(md5sum < $f | cut -d" " -f1)" = 10189e266e2db551527e95196cce327b ] && cat > $f' < device/leuchtfeuer/ports.local || true
  sshd 'rm -f /data/leuchtfeuer/bin/sendspin-player.old /data/leuchtfeuer/podium.conf.vor-bluez'
  sshd 'sh -s' <<'REMOTE'
pid=$(cat /run/leuchtfeuer-hook.pid 2>/dev/null); [ -n "$pid" ] && kill "$pid" 2>/dev/null
sleep 1
if grep -q " /etc/podium/podium.conf " /proc/mounts && [ "$(cat /run/leuchtfeuer-podium-changed 2>/dev/null)" = 1 ]; then
  stop podium; sleep 3; start podium
fi
sh /data/leuchtfeuer/boot.sh init
REMOTE
  # new password: leuchtfeuerd hashes it on the speaker (stdin, never the command line) and is restarted by the hook
  if [ -n "$WEBPASS" ]; then
    printf '%s\n' "$WEBPASS" | sshd '/data/leuchtfeuer/bin/leuchtfeuerd -set-password && { p=$(cat /run/leuchtfeuer-svc-leuchtfeuerd.pid 2>/dev/null); [ -n "$p" ] && kill "$p"; true; }' \
      && ok "web password set (stored as a salted hash)" "Web-Passwort gesetzt (nur als gesalzener Hash gespeichert)" \
      || warn "could not set the web password: use scripts/set-web-password.sh" "Web-Passwort konnte nicht gesetzt werden: scripts/set-web-password.sh nutzen"
  fi
  sshd 'touch /data/leuchtfeuer/disable-adb'
fi

# ====================================================================== reboot and verification
if [ $DRY = 0 ]; then
  say "Step 12: restart" "Schritt 12: Neustart"
  note "A restart makes sure everything starts cleanly from scratch (this also tests that the speaker comes up on its
own). It takes about 3 minutes; the services start about 90 seconds after the speaker is back." \
       "Ein Neustart stellt sicher, dass alles sauber von vorn startet (und testet, dass der Lautsprecher von selbst hochkommt).
Das dauert etwa 3 Minuten; die Dienste starten etwa 90 Sekunden nach dem Hochfahren."
  if [ "$REBOOT" = 1 ] && ask_yn "Restart the speaker now?" "Lautsprecher jetzt neu starten?" y; then
    sshd '/bin/reboot' >/dev/null 2>&1 || true
    info "Waiting for the speaker ..." "Warte auf den Lautsprecher ..."
    sleep 40
    for _ in $(seq 1 60); do sshd true 2>/dev/null && break; sleep 5; done
    sleep 70
  else
    info "No restart. The services are running, but a restart is recommended soon (the audio chain with the visualizer tap is only used by services started afterwards)." "Kein Neustart. Die Dienste laufen, ein Neustart wird aber bald empfohlen (die Tonkette mit dem Visualizer-Abgriff nutzen erst später gestartete Dienste)."
  fi
  say "Step 13: checking" "Schritt 13: Prüfung"
  ./scripts/verify-install.sh --ip "$IP" --key "$KEY" || true
fi

# ====================================================================== done
say "Done" "Fertig"
CFGSRV=${d_srv:-}; [ -n "$CONFIG" ] && CFGSRV=$(cfgval SENDSPIN_SERVER "$(cat "$CONFIG")")
if [ -n "$CFGSRV" ]; then mahint=$(t "Sendspin connects to $CFGSRV." "Sendspin verbindet sich mit $CFGSRV.")
else mahint=$(t "if it is not found automatically, set SENDSPIN_SERVER on the speaker (see settings)." "falls er nicht automatisch gefunden wird, SENDSPIN_SERVER auf dem Lautsprecher setzen (siehe Einstellungen).")
fi
if [ $WEBPASS_SHOW = 1 ]; then WEBPW=$(t "password $WEBPASS (shown only now; change it in Settings)" "Passwort $WEBPASS (wird nur jetzt angezeigt; in den Einstellungen änderbar)")
else WEBPW=$(t "the password you set, or the existing one (reset: scripts/set-web-password.sh)" "das von dir gesetzte oder vorhandene Passwort (zurücksetzen: scripts/set-web-password.sh)"); fi
info "What now:
  - Web interface: http://$IP/  (login page, $WEBPW):
    status and now playing, web radio, alarms (sunrise light, holidays), timers, sleep timer, sound (bass, treble,
    loudness, night mode), source rules and volume limits, button mapping, Wi-Fi guard, Home Assistant, light ring,
    services on/off, backup and restore, updates.
  - Light ring visualizer: off by default; switch it on in Settings > Light ring (needs the restart of the speaker).
  - Bluetooth: press the speaker's Bluetooth button briefly, then pair it on your phone within 2 minutes (no PIN).
    It stays paired and reconnects by itself.
  - Spotify / UPnP / Cast / AirPlay / Tidal: pick the speaker by its name in the app (same Wi-Fi).
  - Music Assistant: $mahint
  - Log in:  ssh -i ${KEY%.pub} root@$IP
  - Settings: /data/leuchtfeuer/config on the speaker; logs: /data/leuchtfeuer/log/
  - Emergency brake: ssh root@$IP 'touch /data/leuchtfeuer/disable-hook' and reboot = original behaviour.
  - Remove again: ./uninstall.sh" \
"Wie weiter:
  - Weboberfläche: http://$IP/  (Anmeldeseite, $WEBPW):
    Status und „Läuft gerade“, Webradio, Wecker (Lichtwecker, Feiertage), Timer, Schlummertimer, Klang (Bass, Höhen,
    Loudness, Nachtmodus), Quellen-Regel und Lautstärkegrenzen, Tastenbelegung, WLAN-Wächter, Home Assistant, Leuchtring,
    Dienste an/aus, Sichern und Wiederherstellen, Updates.
  - Leuchtring-Visualizer: anfangs aus; einschalten unter Einstellungen > Leuchtring (braucht den Neustart des Lautsprechers).
  - Bluetooth: den Bluetooth-Knopf am Lautsprecher kurz drücken und ihn innerhalb von 2 Minuten am Handy koppeln
    (ohne PIN). Er bleibt gekoppelt und verbindet sich selbst wieder.
  - Spotify / UPnP / Cast / AirPlay / Tidal: den Lautsprecher in der App über seinen Namen wählen (gleiches WLAN).
  - Music Assistant: $mahint
  - Anmelden: ssh -i ${KEY%.pub} root@$IP
  - Einstellungen: /data/leuchtfeuer/config auf dem Lautsprecher; Logs: /data/leuchtfeuer/log/
  - Notbremse: ssh root@$IP 'touch /data/leuchtfeuer/disable-hook' und neu starten = Originalverhalten.
  - Wieder entfernen: ./uninstall.sh"
