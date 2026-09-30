#!/usr/bin/env bash
# Prüft den NAND nach "l2nand 83" gegen Sicherung und Abbild (Gerät in der Sicherungs-Ramdisk,
# tools/uboot-send.sh ramdisk). Liest nur (nandread, alle Partitionen ro).
#   scripts/verify-after-flash.sh <sicherungsordner> [83_IMAGE]
# Erwartung:
#   - factory_setting, postbootloaderB, tz_en-B, fw_stat: Daten == Sicherung
#   - factory_setting und tail (BBT): Daten+OOB == Sicherung (nand_raw_oob.bin)
#   - block0, pre-/post-bootloader, tz_en, bootimgs(_B), bsl, rootfs: Anfang == Abbild
# Gibt nur Prüfsummen/Ergebnisse aus, nie Inhalte (factory_setting enthält Geräteschlüssel).
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
bk=${1:?Sicherungsordner}
img=${2:-$here/firmware/83_IMAGE}
tmp=$(mktemp -d "${TMPDIR:-/tmp}/invoke-verify.XXXXXX")
chmod 700 "$tmp"
trap 'rm -rf "$tmp"' EXIT
sh_(){ adb -s invokebackup shell "$*" </dev/null | tr -d '\r' | sed '1{/^\/$/d}'; }
adb -s invokebackup wait-for-device
[ "$(sh_ "cat /tmp/INVOKE_BACKUP_RAMDISK 2>/dev/null")" = ok ] || { echo "Nicht in der Sicherungs-Ramdisk"; exit 1; }
if sh_ "cat /proc/mounts" | grep -qE 'mtdblock|yaffs'; then echo "NAND eingehängt – Abbruch"; exit 1; fi
sh_ "cat /proc/mtd" > "$tmp/proc_mtd.txt"
diff <(grep -v '"nand"\|"mv_nand"' "$tmp/proc_mtd.txt") <(grep -v '"nand"\|"mv_nand"' "$bk/proc_mtd.txt") >/dev/null \
  || { echo "Partitionstabelle anders als bei der Sicherung – Abbruch"; exit 1; }

# read <mtd> <datei> <oob 0|64> [offset len]
rd(){
  local off=${4:-} len=${5:-} extra=""
  [ -n "$off" ] && extra="-S $off -L $len"
  sh_ "nandread -d /dev/mtd/mtd$1 -f /tmp/v.bin -s $3 $extra >/dev/null 2>&1; /bin/busybox sha256sum /tmp/v.bin" | awk '{print $1}' > "$2.devsha"
  adb -s invokebackup pull /tmp/v.bin "$2" >/dev/null 2>&1 </dev/null
  sh_ "rm /tmp/v.bin"
  [ "$(sha256sum "$2" | cut -d' ' -f1)" = "$(cat "$2.devsha")" ] || { echo "Übertragung mtd$1 fehlerhaft"; exit 1; }
}
for n in 0 1 2 3 4 5 6 7 8 9 10 12; do rd $n "$tmp/mtd$n.bin" 0; done
rd 4 "$tmp/mtd4.oob" 64
rd 14 "$tmp/tail.oob" 64 $((0x0ff20000)) $((0x000e0000))
python3 - "$bk" "$img" "$tmp" <<'EOF'
import hashlib, struct, sys
bk, img, t = sys.argv[1:]
h = lambda b: hashlib.sha256(b).hexdigest()[:16]
ok = True
def res(name, good, note=""):
    global ok; ok &= good
    print(f"  {'OK    ' if good else 'FEHLER'} {name} {note}")
names = {0: "block0", 1: "pre-bootloader", 2: "post-bootloader", 3: "postbootloaderB",
         4: "factory_setting", 5: "tz_en", 6: "tz_en-B", 7: "bootimgs_B", 8: "bsl",
         9: "bootimgs", 10: "rootfs", 12: "fw_stat"}
print("Unverändert gegenüber Sicherung:")
for n in (4, 3, 6, 12):
    a = open(f"{t}/mtd{n}.bin", "rb").read(); b = open(f"{bk}/{n:02d}_{names[n]}.bin", "rb").read()
    res(f"mtd{n} {names[n]} (Daten)", a == b, f"sha256 {h(a)} / Sicherung {h(b)}")
raw = open(f"{bk}/nand_raw_oob.bin", "rb").read()
P = 2112
for fn, name, start, size in (("mtd4.oob", "factory_setting", 0x520000, 0x500000),
                              ("tail.oob", "tail/BBT", 0xff20000, 0xe0000)):
    a = open(f"{t}/{fn}", "rb").read(); b = raw[start // 2048 * P:(start + size) // 2048 * P]
    res(f"{name} (Daten+OOB)", a == b, f"sha256 {h(a)} / Sicherung {h(b)}")
print("Geschrieben aus dem Abbild:")
d = open(img, "rb").read(); off = 0x280
num = {v: k for k, v in names.items()}
for i in range(struct.unpack("<I", d[28:32])[0]):
    e = d[64 + 64 * i:128 + 64 * i]; name = e[:16].rstrip(b"\0").decode()
    size = struct.unpack("<Q", e[16:24])[0]; data = d[off:off + size]; off += size
    if name == "app":
        print("  (app: yaffs2-Abbild mit eigenem Format, nicht direkt vergleichbar)"); continue
    a = open(f"{t}/mtd{num[name]}.bin", "rb").read()
    rest = set(a[size:]) <= {0xFF}
    res(f"mtd{num[name]} {name}", a[:size] == data, "" if rest else "(Rest nicht leer)")
sys.exit(0 if ok else 1)
EOF
