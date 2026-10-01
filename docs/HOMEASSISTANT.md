# Home Assistant

Leuchtfeuer bringt eine eigene Integration für Home Assistant mit (`custom_components/leuchtfeuer`, ab Home Assistant
2024.11). Sie spricht direkt mit der Web-API des Lautsprechers ([API.md](API.md)) und bekommt Änderungen sofort über den
Ereignisstrom `/api/events` (kein Polling).

## Installieren

**HACS:** HACS > Integrationen > Menü > *Benutzerdefinierte Repositories* >
`https://github.com/shrippen/leuchtfeuer`, Typ *Integration*. Danach „Leuchtfeuer“ installieren und Home Assistant neu
starten. Die Integration liegt im Repository unter `custom_components/leuchtfeuer`.

**Von Hand:** den Ordner `custom_components/leuchtfeuer` nach `<config>/custom_components/leuchtfeuer` kopieren und Home
Assistant neu starten.

## Einrichten

1. Am Lautsprecher unter **System > Zugang** einen API-Schlüssel mit Bereich **full** erstellen (`lf_...`). Er wird nur
   einmal angezeigt. Ein Schlüssel mit Bereich `read` wird abgelehnt, weil die Integration auch steuert.
2. Home Assistant findet den Lautsprecher per Zeroconf (`_leuchtfeuer._tcp`) und zeigt ihn unter *Einstellungen > Geräte &
   Dienste* als gefunden an. Nur noch den Schlüssel eingeben.
3. Ohne Erkennung: *Integration hinzufügen > Leuchtfeuer*, Adresse (z. B. `invoke.lan` oder `http://192.168.1.23`) und
   Schlüssel eingeben. HTTPS mit dem selbstsignierten Zertifikat des Lautsprechers geht (`https://...`, ohne
   Zertifikatsprüfung).

Ändert sich die IP-Adresse, übernimmt die Zeroconf-Erkennung die neue Adresse. Wird der Schlüssel gelöscht, fragt Home
Assistant nach einem neuen. Ist der Lautsprecher nicht erreichbar, sind seine Entitäten „nicht verfügbar“; die
Integration verbindet sich selbst wieder (2 bis 60 s Abstand).

## Entitäten

| Entität | Inhalt |
|---|---|
| `media_player` | Zustand (spielt, pausiert, puffert, bereit), Lautstärke, Stumm, Titel/Interpret der aktiven Quelle, Senderliste als Quellen, Stopp, Medien durchsuchen (Audio aus den Medienquellen von Home Assistant), `play_media` als Webradio-Stream oder als Durchsage (`announce: true`, z. B. für TTS) |
| `button` | Briefing, Zuhören (Sprachassistent), Alles stoppen, Gong |
| `sensor` | SoC-Temperatur, WLAN-Signal (Diagnose), nächster Wecker (Zeitpunkt), aktive Quelle, Sprachassistent (Zustand), zuletzt gehört (Diagnose) |
| `switch` | Mikrofon des Sprachassistenten (an = nicht stumm) |

## Aktionen (Dienste)

Alle mit einem Leuchtfeuer-`media_player` als Ziel:

- `leuchtfeuer.announce`: Durchsage über der Musik, `url` (auch `media-source://...`) oder `tone` (`chime`, `bell`,
  `beep`), optional `volume` (%).
- `leuchtfeuer.action`: eine Aktion wie bei der Tastenbelegung, z. B. `radio_next`, `sleep_toggle`, `alarm_snooze`
  (Liste in [API.md](API.md#playback-and-control)).
- `leuchtfeuer.briefing`: startet das Briefing.

```yaml
action: leuchtfeuer.announce
target:
  entity_id: media_player.kuche
data:
  tone: bell
  volume: 40
```

## Verhältnis zur MQTT-Erkennung

Die MQTT-Erkennung (Weboberfläche > Home Assistant) bleibt unverändert und kann parallel laufen. Sie liefert einzelne
Entitäten (Lautstärke als Zahl, Leuchtring als Licht, Timer, Tasten-Ereignisse ...) und braucht einen MQTT-Broker. Diese
Integration braucht keinen Broker und bringt einen echten `media_player`: TTS und Durchsagen aus Automationen,
Medienbrowser, Senderauswahl und die Media-Karte im Dashboard. Wer beides nutzt, bekommt zwei Geräte für denselben
Lautsprecher.

## Sprachassistent (Wyoming)

Der Sprach-Satellit ist davon getrennt. Sobald der Sprachassistent am Lautsprecher eingeschaltet ist, meldet er sich per
Zeroconf als `_wyoming._tcp` (Port 10700). In Home Assistant die gefundene Integration **Wyoming Protocol** hinzufügen
(oder von Hand: Adresse des Lautsprechers, Port 10700) und dem Satelliten unter *Einstellungen > Sprachassistenten* eine
Assist-Pipeline zuweisen. Die Sensoren „Sprachassistent“ und „Zuletzt gehört“ sowie der Mikrofon-Schalter dieser
Integration zeigen bzw. steuern seinen Zustand.
