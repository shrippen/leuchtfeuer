# Leuchtfeuer web API

The web interface of a speaker (`leuchtfeuerd`, port 80, or 443 with `WEB_TLS="on"`) is a JSON API. Everything the
interface can do, scripts, Home Assistant (`custom_components/leuchtfeuer`, see [HOMEASSISTANT.md](HOMEASSISTANT.md)), other Leuchtfeuer speakers
and Prometheus can do through it as well.

## Access

| Way | How | Allowed |
|---|---|---|
| Session | `POST /api/login {"password": "..."}` sets the cookie `leuchtfeuer_session` (HttpOnly, SameSite=Strict, 30 days) | everything |
| API key | header `Authorization: Bearer lf_<64 hex>`; create one in the web interface under **System > Access** | `/api/*` and `/metrics`, except access management |
| API key, scope `read` | as above | only `GET`/`HEAD` (status, settings, events, logs, metrics) |

- Keys are shown once when they are created. The speaker stores only their SHA-256 (`/data/leuchtfeuer/tokens.json`).
- Access management needs a session and never works with a key. This covers API keys (`/api/tokens*`), SSH keys
  (`/api/ssh-keys*`), other speakers (`/api/peers*`), the device section with the web password
  (`PUT /api/settings/device`), backup and restore.
- **Cross-origin protection:** a request with a session cookie that changes something (`POST`, `PUT`) must come from
  the speaker's own page. The `Origin` (or `Referer`) host must match `Host`; otherwise the answer is `403`. Requests
  with an API key carry no cookie and are not checked.
- **Security headers:** every response carries a strict `Content-Security-Policy`, `X-Frame-Options: DENY`,
  `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin` and a `Permissions-Policy` that grants the
  microphone only to the page itself (for room measurement).

**Errors:** status `400`, `401`, `403` or `5xx` with `{"error": "message"}`. Many messages are German, some are
bilingual (`"deutsch / english"`).

**Successful actions:** `{"ok": true}`.

```sh
curl -H "Authorization: Bearer $KEY" http://speaker.lan:8080/api/status
curl -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' -d '{"volume":30}' http://speaker.lan:8080/api/volume
```

## Status and live events

### `GET /api/status`

| Field | Meaning |
|---|---|
| `name`, `version` | device name, installed version |
| `volume`, `muted`, `volumeKnown` | volume 0-100, mute; on the Invoke from the vendor `audio-ui` (`volumeKnown` is false until it has answered) |
| `device` | the target: `{id, model, capabilities[], link, linkOK}`. `capabilities` lists the extensions this device has: `buttons`, `ring` (light ring), `vendorSounds`, `vendorVolume`; `link` names the vendor software leuchtfeuerd talks to (`""` = none). Clients should hide what is missing. |
| `wamp` | same as `device.linkOK` (kept for older clients) |
| `output` | chosen output of the sound chain: `{id: "default"\|"card:<n>"\|"bt:<MAC>", name, ok}`; `ok` is false while a Bluetooth speaker is not connected |
| `player` | the speaker's own player: `{kind, name, state, title}`; `kind` is `radio`, `alarm`, `timer`, `briefing` or `""`; `state` is `idle`, `buffering`, `playing` or `reconnecting` |
| `sources` | sources that are playing or paused, playing first: `[{name, state, title, artist, album, since, muted}]`. Names: `spotify upnp cast airplay bluetooth sendspin tidal snapcast radio alarm announce briefing measure`. `muted` means paused because another source took over |
| `activeSource` | the source in front (`""` = none) |
| `alarm` | `{active, name, state: ringing\|snoozed\|idle, snoozeUntil}` |
| `nextAlarm`, `nextAlarmName` | next alarm (RFC 3339) |
| `timers` | `[{id, name, remaining, total}]` (seconds) |
| `sleepSecs` | time left on the sleep timer, 0 = off |
| `wifi` | `{ssid, bssid, freq, rssi, linkMbps, gateway, lossPct, rttMs, good, log[]}` |
| `sys` | `{tempC, uptimeSecs, load1, memTotalMB, memFreeMB, dataFreeMB, dataSizeMB, services[]}`; each service is `{name, title, group, process, ports, requires, running, enabled, missing, fails, restarts, waitSecs, failing}`; `missing`: a program from `requires` is not installed |
| `bluetooth`, `btMode` | pairing window `{open, until}`; `button` or `always` |
| `clock` | `{offsetMs, checked, server, lastSync, synced, error, noNtpTool}` |
| `update` | `{current, latest, available, keySet, busy, pending, rolledBack, message}` |
| `voice` | voice assistant: `{enabled, connected, state: off\|idle\|listening\|thinking\|speaking, streaming, lastHeard, lastAnswer, error, muted}` |
| `briefing` | a briefing is playing |
| `holiday` | name of today's public holiday in the configured region |
| `mqtt`, `viz`, `buttons`, `now`, `timezone`, `webDefaultPassword`, `demo` | further state for the web interface |

