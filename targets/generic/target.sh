# Zielgerät "generic": beliebiges Linux mit ALSA (Raspberry Pi, alter Rechner, Mini-PC). Vom Hook geladen
# (device/leuchtfeuer/hook.sh, Schnittstelle dort und in docs/TARGETS.md). SSH, Netz, Uhr und Bluetooth-Stack
# kommen vom System; Leuchtfeuer bringt nur seine Dienste mit. Firewall standardmäßig aus (FIREWALL="on" schaltet
# die Kette LEUCHTFEUER ein, braucht iptables).
# shellcheck shell=sh disable=SC2034
TARGET_ID=generic
TARGET_NAME=Leuchtfeuer
TARGET_FIREWALL=off
TARGET_IFACE=''
# Tonkette per ALSA_CONFIG_PATH hinter die Grundkonfiguration (Debian, Fedora, Arch: /usr/share/alsa/alsa.conf)
TARGET_ALSA_BASE=/usr/share/alsa/alsa.conf

# Netz bereit, sobald es eine Standardroute gibt
target_net(){ ip route 2>/dev/null | grep -q '^default'; }
