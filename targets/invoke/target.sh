# Zielgerät Harman Kardon Invoke (Marvell BG2CD, Android-init, Harman-Software "podium"). Vom Hook geladen
# (device/leuchtfeuer/hook.sh, Schnittstelle dort und in docs/TARGETS.md).
#  - eigenes dropbear (ed25519, nur Schlüssel, eigener Host-Schlüssel) statt des Original-sshd (dropbear 2016.72,
#    Host-Schlüssel auf allen Geräten gleich); authorized_keys aus $D nach /home/root/.ssh (tmpfs über dem ro-SquashFS)
#  - adbd (Port 5555, root ohne Anmeldung) aus, sobald $D/disable-adb existiert
#  - IPv6 aus (Kernel ohne ip6tables)
#  - Firewall: zusätzlich die Einrichtungs-Ports am Setup-AP (p2p0)
#  - DHCP-Name aus DHCP_HOSTNAME (z. B. "invoke" -> invoke.lan im Router): das Libre-dhcpcd meldet "LibreSync-<nr>",
#    das der Router nicht einträgt. Deshalb nach dem Start und alle 6 h eine zusätzliche DHCP-Anfrage mit busybox
#    udhcpc (-s /bin/true: ändert nichts an der Schnittstelle, fragt nur dieselbe Adresse mit dem Namen an).
#  - Harman-Dienste kürzen: $D/podium.conf per Bind-Mount über /etc/podium/podium.conf, dann init-Dienst
#    "podium" (system-manager) einmal neu starten (ohne Cortana, Harman-Spotify, OTA, Absturzbericht-Upload)
#  - aktueller CA-Bestand per Bind-Mount
# shellcheck shell=sh disable=SC2034
TARGET_ID=invoke
TARGET_NAME="HK Invoke"
TARGET_FIREWALL=on
TARGET_IFACE=wlan0
# System-Bus des Geräts: das ALSA-Plugin "bluealsa" (Ausgabe an Bluetooth-Lautsprecher) in den Audiodiensten findet bluealsa dort
TARGET_DBUS=unix:path=/run/dbus/system_bus_socket
PIDF=$R/leuchtfeuer-dropbear.pid

setup_home(){
  grep -q ' /home/root tmpfs ' /proc/mounts || mount -t tmpfs -o mode=700,size=1m tmpfs /home/root
  mkdir -p /home/root/.ssh && chmod 700 /home/root/.ssh
  cmp -s $D/authorized_keys /home/root/.ssh/authorized_keys || {
    cp $D/authorized_keys /home/root/.ssh/authorized_keys; chmod 600 /home/root/.ssh/authorized_keys
    log "authorized_keys aktualisiert"; }
}

ours_up(){ p=$(cat $PIDF 2>/dev/null); [ -n "$p" ] && kill -0 "$p" 2>/dev/null; }

ssh_up(){
  ours_up && return 0
  stop sshd; sleep 1
  $D/dropbearmulti dropbear -p 22 -r $D/host_ed25519 -r $D/host_ecdsa -P $PIDF 2>>$D/hook.log
  sleep 2
  if ours_up; then log "eigenes dropbear läuft (pid $(cat $PIDF))"; return 0; fi
  log "FEHLER: eigenes dropbear startet nicht – Original-sshd und adbd wieder an"
  start sshd; start adbd; return 1
}

ipv6_off(){
  for f in /proc/sys/net/ipv6/conf/*/disable_ipv6; do
    [ "$(cat $f)" = 1 ] || echo 1 > $f
  done
}

adb_off(){
  [ -e $D/disable-adb ] || return 0
  # getprop braucht ANDROID_PROPERTY_WORKSPACE (fehlt hier), stop geht über den init-Socket
  pidof adbd >/dev/null 2>&1 && { stop adbd; sleep 1; log "adbd gestoppt"; }
}

dhcp_name(){
  n=$(cfg DHCP_HOSTNAME)
  [ -n "$n" ] || n=$(cat $D/hostname 2>/dev/null)
  [ -n "$n" ] || return 0
  ip=$(ip -4 addr show wlan0 2>/dev/null | awk '/inet /{sub(/\/.*/,"",$2); print $2; exit}')
  [ -n "$ip" ] || return 1
  if busybox udhcpc -i wlan0 -f -q -n -t 4 -T 3 -r "$ip" -x hostname:"$n" -F "$n" -s /bin/true >/dev/null 2>&1; then
    log "DHCP-Name $n für $ip gemeldet"; return 0
  fi
  log "DHCP-Name $n: keine Antwort"; return 1
}

podium_trim(){
  [ -s $D/podium.conf ] || return 0
  grep -q ' /etc/podium/podium.conf ' /proc/mounts && return 0
  mount --bind $D/podium.conf /etc/podium/podium.conf || { log "FEHLER: Bind-Mount podium.conf"; return 1; }
  stop podium; sleep 3; start podium
  log "Harman-Dienste mit gekürzter podium.conf neu gestartet"
}

# Aktueller CA-Bestand (der eingebaute ist von 2018 und kennt Let's Encrypt nicht): Musikdienste
# wie gmrender laden sonst https-Adressen (Navidrome u. a.) nicht. Quelle: tools/update-ca-bundle.sh
ca_bundle(){
  [ -s $D/ca-certificates.crt ] || return 0
  grep -q ' /etc/ssl/certs/ca-certificates.crt ' /proc/mounts && return 0
  mount --bind $D/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt || { log "FEHLER: Bind-Mount CA-Bestand"; return 1; }
  log "CA-Bestand aus $D eingebunden"
}

target_fw_rules(){
  for p in 443 12345 53; do echo "-i p2p0 -p tcp --dport $p -j RETURN"; done
  for p in 67 53 48301; do echo "-i p2p0 -p udp --dport $p -j RETURN"; done
}

# dnsmasq des Herstellers fragt den Router und 8.8.8.8. Für lokale Namen (z. B. ploetze.lan) antwortet 8.8.8.8 mit
# NXDOMAIN, und dnsmasq nimmt mitunter diese Antwort: der Name ist dann nur per IPv6 (oder gar nicht) auflösbar und
# Sendspin, UPnP-Server usw. sind unerreichbar. strict-order fragt die Server der Reihe nach (Router zuerst); gilt ab
# dem nächsten Start von dnsmasq. Die Deinstallation stellt dnsmasq.conf.orig wieder her.
dns_strict(){
  f=${LEUCHTFEUER_DNSMASQ:-/data/dnsmasq.conf}
  [ -f "$f" ] && ! grep -q '^strict-order' "$f" || return 0
  printf '\n# Leuchtfeuer: Nameserver der Reihe nach (Router zuerst), damit lokale Namen aufgelöst werden\nstrict-order\n' >> "$f" &&
    log "dnsmasq: strict-order ergänzt (gilt ab dem nächsten Start)"
}

target_init(){
  podium_trim
  ca_bundle
  dns_strict
}

target_tick(){
  setup_home
  ssh_up && adb_off
  ipv6_off
}

target_net(){ dhcp_name; }
