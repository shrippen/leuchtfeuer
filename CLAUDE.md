# Invoke Hack – Projektziel

Harman Kardon Invoke (Marvell BG2CD, Google-Cast-Linux), am Service-USB dieses Rechners.

## Ziel

- **Nicht** Cortana wiederherstellen.
- Dauerhafter Zugang zum Linux des Lautsprechers **über WLAN** (z. B. SSH mit eigenem Schlüssel).
- Auf dem Lautsprecher laufen Programme, die Audio empfangen:
  - UPnP / DLNA (Renderer)
  - Spotify Connect
  - Tidal Connect
  - Sendspin
- Bluetooth soll stabil laufen (bestehende Fehler beheben).

## Vorgehen

- Vor jeder Änderung am NAND zuerst eine vollständige Rohsicherung aller mtd-Partitionen.
- Möglichst ohne Flashen auskommen (Zugang über WLAN, beschreibbares `/data`).
