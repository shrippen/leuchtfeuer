"""Konstanten der Leuchtfeuer-Integration."""

from typing import Final

DOMAIN: Final = "leuchtfeuer"
MANUFACTURER: Final = "Harman Kardon"
MODEL: Final = "Invoke (Leuchtfeuer)"

# Zeroconf-TXT-Felder von _leuchtfeuer._tcp
TXT_ID: Final = "id"
TXT_NAME: Final = "name"
TXT_VERSION: Final = "version"
TXT_SCHEME: Final = "scheme"

# Wiederverbindung des Ereignisstroms (Sekunden)
BACKOFF_MIN: Final = 2
BACKOFF_MAX: Final = 60
# Der Lautsprecher schickt spätestens alle 10 s einen Status; länger Stille = Verbindung tot.
SSE_READ_TIMEOUT: Final = 35
REQUEST_TIMEOUT: Final = 10

# Aktionen für POST /api/action (docs/API.md)
ACTIONS: Final = [
    "none",
    "smart",
    "mute_toggle",
    "volume_up",
    "volume_down",
    "radio_toggle",
    "radio_next",
    "radio_1",
    "radio_2",
    "radio_3",
    "radio_4",
    "radio_5",
    "alarm_stop",
    "alarm_snooze",
    "timer_dismiss",
    "timers_cancel",
    "stop_all",
    "bt_pairing",
    "sleep_toggle",
    "chime",
    "briefing",
    "voice",
    "voice_mute",
]
TONES: Final = ["chime", "bell", "beep"]

# Quellennamen aus /api/status (sources[].name, activeSource)
SOURCES: Final = [
    "spotify",
    "upnp",
    "cast",
    "airplay",
    "bluetooth",
    "sendspin",
    "tidal",
    "snapcast",
    "radio",
    "alarm",
    "announce",
    "briefing",
    "measure",
]
VOICE_STATES: Final = ["off", "idle", "listening", "thinking", "speaking"]

SERVICE_ANNOUNCE: Final = "announce"
SERVICE_ACTION: Final = "action"
SERVICE_BRIEFING: Final = "briefing"

ATTR_URL: Final = "url"
ATTR_TONE: Final = "tone"
ATTR_VOLUME: Final = "volume"
ATTR_ACTION: Final = "action"
