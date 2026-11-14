# Code-Audit Leuchtfeuer, 14.11.2026, Stand f968d4e

## Urteil
Das Repo ist im Großen wartbar. Der Dienst auf dem Lautsprecher ist in viele kleine Dateien zerlegt, eine je Thema. Was nur ein Gerät betrifft, steckt hinter einer festen Schnittstelle je Zielgerät, und die Kernlogik hat Tests: Wecker, Quellen, Kalender, HTTPS. Teuer werden Änderungen an zwei Sammelstellen der Weboberfläche, über die fast jede neue Funktion läuft: der Serverteil mit allen Adressen und Einstellungen und das Browserprogramm mit allen Seiten. Teuer sind auch Listen, die an mehreren Stellen von Hand gleich gehalten werden.

Dies ist das erste Audit, darum gibt es keinen Vergleich. Eine `ARCHITECTURE.md` im Wurzelverzeichnis, die festlegt, welcher Teil welchen benutzen darf, fehlt. `docs/ARCHITECTURE.md` beschreibt das laufende System; gegen diese Datei wurde geprüft.

## Kennzahlen
| | letztes Audit | jetzt |
|---|---|---|
| Quelldateien | – | 197 |
| Einträge in der Bestandsliste | – | 53 |
| größte Datei (Zeilen) | – | 1756 (Browserprogramm der Weboberfläche) |
| längste Funktion (Zeilen) | – | 561 (Liste aller Adressen der Weboberfläche) |

## Funde

### 1. Die Liste der Aktionen für Tasten steht fünfmal von Hand im Repo, und der Weg von Home Assistant über MQTT führt fünf dieser Aktionen an der gemeinsamen Stelle vorbei aus
Eine Aktion ist etwa „Wecker schlummern“ oder „Lieblingssender 3“. Wer eine neue Aktion einführt, ändert:
- den Dienst an zwei Stellen: was die Aktion tut und die Liste der erlaubten Namen,
- die Beschriftungen der Weboberfläche,
- die Home-Assistant-Erweiterung an drei Stellen: Liste, Dienstbeschreibung, Übersetzungen,
- die Doku.

Kein Test merkt, wenn eine Stelle fehlt. Fehlt der Name in der Home-Assistant-Liste, lehnt Home Assistant die Aktion ab, ohne dass jemand es bemerkt.

Dazu kommt MQTT, der Nachrichtenweg zu Home Assistant: Er führt Lautstärke, Sender, Wecker stoppen, Schlummern und Gong selbst aus, statt die gemeinsame Stelle zu rufen. Fehler verschluckt er dabei einmal, ein andermal schreibt er sie ins Protokoll. Ändert sich, was „Schlummern“ tut, muss man an beiden Stellen nachziehen.

Vorschlag: Ein Test vergleicht die Namensliste des Dienstes mit der Liste der Home-Assistant-Erweiterung. MQTT ruft für diese Fälle die gemeinsame Stelle.

Aufwand klein. Risiko: Für die gemeinsame Stelle und die MQTT-Befehle gibt es keinen Test, er käme zuerst.

Beleg: `src/leuchtfeuerd/app.go:284`, `src/leuchtfeuerd/app.go:357`, `src/leuchtfeuerd/web/app.js:661`, `custom_components/leuchtfeuer/const.py:23`, `custom_components/leuchtfeuer/services.yaml:37`, `custom_components/leuchtfeuer/strings.json:140`, `docs/API.md:136`, `src/leuchtfeuerd/mqtt.go:145`, `src/leuchtfeuerd/mqtt.go:156`, `src/leuchtfeuerd/mqtt.go:165`, `src/leuchtfeuerd/mqtt.go:171`.

### 2. Scheitert das Speichern der Einstellungen, merkt es an sieben Stellen niemand
Das Speichern schreibt die Einstellungsdatei und meldet einen Fehler zurück, etwa wenn der Speicher voll ist. Auf dem Invoke sind es nur 123 MB. Diese Stellen werten die Rückmeldung nicht aus:
- neuer Timer,
- abgebrochener Timer,
- abgelaufener Timer,
- „Wecker einmal aussetzen“ zurücksetzen,
- Leuchtring über Home Assistant,
- zwei Stellen beim Start.

Im Speicher gilt die Änderung, nach einem Neustart ist sie weg, und kein Protokoll nennt den Grund. Die Weboberfläche dagegen meldet denselben Fehler. Wer später nach verlorenen Weckern sucht, findet keine Spur.

Vorschlag: An diesen Stellen den Fehler wie überall sonst ins Protokoll schreiben.

Aufwand klein. Risiko gering; der Wecker hat Tests.

Beleg: `src/leuchtfeuerd/store.go:177`, `src/leuchtfeuerd/sched.go:104`, `src/leuchtfeuerd/sched.go:566`, `src/leuchtfeuerd/sched.go:572`, `src/leuchtfeuerd/sched.go:600`, `src/leuchtfeuerd/mqtt.go:264`, `src/leuchtfeuerd/main.go:59`, `src/leuchtfeuerd/main.go:112`.