### `GET /api/events`

Server-Sent Events.

- **`status`:** the full status as above. Sent on every change (volume, source, alarm, timer, settings ...) and
  otherwise every 10 s.
- **`settings`:** `{"changed": true}`. Read `/api/settings` again.

Works with a session or an API key (browsers send the cookie; other clients send the `Authorization` header).

### `GET /metrics`

Prometheus text format. Example `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: leuchtfeuer
    authorization: { credentials: lf_... }   # key with scope "read"
    static_configs: [{ targets: ["speaker.lan:8080"] }]
```

The metrics, all prefixed `leuchtfeuer_`:

- `info{version,name}`
- `uptime_seconds`
- `temperature_celsius`
- `load1`
- `memory_total_bytes`, `memory_available_bytes`
- `data_free_bytes`
- `cpu_seconds_total{mode}`
- `wifi_rssi_dbm`, `wifi_loss_percent`, `wifi_rtt_seconds`, `wifi_good`
- `volume_percent`, `muted`
- `wamp_connected`, `mqtt_connected`
- `clock_offset_seconds`
- `service_enabled|up|failures{service}`
- `service_restarts_total{service}`
- `source_playing{source}`
- `audio_underruns_total{service}`: lines with "underrun"/"xrun" in the logs since leuchtfeuerd started
- `log_lines_total{service}`
- `next_alarm_timestamp_seconds`
- `sleep_timer_seconds`

## Playback and control

| Request | Body | |
|---|---|---|
| `POST /api/volume` | `{"volume": 0-100}` or `{"delta": -5}` | |
| `POST /api/mute` | `{"muted": true}` | |
| `POST /api/radio/play` | `{"index": 0}` | station from the list (`settings.radio`) |
| `POST /api/radio/url` | `{"name": "...", "url": "https://...", "uuid": ""}` | play any stream as web radio without saving it (Home Assistant, preview in the station search) |
| `POST /api/radio/stop` | `{}` | stops the player (and a running briefing) |
| `GET /api/radio/search?q=jazz&country=DE` | | station directory radio-browser.info: `[{stationuuid, name, url, countrycode, codec, bitrate, tags, votes}]` |
| `POST /api/announce` | `{"url": "https://..."}` or `{"tone": "chime\|bell\|beep"}`, optional `volume` (%), `name` | over the music, which is lowered meanwhile |
| `POST /api/action` | `{"action": "..."}` | see below |
| `POST /api/sleep` | `{"minutes": 30}` (0 = off) | |
| `POST /api/alarm/stop`, `/api/alarm/snooze` | `{}` | |
| `POST /api/alarms/skip` | `{"id": "...", "skip": true}` | skip an alarm's next date once (or undo the skip) |
| `POST /api/timers` | `{"name": "Tea", "seconds": 240}` | |
| `POST /api/timers/cancel` | `{"id": "..."}` (`"*"` = all) | |
| `POST /api/bluetooth/pairing` | `{"action": "open\|close\|toggle"}` | |
| `POST /api/bluetooth/control` | `{"action": "play\|pause\|stop\|next\|previous"}` | the connected phone (AVRCP) |
| `POST /api/briefing/start`, `/api/briefing/stop` | `{}` | |
| `GET /api/briefing/preview` | | the texts and audio addresses of the briefing right now: `[{type, text, audio, error}]` |
| `POST /api/voice/listen` | `{}` | voice assistant: listen now (push-to-talk) |
| `POST /api/measure/start` | `{"seconds": 12}` (3-40) | pink noise for room measurement, sound neutral, other sources paused |
| `POST /api/measure/stop` | `{}` | |

