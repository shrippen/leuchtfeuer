#!/usr/bin/env python3
"""Markiert die Versionsanforderungen eines ELF-Programms an bestimmte Bibliotheken als schwach (VER_FLG_WEAK).

  tools/elf-weak-verneed.py <datei> <bibliothek-präfix>...     z. B. ... tidal_connect_application libssl libcrypto

Gebraucht von tools/build-tidal-bundle.sh: tidal_connect_application verlangt OPENSSL_1.0.0/1.0.1 von libssl/libcrypto;
das gepflegte OpenSSL 1.0.2u definiert nur OPENSSL_1.0.2. patchelf --clear-symbol-version löst die Bindung der
Symbole, ld.so prüft aber zusätzlich jede Anforderung in .gnu.version_r. Eine schwache Anforderung, die fehlt, ist kein
Fehler (glibc _dl_check_map_versions). Nur 32- und 64-Bit Little-Endian. Gibt die geänderten Einträge aus."""
import struct
import sys

VER_FLG_WEAK = 0x2


def main(path, prefixes):
    data = bytearray(open(path, "rb").read())
    if data[:4] != b"\x7fELF" or data[5] != 1:
        sys.exit("kein Little-Endian-ELF")
    is64 = data[4] == 2
    if is64:
        shoff, = struct.unpack_from("<Q", data, 0x28)
        shentsize, shnum, shstrndx = struct.unpack_from("<HHH", data, 0x3A)
    else:
        shoff, = struct.unpack_from("<I", data, 0x20)
        shentsize, shnum, shstrndx = struct.unpack_from("<HHH", data, 0x2E)

    def section(i):
        o = shoff + i * shentsize
        if is64:
            name, typ, _, _, off, size, link = struct.unpack_from("<IIQQQQI", data, o)
        else:
            name, typ, _, _, off, size, link = struct.unpack_from("<IIIIIII", data, o)
        return name, typ, off, size, link

    def cstr(off):
        return bytes(data[off:data.index(b"\0", off)]).decode()

    SHT_GNU_verneed = 0x6FFFFFFE
    changed = []
    for i in range(shnum):
        _, typ, off, _, link = section(i)
        if typ != SHT_GNU_verneed:
            continue
        strtab = section(link)[2]
        vn = off
        while True:
            _, cnt, file_, aux, nxt = struct.unpack_from("<HHIII", data, vn)
            lib = cstr(strtab + file_)
            a = vn + aux
            for _ in range(cnt):
                _, flags, _, name, anext = struct.unpack_from("<IHHII", data, a)
                if any(lib.startswith(p) for p in prefixes) and not flags & VER_FLG_WEAK:
                    struct.pack_into("<H", data, a + 4, flags | VER_FLG_WEAK)
                    changed.append(f"{lib} {cstr(strtab + name)}")
                a += anext
            if not nxt:
                break
            vn += nxt
    open(path, "wb").write(data)
    for c in changed:
        print("schwach:", c)


if __name__ == "__main__":
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    main(sys.argv[1], sys.argv[2:])
