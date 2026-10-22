# Review guide

Every PR is reviewed before it is merged (rule in `CLAUDE.md`): by PR-Agent
(owner, 22.10.2026); a new Claude agent checks the **Wichtig** of earlier
rounds, and reviews when PR-Agent fails. The
reviewer knows nothing of the session that wrote the change: it gets the
PR's intent, its diff and the repository, and this file. The owner does not read code; this review is the second reader.

## What failure looks like here

Leuchtfeuer turns small computers with a speaker into smart speakers
(`generic`, Harman Kardon Invoke): leuchtfeuerd with its web interface and
API, the receivers, Bluetooth, one ALSA chain. The devices stay on the
network for years, often with little space:

- **Opens the device:** anything reachable from the LAN without the checks
  it needs (web interface, API, MQTT, Cast, Bluetooth pairing), shell
  injection, an update accepted without a valid signature or writing
  outside the allowed paths, settings or keys leaking into a backup, log or
  diagnostics package.
- **Outdated software:** an old TLS, HTTP or media-parser library, or a
  version picked for size over a known security fix.
- **Bricks or silences the speaker:** a change to the hook, the services or
  `apply-update.sh` that leaves the device without sound, network or SSH,
  or a rollback that does not restore the previous state. Writes to mtd
  partitions without a full raw backup first.
- **Breaks the structure:** device specifics in the common core instead of
  `targets/<device>/`; a module that is not optional or wastes space on
  the device.
- **Lies:** README, `docs/` (also the German pages), API docs or comments
  that say something the code does not do.

## What to check in particular

- Shell on the device is POSIX `sh` (shellcheck `-s sh` in `tests/run.sh`);
  Go is formatted and vetted, also with the `demo` tag.
- New behaviour has a test in `tests/` or the Go packages; a change to the
  update or migration path keeps `tests/apply_update_test.sh` and
  `tests/migrate_test.sh` meaningful.
- The web UI uses Kante: `src/leuchtfeuerd/web/kante/` is vendored
  (`tools/sync-kante.sh`) and never edited; no raw colours or sizes.
- The demo mode (`demo/`) never ends up in a release package.
- API changes: `docs/API.md`; behaviour changes: the matching page in
  `docs/` and its German version.
- Bug fixes: does the fix remove the cause, with a test that failed before?

## Skip

- `src/leuchtfeuerd/web/kante/` (vendored), images, icons, screenshots.
- `docs/RESEARCH-NOTES.md`, unless the PR is about it.

## How to report

- Every finding names file and line and the concrete input or state that
  goes wrong. No guess without evidence; if unsure, say so.
- Each finding gets one label: **Wichtig** (a bug from the list above,
  must be fixed or answered before the merge), **Kleinigkeit** (optional,
  at most five) or **Bestand** (the bug was there before this PR; not
  blocking).
- The report starts with the count, e.g. "2 Wichtig, 1 Kleinigkeit" (the
  session puts the heading above it). The count of **Wichtig** holds every
  one still open, also from an earlier round, e.g. "1 Wichtig (offen aus
  Runde 1)". Only when none is open it says plainly: "Nichts zu beheben."
- In a later round: say for each open **Wichtig** of the last round whether
  it is fixed, or whether its answer is accepted, with the reason. PR-Agent
  sees no earlier round: it reports only its own findings with their labels,
  and a new Claude agent closes the open ones (`CLAUDE.md`).
- Findings in German, code names as they are.
