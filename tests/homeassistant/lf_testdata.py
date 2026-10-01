"""Testdaten: Status und Einstellungen wie von /api/status und /api/settings."""

from __future__ import annotations

from typing import Any

HOST = "http://invoke.lan"
KEY = "lf_" + "a" * 64
DEVICE_ID = "aabbccddeeff"

STATUS: dict[str, Any] = {
    "name": "Küche",
    "version": "2.4.0",
    "volume": 30,
    "muted": False,
    "volumeKnown": True,
    "wamp": True,
    "player": {"kind": "radio", "name": "NDR 2", "state": "playing", "title": "Song - Band"},
    "sources": [
        {"name": "radio", "state": "playing", "title": "", "artist": "", "album": "", "muted": False}
    ],
    "activeSource": "radio",
    "alarm": {"active": False, "name": "", "state": "idle"},
    "nextAlarm": "2026-10-02T07:30:00+02:00",
    "nextAlarmName": "Arbeit",
    "timers": [],
    "sleepSecs": 0,
    "wifi": {"ssid": "home", "rssi": -58},
    "sys": {"tempC": 51.25},
    "voice": {"enabled": True, "connected": True, "state": "idle", "lastHeard": "wie spät ist es", "muted": False},
    "briefing": False,
}

SETTINGS: dict[str, Any] = {
    "settings": {"radio": [{"name": "NDR 2", "url": "http://a"}, {"name": "Deutschlandfunk", "url": "http://b"}]},
    "version": "2.4.0",
}
