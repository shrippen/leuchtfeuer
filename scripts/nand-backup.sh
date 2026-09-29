#!/usr/bin/env bash
# Vollständige Rohsicherung aller mtd-Partitionen des Invoke über ADB.
# Voraussetzung: Gerät läuft mit der Sicherungs-Ramdisk (tools/usb-ramboot.sh backup),
# dort ist nichts vom NAND eingehängt und alle Partitionen sind read-only.
#
# Ergebnis in backup/<zeit>/:
#   <nr>_<name>.bin       ECC-korrigierte Daten je Partition (nanddump ohne OOB)
#   nand_raw_oob.bin      ganzer Chip mit OOB-Bereichen (nanddump -o, Seite+OOB)
#   SHA256SUMS            Prüfsummen (auf dem Gerät und hier geprüft)
#   proc_mtd.txt, cmdline.txt, dmesg.txt, ...
# Jede Partition wird zweimal gelesen; weichen die Prüfsummen ab, bricht das Skript ab.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=$here/backup/$(date +%Y%m%d-%H%M%S)
CHUNK=$((16 * 1024 * 1024))
BB=/bin/busybox
mkdir -p "$out"
log(){ echo "[$(date +%T)] $*" | tee -a "$out/backup.log"; }
sh_(){ adb shell "$*" | tr -d '\r'; }

log "Warte auf ADB ..."
adb wait-for-device
[ "$(sh_ "cat /tmp/INVOKE_BACKUP_RAMDISK 2>/dev/null")" = ok ] || {
  log "FEHLER: Gerät läuft nicht mit der Sicherungs-Ramdisk – Abbruch."; exit 1; }
if sh_ "cat /proc/mounts" | grep -qE 'mtdblock|yaffs|ubifs|jffs2'; then
  log "FEHLER: NAND-Dateisystem eingehängt – Abbruch."; exit 1
fi

for f in /proc/mtd /proc/cmdline /proc/cpuinfo /proc/meminfo /proc/mounts /proc/version; do
  sh_ "cat $f" > "$out/$(echo "${f#/}" | tr / _).txt"
done
sh_ dmesg > "$out/dmesg.txt"
sh_ "ls -l /dev/mtd* /dev/mtd/ 2>/dev/null" > "$out/dev_mtd.txt"
cat "$out/proc_mtd.txt" | tee -a "$out/backup.log"

dev(){ if sh_ "ls /dev/mtd/mtd$1" | grep -q "^/dev/mtd/mtd$1\$"; then echo /dev/mtd/mtd$1; else echo /dev/mtd$1; fi; }

# dump <mtdnr> <ziel> <größe> [oob]
dump(){
  local n=$1 dst=$2 size=$3 oob=${4:-} d off=0 i=0 part
  d=$(dev "$n"); : > "$dst"
  while [ "$off" -lt "$size" ]; do
    local len=$(( size - off < CHUNK ? size - off : CHUNK ))
    part=/tmp/chunk.bin
    sh_ "$BB nanddump ${oob:+-o} -s $off -l $len -f $part $d >/dev/null 2>/tmp/nd.err; cat /tmp/nd.err" >> "$out/nanddump.log"
    local h1 h2 hl
    h1=$(sh_ "$BB sha256sum $part" | awk '{print $1}')
    # zweiter Lesevorgang zur Kontrolle
    sh_ "$BB nanddump ${oob:+-o} -s $off -l $len -f /tmp/chunk2.bin $d >/dev/null 2>&1"
    h2=$(sh_ "$BB sha256sum /tmp/chunk2.bin; rm /tmp/chunk2.bin" | awk '{print $1}')
    [ "$h1" = "$h2" ] || { log "FEHLER: mtd$n @$off: zwei Lesevorgänge verschieden"; exit 1; }
    adb pull "$part" "$out/.chunk" >/dev/null 2>&1
    hl=$(sha256sum "$out/.chunk" | awk '{print $1}')
    [ "$h1" = "$hl" ] || { log "FEHLER: mtd$n @$off: Übertragung fehlerhaft"; exit 1; }
    cat "$out/.chunk" >> "$dst"; rm -f "$out/.chunk"
    sh_ "rm $part"
    off=$(( off + len )); i=$((i+1))
  done
}

# Partitionen aus /proc/mtd: "mtd3: 00200000 00020000 \"name\""
while read -r m size erase name; do
  n=${m#mtd}; n=${n%:}; name=${name//\"/}
  [ "$name" = nand ] && continue
  sz=$((16#$size))
  f=$(printf '%02d_%s.bin' "$n" "$name")
  log "mtd$n $name ($((sz/1024)) KiB) ..."
  dump "$n" "$out/$f" "$sz"
done < <(grep '^mtd' "$out/proc_mtd.txt")

nandnr=$(awk '/"nand"/{sub(/mtd/,"",$1); sub(/:/,"",$1); print $1}' "$out/proc_mtd.txt")
if [ -n "$nandnr" ]; then
  sz=$((16#$(awk '/"nand"/{print $2}' "$out/proc_mtd.txt")))
  log "Ganzer Chip roh mit OOB (mtd$nandnr, $((sz/1048576)) MiB) ..."
  dump "$nandnr" "$out/nand_raw_oob.bin" "$sz" oob
fi

( cd "$out" && sha256sum ./*.bin > SHA256SUMS )
python3 "$here/scripts/verify-backup.py" "$out" | tee -a "$out/backup.log"
log "Fertig: $out"
