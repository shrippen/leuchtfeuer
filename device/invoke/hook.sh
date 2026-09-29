#!/bin/sh
# Dauerhafte Einstellungen des Invoke (läuft als root, gestartet von boot.sh, prüft alle 30 s):
#  - eigenes dropbear (ed25519, nur Schlüssel, eigener Host-Schlüssel) statt des
#    Original-sshd (dropbear 2016.72, Host-Schlüssel auf allen Geräten gleich)
#  - authorized_keys aus /data/invoke nach /home/root/.ssh (tmpfs über dem ro-SquashFS)
#  - Firewall (Kette INVOKE vor INPUT): nur SSH, mDNS, DHCP-Antworten, ICMP, bestehende
#    Verbindungen; am Setup-AP (p2p0) zusätzlich die Einrichtungs-Ports; weitere Ports aus
#    /data/invoke/ports.local (eine Zeile je Port: "tcp 1234" / "udp 5678")
#  - IPv6 aus (Kernel ohne ip6tables)
#  - adbd (Port 5555, root ohne Anmeldung) aus, sobald /data/invoke/disable-adb existiert
# Notbremse: /data/invoke/disable-hook anlegen -> Skript macht nichts.
D=/data/invoke
PIDF=/run/invoke-dropbear.pid
echo $$ > /run/invoke-hook.pid
log(){ echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >> $D/hook.log; }
[ -e $D/disable-hook ] && { log "disable-hook gesetzt – nichts zu tun"; exit 0; }
log "hook gestartet (pid $$)"

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

firewall(){
  iptables -C INPUT -j INVOKE 2>/dev/null && return 0
  iptables -N INVOKE 2>/dev/null || iptables -F INVOKE
  iptables -A INVOKE -i lo -j RETURN
  iptables -A INVOKE -m state --state ESTABLISHED,RELATED -j RETURN
  iptables -A INVOKE -p tcp --dport 22 -j RETURN
  iptables -A INVOKE -p udp --dport 5353 -j RETURN
  iptables -A INVOKE -p udp --sport 67 --dport 68 -j RETURN
  iptables -A INVOKE -p icmp -j RETURN
  for p in 443 12345 53; do iptables -A INVOKE -i p2p0 -p tcp --dport $p -j RETURN; done
  for p in 67 53 48301; do iptables -A INVOKE -i p2p0 -p udp --dport $p -j RETURN; done
  [ -f $D/ports.local ] && while read -r proto port _; do
    case $proto in tcp|udp) iptables -A INVOKE -p "$proto" --dport "$port" -j RETURN ;; esac
  done < $D/ports.local
  iptables -A INVOKE -j DROP
  iptables -I INPUT 1 -j INVOKE
  log "Firewall gesetzt"
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

while :; do
  [ -e $D/disable-hook ] && { log "disable-hook gesetzt – Ende"; exit 0; }
  setup_home
  ssh_up && adb_off
  firewall
  ipv6_off
  sleep 30
done
