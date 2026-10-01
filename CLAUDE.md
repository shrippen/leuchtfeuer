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

## GUI rule

- Every GUI of this project is generated from Kante, not inspired by it: landing pages,
  web apps, Qt Quick / Kirigami apps, Plasma widgets, dialogs, e-mail and print layouts.
  Source: https://github.com/shrippen/Kante (checkout `../Kante`).
  Web: link `https://shrippen.github.io/v1/shrippen.css` and `shrippen.js`, or vendor them
  unchanged. Apps: copy `qml/Kante` (and `KantePlasma` for Plasma widgets) unchanged.
- Use Kante's tokens, roles, components, classes, QML components and motion as they are.
  No own colours, fonts, sizes, radii, cuts, shadows, animation timings, no own copy or
  variant of a component that Kante has. Raw values (`#hex`, `px` for controls) are a bug;
  use roles (`--primary`, `--focus`, `--warn`, `KanteStyle.*`).
- A missing element is added to Kante first (CSS or QML, docs, catalogue), then used here.
  Never solve it locally in this project and never wait with a "temporary" copy.
- Exception: Kimai plugins take their GUI from Knust (`kimai/knust/` in Kante) and
  the kit (`kimai/kit/`), the Kante spinoff that adapts Kante to Kimai's look. The same rule applies to Knust: use it
  as it is, and add missing elements to Knust.
- Exception: Kintsugi (`shrippen/kintsugi`) uses Kante Gold (`<html data-kante="gold">`),
  the noble variant defined in Kante itself. The same rule applies: use it as it is, and
  add a missing element or Gold detail to Kante (its Gold block) first. No other project
  uses Kante Gold without a decision recorded here.
- A project without a GUI (library, CLI, scripts) has nothing to do here.
- Here: the leuchtfeuerd web UI vendors `src/leuchtfeuerd/web/kante/` (`tools/sync-kante.sh`);
  never edit it. The landing page links `https://shrippen.github.io/v1/`.
- Rule text: https://github.com/shrippen/Kante/blob/main/AGENT-RULE.md
