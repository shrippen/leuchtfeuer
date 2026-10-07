# Leuchtfeuer – Roadmap

## Update-Hinweis

Ein Hinweis, wenn es ein neueres Release gibt: das Gerätepaket wird mit `install.sh` aufgespielt und meldet sich sonst nie. Die Home-Assistant-Integration bekommt keinen, dort meldet HACS die Updates. Format, Plattformen und Regeln: `shrippen.github.io/overview/VERSIONS.md`.

- [ ] Der Server prüft (`leuchtfeuerd`, Version aus `var version`; kleine Cache-Datei, `/data` ist knapp) höchstens einmal am Tag `https://shrippen.github.io/versions.json` (Projekt `leuchtfeuer`), speichert die Antwort im Datenordner, bleibt bei Fehlern still; nur Format 1 und `https`-Links. Kein Abruf aus dem Browser, die CSP bleibt `connect-src 'self'`
- [ ] Hinweis nur für Admins als Kante `.callout.has-x` mit Link zur Release-Seite; „Ausblenden“ gilt bis zur nächsten Version
- [ ] Abschaltbar in den Einstellungen und per `LEUCHTFEUER_UPDATE_CHECK=0`; im Demo-Modus immer aus
- [ ] README: was abgerufen wird (`https://shrippen.github.io/versions.json` ohne Parameter, höchstens einmal am Tag) und wie man es abschaltet
- [ ] Nach jedem Release `python3 ../shrippen.github.io/overview/tools/build-versions.py` und `docs/versions.json` dort committen

## Release 1.0 veröffentlichen

Stand: `main` und die Tags `v1.0.0` und `v1.0.1` liegen auf Gitea. Der Release-Lauf für `v1.0.1` (Run #52) baut und paketiert alles, scheitert aber im letzten Schritt, weil zwei Secrets im Repository fehlen. `v1.0.0` ist ein toter Tag (der Build brach damals an den Bind-Mounts ab, siehe `tools/docker-run.sh`) und hat kein Release.

- [ ] Secret `RELEASE_TOKEN` in Gitea anlegen (Repo > Einstellungen > Actions > Secrets): Gitea-Token mit Schreibrecht auf dieses Repository. Ohne ihn antwortet die API im Schritt „Release auf Gitea“ mit 404
- [ ] Signaturschlüssel erzeugen: `(cd src/relsign && go run . keygen ~/.config/leuchtfeuer/release.key)`; der private Schlüssel kommt als Secret `LEUCHTFEUER_SIGNING_KEY` (Inhalt, Base64) in Gitea und zusätzlich in eine Sicherung, der öffentliche in `docs/release-key.pub`. Ohne Schlüssel entstehen keine `.sig`-Dateien, und Updates aus der Weboberfläche und `install.sh --prebuilt` prüfen nichts
- [ ] Release-Lauf für `v1.0.1` in Gitea (Actions) neu starten, danach prüfen: Release „Leuchtfeuer v1.0.1“ mit Paketen für `invoke` und `generic-amd64/-arm64/-armv7`, jeweils `.sha256` und `.sig`
- [ ] Toten Tag `v1.0.0` auf Gitea (und im GitHub-Spiegel) entfernen, oder dort ein Release aus ihm anlegen
- [ ] Danach den Punkt „Nach jedem Release `build-versions.py`“ aus dem Abschnitt Update-Hinweis erledigen
