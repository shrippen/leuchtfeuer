# Leuchtfeuer (Invoke Hack) – Projektziel

The rules for all shrippen repositories are in `RULES.md` (writing, code, commits, bug
fixes, repository, GUI). It is a synced copy from the rules repository; do not edit it
here. This file holds only what is particular to this repository. A Claude Code session on
the owner's machine has the rules through the global `CLAUDE.md` already and reads
`RULES.md` only for its GUI rule, before work on a user interface.

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

## Grundsatz: platzsparend und modular

- Zielplattform sind Smartspeaker, und die haben wenig Speicherplatz (Invoke: 123 MB `/data`). Alles muss möglichst
  **wenig Platz** brauchen.
- Vieles ist **optional bzw. modular**: Empfänger, Bündel (z. B. Tidal Connect) und Zusätze kommen nur aufs Gerät, wenn
  sie gewollt sind, und lassen sich nachrüsten und wieder entfernen. Bibliotheken des Systems mitnutzen statt sie doppelt
  mitzubringen; ein eigenes Bündel nur, wenn es nicht anders geht, und dann als getrenntes Modul.
- Die **Weboberfläche** (leuchtfeuerd) ist immer dabei.

## Grundsatz: aktuelle Software, hoher Sicherheitsstand

- Die Geräte hängen dauerhaft im Netz und dürfen **nicht in einem Botnetz enden**. Deshalb möglichst **aktuelle,
  gepflegte Software** verwenden, vor allem für TLS, HTTP, Netzwerk und Medien-Parser – auch wenn eine ältere Version
  kleiner wäre. Platzsparen darf nie auf Kosten bekannter Sicherheitslücken gehen.
- Was das heißt, ist je Zielgerät verschieden (Invoke: alter Kernel und alte glibc, nicht ersetzbar; Raspberry Pi:
  Updates über die Distribution). Bibliotheken des Systems mitnutzen, wo sie aktuell sind.
- Wo eine alte Version unvermeidbar ist (proprietäres Programm mit alter ABI), die neueste Fassung dieser Linie nehmen,
  die Angriffsfläche klein halten (optional, standardmäßig aus, Firewall, nur LAN) und das Risiko in der Doku nennen.

## Zielgerät Harman Kardon Invoke

Marvell BG2CD, Google-Cast-Linux, am Service-USB dieses Rechners.

- Vor jeder Änderung am NAND zuerst eine vollständige Rohsicherung aller mtd-Partitionen.
- Möglichst ohne Flashen auskommen (Zugang über WLAN, beschreibbares `/data`).

## GUI (this project)

- Here: the leuchtfeuerd web UI vendors `src/leuchtfeuerd/web/kante/` (`tools/sync-kante.sh`);
  never edit it. The landing page links `https://shrippen.github.io/v1/`.

## Release

Release steps: `RELEASE.md`. Before a release, load the skill `release-changelog`. A release
is published, so the owner confirms it in words.

## Repository (this project)

- This repository keeps its rules in `CLAUDE.md`; where `RULES.md` says `agent.md`, read `CLAUDE.md`.
- CI runs on the GitHub mirror (`.github/workflows/ci.yml`: `tests/run.sh`) and reports back to Gitea as the commit status `github / test`.
- The merge waits for: CI is green on the PR's latest commit: `github / test` from the GitHub mirror shows success there. No status there yet means wait, not merge; if it never comes, check the mirror or ask the owner. PR-Agent's status does not count here.
- Everything else: `RULES.md` (Repository) and the skill `pr-review`.
