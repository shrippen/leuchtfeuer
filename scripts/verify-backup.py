#!/usr/bin/env python3
"""Prüft eine Sicherung aus scripts/nand-backup.sh.

- SHA256SUMS stimmt mit den Dateien überein
- die Partitionsdateien ergeben aneinandergereiht den Datenteil des Roh-Abbilds
  (nand_raw_oob.bin ohne OOB), d. h. zwei unabhängige Lesewege liefern dasselbe
- Plausibilität: rootfs beginnt mit SquashFS, factory_setting ist nicht leer
"""
import hashlib
import pathlib
import re
import sys

d = pathlib.Path(sys.argv[1])
ok = True


def fail(msg):
    global ok
    ok = False
    print("FEHLER:", msg)


for line in (d / "SHA256SUMS").read_text().splitlines():
    h, name = line.split(maxsplit=1)
    name = name.lstrip("./")
    if hashlib.sha256((d / name).read_bytes()).hexdigest() != h:
        fail(f"Prüfsumme {name}")

parts = []
for line in (d / "proc_mtd.txt").read_text().splitlines():
    m = re.match(r'mtd(\d+): ([0-9a-f]+) ([0-9a-f]+) "(.*)"', line)
    if m and m.group(4) != "nand":
        parts.append((int(m.group(1)), int(m.group(2), 16), m.group(4)))

data = b"".join((d / f"{n:02d}_{name}.bin").read_bytes() for n, _, name in parts)
raw_p = d / "nand_raw_oob.bin"
if raw_p.exists():
    raw = raw_p.read_bytes()
    page = 2048
    pages = len(data) // page
    oob = len(raw) // pages - page if pages else 0
    print(f"Chip: {len(data)} Bytes Daten, OOB je Seite {oob} Bytes")
    stripped = b"".join(raw[i * (page + oob): i * (page + oob) + page] for i in range(pages))
    if stripped != data:
        diff = next(i for i in range(len(data)) if stripped[i] != data[i])
        fail(f"Roh-Abbild und Partitionen verschieden ab Offset {diff:#x}")
    else:
        print("Roh-Abbild (ohne OOB) == Partitionen: OK")
else:
    fail("nand_raw_oob.bin fehlt")

for n, size, name in parts:
    b = (d / f"{n:02d}_{name}.bin").read_bytes()
    if len(b) != size:
        fail(f"{name}: Größe {len(b)} statt {size}")
    if name == "rootfs" and b[:4] != b"hsqs":
        fail("rootfs beginnt nicht mit SquashFS – Partitionstabelle/ECC prüfen")
    if name == "factory_setting" and set(b) == {0xFF}:
        fail("factory_setting ist leer (nur 0xFF)")

print("Sicherung OK" if ok else "Sicherung FEHLERHAFT")
sys.exit(0 if ok else 1)
