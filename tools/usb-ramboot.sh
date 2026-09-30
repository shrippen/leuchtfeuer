#!/usr/bin/env bash
# Bootet den Invoke über den Service-USB-Port NUR IN DEN RAM (NAND bleibt unberührt).
#   tools/usb-ramboot.sh uboot    -> hält an der U-Boot-Konsole (telnet 127.0.0.1:8141)
#   tools/usb-ramboot.sh backup   -> SDK-Kernel + eigene Sicherungs-Ramdisk (hängt nichts ein,
#                                    alle mtd-Partitionen read-only), danach scripts/nand-backup.sh
#   tools/usb-ramboot.sh ramdisk  -> Original-SDK-Ramdisk (hängt Partitionen ein – NICHT verwenden)
#
# Ablauf (Flashing-Modus laut Harman-Anleitung):
#   1. Skript starten.
#   2. Invoke vom Strom trennen, USB-Kabel zum Rechner stecken lassen.
#   3. Reset-Loch (Büroklammer) gedrückt halten, Strom wieder anstecken.
#   4. Weiter Reset halten und innerhalb von 5 s genau 4x die Mic-aus-Taste drücken.
#   5. Leuchtring wird gelb -> Reset loslassen, sobald die Konsole/U-Boot erscheint.
# Die Konsole wird nach recon/uboot-<zeit>.log mitgeschrieben.
set -euo pipefail
mode=${1:-uboot}
here=$(cd "$(dirname "$0")/.." && pwd)
src=$here/firmware/extracted/flashing/marvell_flash_tool
[ -f "$src/usb_boot" ] || { echo "Flash-Tool fehlt: scripts/fetch.sh ausführen und entpacken" >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/invoke-usbboot.XXXXXX")
# Nur die Images für den RAM-Boot kopieren; 80/83_IMAGE (NAND-Abbild) bewusst NICHT,
# damit ein versehentliches "l2nand 83" ins Leere läuft.
cp "$src"/{usb_boot,bcm_erom.bin.usb,bootloader.img,drm_erom.img,sysinit.img,0[6-9]_IMAGE,81_IMAGE,82_IMAGE} "$work"/

# Partitionstabelle des Invoke (aus cmdline.txt in bootimgs der Firmware 11.1842/12.2134),
# alles read-only, dazu der Rest (BBT-Bereich) und eine Gesamt-Partition über den ganzen Chip.
INVOKE_MTD="mtdparts=mv_nand:128K(block0)ro,1M(pre-bootloader)ro,2M(post-bootloader)ro,2M(postbootloaderB)ro,5M(factory_setting)ro,5M(tz_en)ro,1M(tz_en-B)ro,10M(bootimgs_B)ro,5M(bsl)ro,10M(bootimgs)ro,90M(rootfs)ro,123M(app)ro,1M(fw_stat)ro,896K(tail)ro,256M@0(nand)ro"

case $mode in
  uboot)   printf '#skip\n' > "$work/79_IMAGE" ;;
  backup)
    python3 "$here/tools/mkramdisk.py" "$src/82_IMAGE" "$work/82_IMAGE" >/dev/null
    size=$(stat -L -c %s "$work/82_IMAGE")
    cat > "$work/79_IMAGE" <<EOF
usbload 0x81 0x0c400000
usbload 0x82 0x08000000
set bootargs console=ttyS0,115200 debug init=/bin/sh root=/dev/ram $INVOKE_MTD initrd=0x08000000,$size
bootm 0x0c400000
EOF
    ;;
  ramdisk) cp "$src/79_IMAGE.ramdisk_boot" "$work/79_IMAGE" ;;
  *) echo "Modus: uboot | backup | ramdisk" >&2; exit 2 ;;
esac
if grep -qiE 'nand(erase|write|init)|2nand|flash|erase' "$work/79_IMAGE"; then
  echo "79_IMAGE enthält Schreibbefehle für den NAND – Abbruch." >&2; exit 3
fi
chmod +x "$work/usb_boot"
mkdir -p "$here/recon"
log=$here/recon/uboot-$(date +%Y%m%d-%H%M%S)-$mode.log
# Konsole mitschreiben, sobald usb_boot den Telnet-Port öffnet. Im uboot-Modus stattdessen
# interaktiv: script -c "telnet 127.0.0.1 8141" recon/uboot.log
[ "$mode" = uboot ] || ( for _ in $(seq 600); do
    socat -u TCP:127.0.0.1:8141 "OPEN:$log,creat,append" 2>/dev/null && break
    sleep 1
  done ) &
echo "Arbeitsverzeichnis: $work"
echo "Konsolen-Log:       $log"
echo "Jetzt Flashing-Modus auslösen (Reset halten, Strom an, 4x Mic-aus) ..."
cd "$work"
exec ./usb_boot 1286 8174 ./ 8141 "${TERMCMD:-true}"
