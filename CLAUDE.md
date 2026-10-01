# Leuchtfeuer (Invoke Hack) – Projektziel

## Ziel

Leuchtfeuer ist eine **gemeinsame, geräteunabhängige Plattform**, die jeden kleinen Rechner mit angeschlossenem
Lautsprecher zum Smartspeaker macht.

- Die gemeinsame Technik (Dienste, leuchtfeuerd, Weboberfläche, Tonkette, Hook) bleibt **geräteunabhängig**.
- Gerätespezifisches kommt als **Erweiterung** oder als **eigene Installationsroutine** je Zielgerät dazu
  (`targets/<gerät>/`, z. B. `targets/invoke`, `targets/generic`), nicht in den gemeinsamen Kern.
- Kernaufgaben auf jedem Gerät:
  - dauerhafter Zugang über das Netz (SSH mit eigenem Schlüssel)
  - Audio-Empfang: UPnP / DLNA (Renderer), Spotify Connect, Tidal Connect, Sendspin und weitere
  - stabiles Bluetooth
- **Nicht** Cortana wiederherstellen.

## Zielgerät Harman Kardon Invoke

Marvell BG2CD, Google-Cast-Linux, am Service-USB dieses Rechners.

- Vor jeder Änderung am NAND zuerst eine vollständige Rohsicherung aller mtd-Partitionen.
- Möglichst ohne Flashen auskommen (Zugang über WLAN, beschreibbares `/data`).
