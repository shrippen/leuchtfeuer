#!/usr/bin/env bash
# Flashing-Modus für "l2nand -m 83" (StockRoot-Abbild) vorbereiten. NICHT "l2nand 83" ohne -m:
# das löscht den ganzen NAND inkl. factory_setting (gerätespezifische Zertifikate, MAC) – nur -m schont sie.
# Das Skript selbst schreibt NICHTS auf den NAND: es lädt U-Boot per USB in den RAM und hält
# an der U-Boot-Konsole. Befehle schickt man mit tools/uboot-send.sh "<befehl>".
#
#   tools/usb-flash.sh            -> startet usb_boot, Konsole -> recon/flash-<zeit>.log
#   tools/uboot-send.sh help      -> Befehl an U-Boot
#   tools/uboot-send.sh "l2nand -m 83"            -> flasht firmware/83_IMAGE (StockRoot), ohne alles zu löschen
#   tools/uboot-send.sh ramdisk   -> danach (ohne Neustart) Sicherungs-Ramdisk booten,
#                                    dann scripts/verify-after-flash.sh
#
# Im Arbeitsverzeichnis liegen: 83_IMAGE (StockRoot, Prüfsumme geprüft), 81_IMAGE (SDK-Kernel)
# und 82_IMAGE (Sicherungs-Ramdisk aus tools/mkramdisk.py, hängt nichts ein).
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
src=$here/firmware/extracted/flashing/marvell_flash_tool
img=$here/firmware/83_IMAGE
SHA_STOCKROOT=f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7
[ "$(sha256sum "$img" | cut -d' ' -f1)" = "$SHA_STOCKROOT" ] || { echo "83_IMAGE ist nicht StockRoot 11.1842 – Abbruch" >&2; exit 1; }
grep -q Barracuda_rooted_libre-11.1842.0 <(python3 "$here/tools/check83.py" "$img") || { echo "Kopf/CRC-Prüfung fehlgeschlagen" >&2; exit 1; }

work=$(mktemp -d "${TMPDIR:-/tmp}/invoke-flash.XXXXXX")
cp "$src"/{usb_boot,bcm_erom.bin.usb,bootloader.img,drm_erom.img,sysinit.img,0[6-9]_IMAGE,81_IMAGE} "$work"/
cp "$img" "$work/83_IMAGE"
python3 "$here/tools/mkramdisk.py" "$src/82_IMAGE" "$work/82_IMAGE" >/dev/null
printf '#skip //an der U-Boot-Konsole anhalten\n' > "$work/79_IMAGE"
stat -L -c %s "$work/82_IMAGE" > "$work/.ramdisk_size"
chmod +x "$work/usb_boot"

mkdir -p "$here/recon"
log=$here/recon/flash-$(date +%Y%m%d-%H%M%S).log
fifo=$work/console.in
mkfifo "$fifo"
echo "$log" > "$work/.log"
ln -sfn "$work" "${TMPDIR:-/tmp}/invoke-flash.current"
# FIFO offen halten, damit socat beim Schreiben einzelner Befehle kein EOF sieht
sleep infinity > "$fifo" &
keeper=$!
trap 'kill $keeper 2>/dev/null || true' EXIT
( for _ in $(seq 900); do
    socat "PIPE:$fifo!!OPEN:$log,creat,append" TCP:127.0.0.1:8141 2>/dev/null && break
    sleep 1
  done ) &
echo "Arbeitsverzeichnis: $work"
echo "Konsolen-Log:       $log"
echo "Jetzt Flashing-Modus auslösen (Reset halten, Strom an, 4x Mic-aus) ..."
cd "$work"
./usb_boot 1286 8174 ./ 8141 "${TERMCMD:-true}" > "$here/recon/usb_boot-flash.log" 2>&1
