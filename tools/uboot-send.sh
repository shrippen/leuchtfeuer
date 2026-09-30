#!/usr/bin/env bash
# Schickt einen Befehl an die U-Boot-Konsole von tools/usb-flash.sh.
#   tools/uboot-send.sh "<befehl>"
#   tools/uboot-send.sh ramdisk   -> Sicherungs-Ramdisk (alle mtd read-only) aus dem RAM booten
set -euo pipefail
work=${TMPDIR:-/tmp}/invoke-flash.current
fifo=$work/console.in
[ -p "$fifo" ] || { echo "Keine laufende Sitzung von tools/usb-flash.sh" >&2; exit 1; }
log=$(cat "$work/.log")
send(){ printf '%s\r' "$1" > "$fifo"; }

# Schutz: "l2nand 83" (ohne -m) löscht den GANZEN NAND, auch factory_setting (gerätespezifische Zertifikate,
# MAC, Kalibrierung). Die steht in keinem Abbild und ist ohne eigene Sicherung unwiederbringlich verloren.
if printf '%s' "$1" | grep -Eq '^l2nand( |$)' && ! printf '%s' "$1" | grep -Eq '^l2nand +-m '; then
  echo "Abbruch: 'l2nand' ohne -m löscht den ganzen NAND inkl. factory_setting. Stattdessen: l2nand -m 83" >&2
  [ "${ALLOW_FULL_ERASE:-}" = 1 ] || exit 4
fi

if [ "$1" = ramdisk ]; then
  size=$(cat "$work/.ramdisk_size")
  mtd="mtdparts=mv_nand:128K(block0)ro,1M(pre-bootloader)ro,2M(post-bootloader)ro,2M(postbootloaderB)ro,5M(factory_setting)ro,5M(tz_en)ro,1M(tz_en-B)ro,10M(bootimgs_B)ro,5M(bsl)ro,10M(bootimgs)ro,90M(rootfs)ro,123M(app)ro,1M(fw_stat)ro,896K(tail)ro,256M@0(nand)ro"
  n=$(stat -c %s "$log"); send "usbload 0x81 0x0c400000"; sleep 1
  for _ in $(seq 120); do tail -c +$((n+1)) "$log" | grep -q 'all done' && break; sleep 1; done
  n=$(stat -c %s "$log"); send "usbload 0x82 0x08000000"; sleep 1
  for _ in $(seq 120); do tail -c +$((n+1)) "$log" | grep -q 'all done' && break; sleep 1; done
  sleep 1
  send "set bootargs console=ttyS0,115200 debug init=/bin/sh root=/dev/ram $mtd initrd=0x08000000,$size"; sleep 1
  send "bootm 0x0c400000"
else
  send "$1"
fi
