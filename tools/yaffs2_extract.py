#!/usr/bin/env python3
"""Liest ein yaffs2-Dateisystem aus einem Roh-Abbild (Seite 2048 B + OOB 64 B) des Invoke.

OOB-Belegung (am Gerät beobachtet): Bytes 0..15 = yaffs packed tags2 ohne ECC
(seq, obj_id, chunk_id, n_bytes, je u32 LE), ab Byte 30 Hardware-ECC des Marvell-NFC.

  yaffs2_extract.py <raw_oob.bin> <start_byte> <länge_byte> [--list] [--out DIR]

start/länge beziehen sich auf die Datenadressen (ohne OOB), z. B. app: 0x8320000 0x7b00000.
"""
import argparse
import os
import struct
import sys

PAGE, OOB = 2048, 64
PPB = 64  # Seiten je Block
TYPES = {0: "unknown", 1: "file", 2: "symlink", 3: "dir", 4: "hardlink", 5: "special"}


def scan(raw, start, length):
    """Liefert je Objekt den neuesten Kopf und je (obj, chunk) die neueste Datenseite."""
    heads, chunks = {}, {}
    first = start // PAGE
    for blk in range(length // PAGE // PPB):
        for p in range(PPB):
            i = first + blk * PPB + p
            rec = raw[i * (PAGE + OOB):(i + 1) * (PAGE + OOB)]
            oob = rec[PAGE:]