### 3. Der Zugang zu Home Assistant und der Kalenderabruf stecken im Briefing; andere Teile greifen hinein oder bauen sie nach
Wie man Home Assistant anfragt (Adresse, Schlüssel, Antwort prüfen), steht im Morgenbriefing. Die Einrichtungshilfe benutzt dessen innere Teile. Für den Verbindungstest baut sie eine eigene Anfrage nach.

Die Kalenderprüfung vor dem Speichern wiederholt den Kalenderabruf des Briefings fast Zeile für Zeile, samt Umwandlung von „webcal://“-Adressen. Ändert sich, wie Home Assistant angesprochen wird (anderer Pfad, Zeitlimit, Zertifikat) oder wie ein Kalender geladen wird, muss man das an zwei Stellen finden. Wer das Briefing umbaut, bricht unbemerkt die Einrichtungshilfe.

Vorschlag: einen kleinen eigenen Baustein für Home Assistant und eine gemeinsame Funktion „Kalender laden“, die beide Seiten benutzen.

Aufwand klein. Risiko mittel: Das Briefing hat Tests, Verbindungstest und Kalenderprüfung haben keine.

Beleg: `src/leuchtfeuerd/briefing.go:534`, `src/leuchtfeuerd/briefing.go:557`, `src/leuchtfeuerd/options.go:95`, `src/leuchtfeuerd/options.go:219`, `src/leuchtfeuerd/options.go:232`, `src/leuchtfeuerd/briefing.go:426`, `src/leuchtfeuerd/options.go:254`.

### 4. Der Cast-Empfänger hat noch einen eigenen Lautstärkeweg, der nur auf dem Invoke passt
Laut Architektur sprechen die eigenen Programme nur über den lokalen Bus mit dem Dienst und laufen deshalb auf jedem Gerät unverändert. Der lokale Bus ist eine interne Verbindung auf dem Gerät.

Ist der Bus nicht verbunden, stellt der Cast-Empfänger die Lautstärke aber selbst am Regler „system“ der ersten Soundkarte. Diesen Regler gibt es nur auf dem Invoke. Außerdem führt er dafür einen zweiten, eigenen Stummzustand. Die Verbindung heißt im Code noch nach der Herstellersoftware des Invoke, obwohl sie längst der Bus ist.

Wer die Lautstärke ändert, muss diesen Sonderweg kennen. Auf anderen Geräten tut er nichts.

Vorschlag: den Rückweg entfernen und die Verbindung nach dem Bus benennen. Das ändert das Verhalten bei ausgefallenem Dienst, also entscheidet der Eigentümer.

Aufwand klein. Risiko: Für die Lautstärke gibt es keinen Test, nur für das Protokoll.

Beleg: `src/castrecv/main.go:46`, `src/castrecv/main.go:104`, `src/castrecv/main.go:119`, `src/castrecv/main.go:123`, `src/castrecv/link.go:17`, `docs/ARCHITECTURE.md:25`.

### 5. Die Regeln für gültige Einstellungen stehen in der Weboberfläche statt bei den Einstellungen, in einer Funktion von 320 Zeilen
Was eine Einstellung erlaubt, prüft eine einzige Funktion des Webservers für alle 20 Bereiche: Sender, Wecker, Ton, Briefing, MQTT und weitere. Ihre Komplexität beträgt 114. Komplexität zählt die Verzweigungen; erlaubt sind 15.

Andere Wege, die Einstellungen ändern, kennen diese Regeln nicht und wiederholen sie. Die Helligkeit des Leuchtrings (5 bis 100) steht viermal im Code. Die Hilfsfunktion, die einen Wert in Grenzen hält, gibt es zweimal mit gleichem Inhalt. Die Prüfung „Adresse beginnt mit http:// oder https://“ steht neunmal, jedes Mal leicht anders geschrieben.

Wer eine Einstellung hinzufügt, ändert die große Funktion. Wer eine Grenze ändert, muss alle Kopien finden.

Vorschlag: Jeder Bereich bekommt seine Prüfung neben seiner Beschreibung im Einstellungsteil, und der Webserver ruft sie nur noch auf. Dazu je eine gemeinsame Funktion für die Adressprüfung und für die Grenzen.

Aufwand mittel. Risiko hoch: Für diese Prüfungen gibt es keinen Test, nur einen für die Zugriffsrechte. Tests je Bereich kämen zuerst.

Beleg: `src/leuchtfeuerd/web.go:183`, `src/leuchtfeuerd/web.go:284`, `src/leuchtfeuerd/mqtt.go:280`, `src/leuchtfeuerd/viz.go:368`, `src/leuchtfeuerd/viz.go:386`, `src/leuchtfeuerd/viz.go:446`, `src/leuchtfeuerd/audio.go:16`, `src/leuchtfeuerd/web.go:193`, `src/leuchtfeuerd/web.go:221`, `src/leuchtfeuerd/web.go:335`, `src/leuchtfeuerd/web.go:356`, `src/leuchtfeuerd/app.go:178`, `src/leuchtfeuerd/announce.go:42`, `src/leuchtfeuerd/briefing.go:429`, `src/leuchtfeuerd/options.go:257`, `src/leuchtfeuerd/radiosearch.go:124`, `src/leuchtfeuerd/access_test.go:65`.