**Actions:** use these with `/api/action`, as button mappings and from Home Assistant:

| Action | What it does |
|---|---|
| `none` | nothing |
| `smart` | snooze a ringing alarm or end a ringing timer, otherwise toggle mute |
| `mute_toggle`, `volume_up`, `volume_down` | mute and volume |
| `radio_toggle`, `radio_next` | radio on/off, next station |
| `radio_1` … `radio_5` | play favourite station 1 to 5 |
| `alarm_stop`, `alarm_snooze` | alarm |
| `timer_dismiss`, `timers_cancel` | end a ringing timer, cancel all timers |
| `stop_all` | stop everything |
| `bt_pairing` | open or close the Bluetooth pairing window |
| `sleep_toggle` | sleep timer 30 min on/off |
| `chime` | play the chime |
| `briefing` | play the briefing |
| `voice` | voice assistant: listen now |
| `voice_mute` | voice assistant: microphone on/off |

**Buttons:** a button can also have the press types `double` and `triple`. Once one of them is mapped, a short press
waits 450 ms for more presses.

## Output (sound card, Bluetooth speaker)

Where the sound chain plays: the device's built-in output (`default`), another sound card (`card:<n>`, from
`/proc/asound/cards`) or a paired Bluetooth speaker (`bt:<MAC>`, A2DP source through bluez-alsa). Device-specific, not
part of the copyable settings sections.

| Request | Body / answer |
|---|---|
| `GET /api/outputs` | `{current, outputs[{id, kind: default\|card\|bluetooth, name, available, current}], bluetooth: {available, scanning, busy, error, devices[{addr, name, paired, connected, rssi}]}}`. `bluetooth.available` is false while the Bluetooth agent is off. Only paired speakers appear in `outputs`. |
| `PUT /api/outputs` | `{"id": "default"\|"card:1"\|"bt:AA:BB:CC:DD:EE:FF"}`: switch (the answer is the new listing). Writes `output.conf`, stops the speaker's own playback and restarts the audio services (they read the ALSA configuration at start). |
| `POST /api/outputs/bluetooth` | `{"action": "scan"\|"stop"\|"connect"\|"disconnect"\|"forget", "addr": "AA:BB:…"}`. `scan` looks for 30 s; `connect` pairs first if needed. Poll `GET /api/outputs` for the result (`error`, `busy`). |

## Settings

`GET /api/settings` returns `{settings, device, actions, holidayRegions, sourceNames, services, groups, podcasts,
copySections, buttonNames, version}`. `settings.setupDone` is false only on a fresh installation (the web interface then opens its setup assistant); `services[].uses` lists the config keys a service reads only at start (`DEVICE_NAME`, `SENDSPIN_SERVER`). Secrets come back empty: the MQTT password, the Home Assistant token and the keys of other
speakers.

`PUT /api/settings/<section>` replaces one section. Sending a secret empty keeps the stored one.

