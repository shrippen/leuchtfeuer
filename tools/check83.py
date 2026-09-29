#!/usr/bin/env python3
"""Kopf eines Marvell-NAND-Abbilds (83_IMAGE) lesen und jede Partition per CRC32 prüfen.

Kopf: Magic f1a3add2, Seite, Seiten/Block, Anzahl Einträge; ab 0x40 Einträge à 64 Byte
(Name 16, Größe u64, CRC32, Version, Zeitstempel, 0, Startblock, Blockzahl, Flags).
Die Daten folgen ab 0x280 in der Reihenfolge der Einträge. Gibt außerdem die Version aus
der rootfs-SquashFS-Datei /etc/version.txt aus (falls 7z vorhanden) und bricht ab, wenn
ein Eintrag factory_setting (Blöcke 41..80) oder den BBT-Bereich (ab Block 2033) berührt.
"""
import struct
import subprocess
import sys
import tempfile
import zlib

PROTECTED = [("factory_setting", 41, 81), ("fw_stat+tail/BBT", 2033, 2048)]

fn = sys.argv[1]
d = open(fn, "rb").read()
magic, ver, _, page, _, ppb, _, n = struct.unpack("<8I", d[:32])
assert magic == 0xD2ADA3F1, "falsches Magic"
print(f"{fn}: Seite {page}, {ppb} Seiten/Block, {n} Einträge")
off, ok, rootfs = 0x280, True, None
for i in range(n):
    e = d[64 + 64 * i:128 + 64 * i]
    name = e[:16].rstrip(b"\0").decode()
    size, crc, _v, _t, _z, start, count, flags = struct.unpack("<QIIIIIII", e[16:52])
    data = d[off:off + size]
    good = len(data) == size and zlib.crc32(data) == crc
    ok &= good
    print(f"  {name:16} Blöcke {start:5}..{start + count - 1:5} ({count * 128:6} KiB) "
          f"Daten {size:9} B  CRC {'ok' if good else 'FALSCH'}  flags={flags}")
    for pname, a, b in PROTECTED:
        if start < b and start + count > a:
            print(f"  FEHLER: {name} überschneidet {pname}")
            ok = False
    if name == "rootfs":
        rootfs = data
    off += size
print(f"  Ende der Partitionsdaten {off:#x}, danach {len(d) - off} B (nicht im Kopf)")
if rootfs:
    with tempfile.TemporaryDirectory() as t:
        open(f"{t}/r.sqfs", "wb").write(rootfs)
        r = subprocess.run(["7z", "e", "-so", f"{t}/r.sqfs", "etc/version.txt"],
                           capture_output=True)
        if r.returncode == 0:
            print("  Version:", r.stdout.decode().strip())
if not ok:
    sys.exit(1)