### 6. Was ein DNS-Anbieter für HTTPS braucht, steht an drei Stellen
Für ein Zertifikat von Let's Encrypt meldet sich der Lautsprecher beim DNS-Anbieter des Nutzers an (Cloudflare, netcup, Porkbun und andere). Dieses Wissen ist verteilt:
- welche Zugangsdaten ein Anbieter braucht und welche Pflicht sind, prüft der Dienst einmal beim Speichern und ein zweites Mal beim Verbinden, jeweils mit eigenen Fehlermeldungen,
- die Weboberfläche weiß es ein drittes Mal, nämlich welche Felder sie je Anbieter zeigt.

Ein neuer Anbieter heißt also vier Stellen ändern. Vergisst man eine, fordert die Oberfläche ein Feld nicht an, das der Dienst dann vermisst.

Vorschlag: eine Tabelle je Anbieter mit Name und benötigten Feldern im Dienst. Die Weboberfläche bekommt sie mit den übrigen Einstellungen, die Anbieterliste bekommt sie heute schon so.

Aufwand mittel. Risiko gering: Für beide Prüfungen gibt es Tests.

Beleg: `src/leuchtfeuerd/https.go:40`, `src/leuchtfeuerd/https.go:71`, `src/leuchtfeuerd/https.go:320`, `src/leuchtfeuerd/web/app.js:1339`, `src/leuchtfeuerd/web/app.js:1351`, `src/leuchtfeuerd/web/app.js:1396`, `src/leuchtfeuerd/https_test.go:20`, `src/leuchtfeuerd/https_test.go:167`.

### 7. Das Browserprogramm der Weboberfläche ist eine Datei für alle zwölf Seiten
Alle Seiten der Weboberfläche stehen in einer Datei von 1756 Zeilen: Übersicht, Radio, Wecker, Einstellungen, System und weitere. Diese Datei hat sich in der Geschichte am häufigsten geändert. Sieben Einträge der Bestandsliste kommen aus ihr.

Die laufende Aktualisierung aller Seiten steckt in einer Funktion mit einer Kette „wenn Seite X, dann …“. Ihre Komplexität ist 44, sie ist zehn Ebenen tief verschachtelt. Wer an einer Seite arbeitet, muss die ganze Datei durchsuchen und die zentrale Kette mit ändern.

Die Seiten sind schon in einem Verzeichnis eingetragen. Das ist die natürliche Naht.

Vorschlag: eine Datei je Seite, und jede Seite trägt im Verzeichnis neben Aufbau und Bedienung auch ihre laufende Aktualisierung ein.

Aufwand groß, mehrere PRs. Risiko hoch: Für die Seiten gibt es keine Tests, nur für die Raumeinmessung. Vor dem Umbau Bildschirmfotos der Seiten als Vergleich aufnehmen.

Beleg: `src/leuchtfeuerd/web/app.js:1566`, `src/leuchtfeuerd/web/app.js:1591`.

### 8. Bestandsliste: was sich an einer natürlichen Naht teilen lässt und was in Ordnung ist
Die Bestandsliste ist die Liste der Stellen, die heute über den Grenzen der Strukturprüfung liegen. Sie hat 53 Einträge.

An einer natürlichen Naht teilen lassen sich diese:
- Die Liste aller Adressen der Weboberfläche (561 Zeilen) zerfällt in die Bereiche, die sie schon mit Kommentaren abgrenzt: Klänge, Raum einmessen, andere Lautsprecher, Zugang, Update.
- Die Prüfung der Einstellungen teilt sich je Bereich, siehe Fund 5.
- Das Browserprogramm teilt sich je Seite, siehe Fund 7.
- Das Briefing (908 Zeilen) teilt sich in „Quellen abrufen“ (Wetter, Warnungen, Pollen, Kalender, Podcast) und „Abspielen“.
- Die Berechnung wiederkehrender Kalendertermine (177 Zeilen) ließe sich je Wiederholungsart teilen; sie hat Tests.

In Ordnung, wie sie sind:
- die Feiertage: eine flache Tabelle,
- die Nachrichtenverteilung im Cast-Empfänger: ein Zweig je Protokollbereich,
- die Verteilung der Aktionen und der MQTT-Befehle: ein Zweig je Name,
- der Leser für WAV-Dateien,
- das Demo-Programm, das nie ausgeliefert wird,
- die zwei Leseschleifen der Home-Assistant-Erweiterung,
- die 13 Testfunktionen: lange Prüftabellen.

Aufwand: je Naht ein PR. Risiko wie bei den Funden 5 und 7.

Beleg: `src/leuchtfeuerd/web.go:504`, `src/leuchtfeuerd/web.go:652`, `src/leuchtfeuerd/briefing.go:229`, `src/leuchtfeuerd/briefing.go:692`, `src/leuchtfeuerd/ical.go:229`, `src/leuchtfeuerd/holidays.go:28`, `src/castrecv/main.go:197`, `src/leuchtfeuerd/app.go:283`, `src/leuchtfeuerd/mqtt.go:140`.

## Seit dem letzten Audit
Erstes Audit.
