# Leuchtfeuer – Roadmap

## Update-Hinweis

Ein Hinweis, wenn es ein neueres Release gibt: das Gerätepaket wird mit `install.sh` aufgespielt und meldet sich sonst nie. Die Home-Assistant-Integration bekommt keinen, dort meldet HACS die Updates. Format, Plattformen und Regeln: `shrippen.github.io/overview/VERSIONS.md`.

- [ ] Der Server prüft (`leuchtfeuerd`, Version aus `var version`; kleine Cache-Datei, `/data` ist knapp) höchstens einmal am Tag `https://shrippen.github.io/versions.json` (Projekt `leuchtfeuer`), speichert die Antwort im Datenordner, bleibt bei Fehlern still; nur Format 1 und `https`-Links. Kein Abruf aus dem Browser, die CSP bleibt `connect-src 'self'`
- [ ] Hinweis nur für Admins als Kante `.callout.has-x` mit Link zur Release-Seite; „Ausblenden“ gilt bis zur nächsten Version
- [ ] Abschaltbar in den Einstellungen und per `LEUCHTFEUER_UPDATE_CHECK=0`; im Demo-Modus immer aus
- [ ] README: was abgerufen wird (`https://shrippen.github.io/versions.json` ohne Parameter, höchstens einmal am Tag) und wie man es abschaltet
- [ ] Nach jedem Release `python3 ../shrippen.github.io/overview/tools/build-versions.py` und `docs/versions.json` dort committen
