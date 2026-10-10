# Leuchtfeuer – Roadmap

## Update-Hinweis

Ein Hinweis, wenn es ein neueres Release gibt: das Gerätepaket wird mit `install.sh` aufgespielt und meldet sich sonst nie. Die Home-Assistant-Integration bekommt keinen, dort meldet HACS die Updates. Format, Plattformen und Regeln: `shrippen.github.io/overview/VERSIONS.md`.

- [ ] Der Server prüft (`leuchtfeuerd`, Version aus `var version`; kleine Cache-Datei, `/data` ist knapp) höchstens einmal am Tag `https://shrippen.github.io/versions.json` (Projekt `leuchtfeuer`), speichert die Antwort im Datenordner, bleibt bei Fehlern still; nur Format 1 und `https`-Links. Kein Abruf aus dem Browser, die CSP bleibt `connect-src 'self'`
- [ ] Hinweis nur für Admins als Kante `.callout.has-x` mit Link zur Release-Seite; „Ausblenden“ gilt bis zur nächsten Version
- [ ] Abschaltbar in den Einstellungen und per `LEUCHTFEUER_UPDATE_CHECK=0`; im Demo-Modus immer aus
- [ ] README: was abgerufen wird (`https://shrippen.github.io/versions.json` ohne Parameter, höchstens einmal am Tag) und wie man es abschaltet
- [ ] Nach jedem Release `python3 ../shrippen.github.io/overview/tools/build-versions.py` und `docs/versions.json` dort committen

## Release 1.0 veröffentlichen

Stand: Releases baut und veröffentlicht nur noch GitHub (`.github/workflows/release.yml`, bei einem Tag `v*`); Gitea prüft nur und bekommt keine Release-Dateien. Bisher gibt es weder auf Gitea noch auf GitHub ein Release. Die Tags `v1.0.0` (Build brach an den Bind-Mounts ab, siehe `tools/docker-run.sh`) und `v1.0.1` (Run #52 scheiterte am fehlenden Gitea-Token) zeigen auf Commits ohne den GitHub-Workflow; ein Tag-Lauf nimmt den Workflow aus dem getaggten Commit, darum lassen sie sich nicht neu starten.

- [x] Signaturschlüssel erzeugen: `(cd src/relsign && go run . keygen ~/.config/leuchtfeuer/release.key)`; den ausgegebenen öffentlichen Schlüssel per PR in `docs/release-key.pub` eintragen
- [x] Privaten Schlüssel als Secret auf GitHub hinterlegen: `gh secret set LEUCHTFEUER_SIGNING_KEY -R shrippen/leuchtfeuer < ~/.config/leuchtfeuer/release.key`, zusätzlich sichern (z. B. Passwortmanager); ohne ihn nehmen installierte Lautsprecher keine Updates mehr an. Ohne Schlüssel entstehen keine `.sig`-Dateien, und Updates aus der Weboberfläche und `install.sh --prebuilt` prüfen nichts
- [x] Neuen Tag `v1.0.2` auf `main` setzen und auf Gitea pushen (der Spiegel bringt ihn zu GitHub), danach prüfen: Release „Leuchtfeuer v1.0.2“ auf GitHub mit Paketen für `invoke` und `generic-amd64/-arm64/-armv7`, jeweils `.sha256` und `.sig`; auf Gitea nur Tag und Release-Notes
- [ ] Tote Tags `v1.0.0` und `v1.0.1` auf Gitea entfernen (der Spiegel übernimmt das), oder sie bewusst ohne Release stehen lassen
- [x] Danach den Punkt „Nach jedem Release `build-versions.py`“ aus dem Abschnitt Update-Hinweis erledigen