| Section | Content |
|---|---|
| `radio` | `[{name, url, uuid?}]` |
| `alarms` | `[{id, name, time "07:30", days [0-6], enabled, source "tone"\|"radio:<n>"\|"url:<address>"\|"briefing", volume, rampSecs, snoozeMin, maxMins, sunriseMin, fadeOutSecs, skipHolidays, skipDate}]`. If a stream does not start within 15 s or ends early, the built-in alarm tone rings. |
| `buttons` | `{"<button>": {"short\|long\|double\|triple\|<value>": "<action>"}}` |
| `sources` | `{policy "last"\|"mix", max, duckDB, limits {"<source>": {max, start, trimDB}}}`. `trimDB` (0-20) permanently lowers a source to even out loudness. |
| `eq` | `{bass, treble, loudness, night, roomOn, room [{hz, db, q}]}`, room filters: up to 6, -15…+6 dB, Q 0.3…10 |
| `viz` | light ring `{mode, color, rgb, brightness, rotate, timerRing}` (only meaningful with capability `ring`) |
| `briefing` | `{lang "de"\|"en", place, lat, lon, items [{type, on, name, url, days, text, region}], then ""\|"radio:<n>", tts ""\|"ha"\|"url", ttsUrl}`. Item types: `greeting weather warnings pollen calendar podcast ha text` |
| `homeAssistant` | `{url, token, ttsEngine}`. Used by the briefing for speech (`/api/tts_get_url`) and templates (`/api/template`, admin token). |
| `voice` | `{enabled, port 10700, mic "leuchtfeuer_mic" (or "plughw:X,Y"), mode "wake"\|"button", area, duckDB, muted}` |
| `mqtt` | `{enabled, host, port, user, pass, discovery, tls, insecure}` |
| `wifi` | Wi-Fi guard `{enabled, intervalSec, lossPct, rttMs, prefer5GHz, penaltyMins, dryRun}` |
| `syslog` | `{enabled, host, port 514, proto "udp"\|"tcp"}` (RFC 5424, facility local0) |
| `holidays` | `{region: ""\|"DE"\|"DE-BY"…}` |
| `timezone` | `{timezone: "Europe/Berlin"}` (any IANA zone; the time zone database is built in) |
| `setup` | `{done: true}`: the setup assistant is finished or skipped |
| `update` | `{url}` (release page, https) |
| `device` | `{name, dhcpHostname, sendspinServer, bluetoothPairing, webPassword?}`. Session only. |

Further helpers for the settings:

- `GET /api/briefing/geocode?q=Hamburg&lang=de`: `[{name, admin, country, lat, lon}]` (Open-Meteo)
- `GET /api/briefing/pollen-regions`: `[{id, name}]` (DWD)
- `POST /api/briefing/check-calendar {url, days}`: fetches a calendar and reports `{total, day, next[]}` (events in the file, on that day, the first titles)
- `GET /api/audio/inputs`: capture devices `[{id, name, card, device, recommended}]`; first `leuchtfeuer_mic` (the target's microphone in the audio chain), then the cards raw (no loopback)
- `GET /api/audio/level?dev=leuchtfeuer_mic`: microphone level `{rms, peak}` in dBFS (while the voice assistant captures, its own level)
- `GET /api/ha/options`: from Home Assistant (address and admin token under `homeAssistant`): `{tts[], areas[], sensors[{id, name, unit}]}`
- `POST /api/ha/test {url, token}` (empty = stored): `{ok, version, admin, tts, message}`
- `GET /api/discover?kind=sendspin|mqtt|ha`: searches the Wi-Fi for 3 s (mDNS) and returns `[{name, host, port, url?}]`
- `POST /api/eq/preview`: plays a quiet 6 s sample (bass, chord, treble) through the sound and room correction
- `GET /api/radio/search?country=DE` without `q`: the most popular stations of that country
- `POST /api/services/group {group, enabled}`: switch a service group (Spotify, AirPlay …) on or off
- `POST /api/services/restart {name}`: restart a service

## Sounds

- `GET /api/sounds` (`?rescan=1` searches again): returns `{tones, vendor}`.
  - `tones`: Leuchtfeuer's own tones `alarm timer chime bell beep`, each `{id, replaced, seconds}`.
  - `vendor`: the WAV files of the vendor software found on the speaker (start, error …), each
    `{path, name, format {rate, channels, bits}, seconds, replaced, mounted}`.
- `POST /api/sounds/upload?target=tone:<id>|vendor:<path>`: the body is the audio file (WAV, MP3, OGG, FLAC; at most
  12 MB). It is converted to the format of the original and is at most 30 s for vendor sounds, 60 s for tones. Vendor
  sounds are bind-mounted over the original at once and after every boot. Only paths from the list are accepted.
- `POST /api/sounds/reset {target}`: back to the original.
- `POST /api/sounds/play {target, original}`: play on the speaker (like an announcement).
- `GET /api/sounds/file?target=…[&original=1]`: the file as `audio/wav`.

## Logs

- `GET /api/logs?name=<service>`: the last 200 lines.
- `GET /api/logs/names`: the known logs.
- `GET /api/logs/stream?service=<name>` (empty = all): Server-Sent Events `line` with `{time, service, text}`. It
  first sends the last 200 lines, then live lines.

## Access management (session only)

| Request | |
|---|---|
| `GET /api/tokens` | `[{id, name, scope, created, lastUsed}]` |
| `POST /api/tokens {name, scope "read"\|"full"}` | `{token, info}`: the key is shown only now |
| `POST /api/tokens/delete {id}` | |
| `GET /api/ssh-keys` | `[{type, comment, fingerprint, options}]` |
| `POST /api/ssh-keys {key}` | adds one key (ed25519, ECDSA or RSA ≥ 2048 bit); active within 30 s |
| `POST /api/ssh-keys/delete {fingerprint}` | the last key cannot be deleted |
| `GET /api/peers` | `{peers [{name, url, online, version, volume, muted, playing, tempC, error}], found [{name, url, version, id, known}]}` (found via mDNS `_leuchtfeuer._tcp`) |
| `POST /api/peers/add {name, url, token}` | another speaker with one of its keys (scope `full`); with https the certificate fingerprint is pinned |
| `POST /api/peers/delete {url}` | |
| `POST /api/peers/copy {url, sections [...]}` | copy settings sections to the other speaker (`copySections` in `/api/settings`) |
| `POST /api/peers/update {url}` | starts the update there (needs a release key on that speaker) |
| `POST /api/peers/action {url, action}` | run an action there |

## Backup, update

- `GET /api/backup`: download the backup, tar.gz with secrets. Session only.
- `GET /api/diag`: diagnostics package without secrets.
- `POST /api/restore`: upload a backup (tar.gz body). Session only.
- `GET /api/update`: check for a new version.
- `POST /api/update/install`: install it.
- `POST /api/update/upload`: multipart `file` + `sig`.
- `POST /api/update/rollback`: back to the previous version.

Packages are per target: `leuchtfeuer-<version>-invoke.tar.gz`, `leuchtfeuer-<version>-generic-<amd64|arm64|armv7>.tar.gz`;
the update check picks the one for its own target.

## Local bus

Not part of the web API: the device-internal interface between leuchtfeuerd and its own helper programs (btagent,
castrecv; Go client `src/lfbus`). HTTP on the Unix socket `$LEUCHTFEUER_RUN/leuchtfeuer-bus.sock` (`/run` on the Invoke,
`/run/leuchtfeuer` on `generic`), mode 600, no authentication beyond file permissions.

| Request | Body / answer |
|---|---|
| `GET /events` | Server-Sent Events. First `volume` with the current state, then: `volume {volume, muted, known}`, `claim {source}` (a source starts; others pause), `bt-pairing {action: open\|close\|toggle}`, `bt-control {action: play\|pause\|stop\|next\|previous}`, `button {name, value}` (devices with buttons). A comment line every 25 s. |
| `GET /state` | `{volume, muted, known}` |
| `POST /volume` | `{volume}` or `{delta}` or `{muted}` |
| `POST /source` | `{name, state, title, artist, album}`: state of a source (now playing, source rule) |
| `POST /ring` | `{animation, repeat}`: `bt_open`, `bt_closed`, `alarm`, `timer`, `success`; no effect without a ring |
| `POST /bt` | btagent reports `{devices[{addr, name, paired, connected, rssi}], scanning, busy, error}` (speakers it sees) and gets `{ok, want}` back: the speaker that should be connected (output `bt:…`), `""` = none |

Event `bt-sink {action: scan\|stop\|connect\|disconnect\|forget, addr}`: commands from the web interface to btagent.

```sh
curl --unix-socket /run/leuchtfeuer-bus.sock http://bus/state
curl --unix-socket /run/leuchtfeuer-bus.sock -N http://bus/events
```

## Discovery (mDNS)

- **`_leuchtfeuer._tcp`:** the web interface. TXT `name`, `version`, `id` (Wi-Fi MAC without colons), `scheme`.
- **`_wyoming._tcp`:** the voice assistant (Home Assistant finds it in the Wyoming integration), when it is switched on.
