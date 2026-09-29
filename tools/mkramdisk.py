#!/usr/bin/env python3
"""Baut aus der Marvell-SDK-Ramdisk (82_IMAGE, cpio newc + gzip) eine Sicherungs-Ramdisk.

Die Original-Ramdisk hängt in /etc/init.d/rcS Partitionen per Name ein
(factory_setting, app, localstorage – letztere sogar rw) und startet /home/galois/run.sh.
Für die Rohsicherung soll aber NICHTS vom NAND eingehängt werden. Dieses Skript ersetzt
nur rcS (Gerätedateien, Rechte und Besitzer der übrigen Einträge bleiben unverändert,
weil der cpio-Strom direkt umgeschrieben wird – kein Entpacken als root nötig).

  tools/mkramdisk.py <82_IMAGE> <ausgabe.cpio.gz>
"""
import gzip
import sys

RCS = b"""#!/bin/sh
# Invoke-Hack: Sicherungs-Ramdisk. Haengt KEINE NAND-Partition ein.
mount -t sysfs sys /sys
mount -t proc proc /proc
mount -t tmpfs devtmpfs /dev
mkdir /dev/pts
mount -t devpts devpts /dev/pts
sync
/bin/mount -o remount,ro /
( umask 0; /sbin/ueventd -s )
mount -t tmpfs tmpfs /etc/tmpfs
/bin/touch /etc/tmpfs/resolv.conf
/bin/touch /etc/tmpfs/hosts
mount -t tmpfs tmpfs /tmp
ifconfig lo up
# ADB + ACM ueber den Service-USB
echo 0d02 > /sys/class/android_usb/android0/idProduct
echo "INVOKE BACKUP" > /sys/class/android_usb/android0/iProduct
echo acm,adb > /sys/class/android_usb/android0/functions
echo 1 > /sys/class/android_usb/android0/f_acm/instances
echo 1 > /sys/class/android_usb/android0/enable
echo invokebackup > /sys/devices/virtual/android_usb/android0/iSerial
echo ok > /tmp/INVOKE_BACKUP_RAMDISK
"""

TARGET = "etc/init.d/rcS"


def records(data):
    off = 0
    while off < len(data):
        hdr = data[off:off + 110]
        if hdr[:6] not in (b"070701", b"070702"):
            raise SystemExit(f"kein newc-Header bei {off:#x}")
        f = [int(hdr[6 + 8 * i:14 + 8 * i], 16) for i in range(13)]
        namesize, filesize = f[11], f[6]
        name_end = off + 110 + namesize
        name = data[off + 110:name_end - 1].decode()
        data_off = (name_end + 3) & ~3
        data_end = data_off + filesize
        nxt = (data_end + 3) & ~3
        yield hdr, name, data[data_off:data_end], f
        off = nxt
        if name == "TRAILER!!!":
            return


def pack(hdr, name, body, fields):
    fields = list(fields)
    fields[6] = len(body)
    nb = name.encode() + b"\0"
    fields[11] = len(nb)
    out = hdr[:6] + b"".join(b"%08X" % v for v in fields) + nb
    out += b"\0" * (-len(out) % 4)
    out += body + b"\0" * (-len(body) % 4)
    return out


def main():
    src, dst = sys.argv[1:3]
    data = gzip.decompress(open(src, "rb").read())
    out, hit = [], False
    for hdr, name, body, f in records(data):
        n = name.lstrip("./")
        if n == TARGET:
            body, hit = RCS, True
        out.append(pack(hdr, name, body, f))
    if not hit:
        raise SystemExit(f"{TARGET} nicht gefunden")
    blob = b"".join(out)
    blob += b"\0" * (-len(blob) % 512)
    with open(dst, "wb") as fh:
        fh.write(gzip.compress(blob, 9, mtime=0))
    print(dst)


if __name__ == "__main__":
    main()
