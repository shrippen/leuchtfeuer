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

## Release rule

Release steps, in this order (`X.Y.Z` the new version, `A.B.C` the previous one). A release is done only after step 10.

1. On a branch from `main`, bump the version of the Home Assistant integration in `custom_components/leuchtfeuer/manifest.json` (`version`; HACS shows it). The programs and packages take theirs from the tag (`git describe` in `tools/build-*.sh` and `tools/make-release.sh`).
2. Write the detailed release log before the tag in `CHANGELOG.md`, the single source: a section `## [X.Y.Z] — YYYY-MM-DD` (newest first, `## [Unreleased]` on top if used) with `### English` and `### Deutsch`, each with a lead (what the release is about, one or two sentences), then the changes grouped as New, Improved, Fixed and Note (Neu, Verbessert, Behoben, Hinweis; breaking changes, steps the user must take). Empty groups are left out. Each point says what changed for the user and why, not which code moved. The English part ends with `All changes: https://github.com/shrippen/leuchtfeuer/compare/vA.B.C...vX.Y.Z`. Check the headings with `scripts/release-notes.sh --check` (CI runs it on every push) and read the notes with `scripts/release-notes.sh X.Y.Z`.
3. Landing page `docs/index.html` (Kante `.changelog`): the same wording in both languages as a new entry at the top of `#changes`, with the compare link; from the fourth entry on, the oldest move into `details.changelog-more`. Bump the footer version (`vX.Y.Z`, linked to `#changes`). Graphics wherever possible: a screenshot from the demo world per visible change (`docs/shots/release-<version>-<name>-<lang>.webp`; shots in `demo/shots.json`, taken with `../shrippen.github.io/demo/tools/.venv/bin/python ../shrippen.github.io/demo/tools/screenshots.py leuchtfeuer`), a diagram (Kante `.flow` or inline SVG) for changes inside. Screenshots are taken now, for the tagged release, not with every change.
4. Open the PR on Gitea, review it as the Repository rule says (a release is published: the owner confirms it in words), merge on Gitea.
5. Tag the merge commit and push the tag to Gitea only: `git tag vX.Y.Z <merge commit> && git push origin vX.Y.Z` (the mirror carries it to GitHub).
6. Gitea release, notes only, never files: `scripts/release-notes.sh X.Y.Z > notes.md`, then `tea release create --repo shrippen/leuchtfeuer --tag vX.Y.Z --title "Leuchtfeuer vX.Y.Z" --note-file notes.md`.
7. GitHub: the tag starts `.github/workflows/release.yml`. It takes the notes from the section (fails without it, before building) and adds the line about the packages, runs `tests/run.sh`, builds, packs and signs `invoke` and `generic-amd64/-arm64/-armv7` (secret `LEUCHTFEUER_SIGNING_KEY`) and creates the release "Leuchtfeuer vX.Y.Z" with the packages, `.sha256` and `.sig`. Check the run (`gh run list -R shrippen/leuchtfeuer --workflow release.yml`, up to three hours) and the release (`gh release view vX.Y.Z -R shrippen/leuchtfeuer`: notes and all files).
8. Update check: `python3 ../shrippen.github.io/overview/tools/build-versions.py`, then commit `docs/versions.json` in `shrippen.github.io` via a PR there.
9. Update the own speaker from the GitHub package (web interface: Settings > Backup & update, or `install.sh --prebuilt`), then `scripts/smoke.sh --ip IP`. HACS offers the integration update from the GitHub release by itself.
10. Done when https://shrippen.github.io/leuchtfeuer/ shows X.Y.Z at the top of `#changes` and in the footer, and https://shrippen.github.io/versions.json lists `leuchtfeuer` with X.Y.Z.

`scripts/release-notes.sh` is a copy of Kante's `tools/release-notes.sh`: change it only there, then sync with `../Kante/tools/sync-release-notes.sh "$PWD"`.

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

## Repository rule

- This repository lives on Gitea (`git.arianw.de`). GitHub is only a push mirror of it.
- CI runs on the GitHub mirror (`.github/workflows/ci.yml`: `tests/run.sh`) and reports back to Gitea as the commit status `github / test`.
- Changes arrive as pull requests: work on a branch, open a PR, merge it on Gitea (the mirror follows).
- Never merge a PR, push to `main` (or any default branch), push tags or publish releases on GitHub. A merge there is overwritten by the next Gitea push.
- Never force-push a branch that someone else's PR depends on.
- Review (all shrippen repositories, owner 18.10. and 22.10.2026): every PR is reviewed before it is merged. The owner does not read code, so this is the second reader.
  - PR-Agent first (owner, 22.10.2026): where `.gitea/workflows/pr-agent.yml` is set up, the session comments `/review` on the PR for each round with new code or a new base (the first one included; a rebase, a merge of the base or a change of the base branch, see below) and waits for PR-Agent's comment. That comment reviews the whole diff and covers the commit that was the PR's head when `/review` was posted. PR-Agent labels its findings as `REVIEW.md` says; a finding without a label counts as **Wichtig**. A run cancelled by a newer `/review` on the same PR (the concurrency group holds the PR number) is replaced, not failed.
  - If PR-Agent fails (the run fails or ends without a review comment, reports a key or cost limit, or no run is created within 10 minutes), the session posts `/review` once more. If that fails too, a new Claude agent reviews the round as below, and the session says in the PR why. Where PR-Agent is not set up, the new Claude agent always reviews.
  - The Claude agent: the session that wrote the PR starts a new agent (Agent tool, no shared context) and gives it only the PR's title and body, the diff against its base (`git diff <base>...HEAD`), the repository and `REVIEW.md`. It does not tell it what it thinks of its own change. The agent checks every finding it would post against the code (file, line, the input that goes wrong) and drops what it cannot show. It reports as `REVIEW.md` says: count first, labels **Wichtig**, **Kleinigkeit**, **Bestand**. The session posts the report unchanged as a PR comment on Gitea. Above it goes the heading `## Claude-Review (neuer Agent) zu <commit>`, below it the footer `---` and `_Generated by [Claude Code](https://claude.ai/code)_`.
  - Each **Wichtig** is fixed, or answered in the PR with a reason.
  - PR-Agent sees no earlier round. So when an earlier round had a **Wichtig** (open or closed), after PR-Agent's comment (or with answers only, alone) a new Claude agent closes the round: it gets the PR's title and body, the repository, `REVIEW.md`, the open **Wichtig** with the answers, PR-Agent's findings of this round, the closed **Wichtig** and how they were closed, and the new commits (`git diff <last reviewed commit>..HEAD`; without new commits, the code at HEAD). It says for each open **Wichtig** whether it is fixed or its answer is accepted, and whether a **Wichtig** PR-Agent raises again is one already closed (fixed, answer accepted or decided by the owner; it stays closed unless the agent shows it is not fixed). Its count holds every **Wichtig** still open, PR-Agent's new ones included. It posts like a review, heading `## Claude-Review (offene Wichtig) zu <commit>`. An answer it does not accept goes to the owner, who decides in words. With the fallback, one new agent does both: the review of the new commits and the open **Wichtig**.
  - After a rebase, a merge of the base or a change of the PR's base branch a new round is due, also without a new commit, against the new base; a Claude agent of that round (closing or fallback) gets the whole diff against the new base (`git diff <base>...HEAD`) in place of the new commits. Every round counts, also one with answers only: after three rounds that still leave a **Wichtig** open, stop and rethink the approach with the owner.
  - Merge only when the last round covers the PR's latest commit against its current base branch (the branch by name; the base moving on alone needs no new round) and leaves no **Wichtig** open (its closing agent's count, or PR-Agent's comment when no earlier round had a **Wichtig**), and CI is green on the PR's latest commit: `github / test` from the GitHub mirror shows success there. No status there yet means wait, not merge; if it never comes, check the mirror or ask the owner. PR-Agent's status does not count here.
  - Before the merge, the owner confirms in words, not by reading code, a PR that changes CI or release workflows (`.gitea/`, `.github/`), this review rule (`CLAUDE.md`, `REVIEW.md`), secrets or what is published (a release, a tag, a new public page, site or package; rebuilding what is already published does not count).
  - `REVIEW.md` in the repository root says what failure looks like here, what to check in particular, what to skip and how to report. A repository without one gets it in its next PR.
- PR-Agent's status is not a CI check. The owner may post `/review` at any time (PR-Agent takes no other command here); the merge waits for that run and handles its findings like a round's. Owner and session post from the same account, so a run that fails is handled the same whoever posted `/review`: as above, once more, then the fallback.
