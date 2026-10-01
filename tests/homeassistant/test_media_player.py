"""Tests für Media-Player, Sensoren, Tasten, Schalter und den Push-Koordinator.

Ausführen (Repo-Wurzel):
    tests/homeassistant/run.sh
oder direkt:
    tests/homeassistant/run.sh
"""

from __future__ import annotations

import asyncio
import copy
from typing import Any
from unittest.mock import patch

import pytest

from homeassistant.components.media_player import (
    ATTR_INPUT_SOURCE,
    ATTR_INPUT_SOURCE_LIST,
    ATTR_MEDIA_ANNOUNCE,
    ATTR_MEDIA_CONTENT_ID,
    ATTR_MEDIA_CONTENT_TYPE,
    ATTR_MEDIA_TITLE,
    ATTR_MEDIA_VOLUME_LEVEL,
    ATTR_MEDIA_VOLUME_MUTED,
    DOMAIN as MP_DOMAIN,
    SERVICE_PLAY_MEDIA,
    SERVICE_SELECT_SOURCE,
    MediaPlayerState,
)
from homeassistant.config_entries import SOURCE_REAUTH, ConfigEntryState
from homeassistant.const import (
    ATTR_ENTITY_ID,
    SERVICE_MEDIA_STOP,
    SERVICE_TURN_OFF,
    SERVICE_VOLUME_MUTE,
    SERVICE_VOLUME_SET,
    SERVICE_VOLUME_UP,
    STATE_UNAVAILABLE,
)
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ServiceValidationError

from custom_components.leuchtfeuer.api import CannotConnect, InvalidAuth
from custom_components.leuchtfeuer.coordinator import LeuchtfeuerData
from custom_components.leuchtfeuer.media_player import media_state

from lf_testdata import HOST, SETTINGS, STATUS

MP = "media_player.kuche"
EVENTS = "custom_components.leuchtfeuer.api.LeuchtfeuerClient.events"


async def _idle_events(self):
    """Ereignisstrom, der nichts liefert und offen bleibt."""
    await asyncio.Event().wait()
    yield  # pragma: no cover


@pytest.fixture
async def setup(hass: HomeAssistant, aioclient_mock, mock_entry):
    aioclient_mock.get(f"{HOST}/api/status", json=STATUS)
    aioclient_mock.get(f"{HOST}/api/settings", json=SETTINGS)
    for path in ("volume", "mute", "radio/play", "radio/url", "radio/stop", "announce", "action", "briefing/start"):
        aioclient_mock.post(f"{HOST}/api/{path}", json={"ok": True})
    mock_entry.add_to_hass(hass)
    with patch(EVENTS, _idle_events):
        assert await hass.config_entries.async_setup(mock_entry.entry_id)
        await hass.async_block_till_done()
        yield mock_entry
        assert await hass.config_entries.async_unload(mock_entry.entry_id)


def _push(hass: HomeAssistant, entry, status: dict[str, Any]) -> None:
    coord = entry.runtime_data
    coord.async_set_updated_data(LeuchtfeuerData(status=status, stations=coord.data.stations))


def _last_post(aioclient_mock) -> tuple[str, Any]:
    method, url, body, _ = aioclient_mock.mock_calls[-1]
    assert method == "POST"
    return url.path, body


@pytest.mark.parametrize(
    ("sources", "player", "expected"),
    [
        ([], {"kind": "", "state": "idle"}, MediaPlayerState.IDLE),
        ([{"name": "spotify", "state": "playing"}], {}, MediaPlayerState.PLAYING),
        ([{"name": "spotify", "state": "paused"}], {}, MediaPlayerState.PAUSED),
        (
            [{"name": "announce", "state": "playing"}, {"name": "spotify", "state": "paused", "muted": True}],
            {},
            MediaPlayerState.PLAYING,
        ),
        ([], {"kind": "radio", "state": "buffering"}, MediaPlayerState.BUFFERING),
        ([], {"kind": "radio", "state": "reconnecting"}, MediaPlayerState.BUFFERING),
        ([], {"kind": "radio", "state": "playing"}, MediaPlayerState.PLAYING),
    ],
)
def test_state_mapping(sources, player, expected) -> None:
    assert media_state({"sources": sources, "player": player}) == expected


async def test_entities(hass: HomeAssistant, setup) -> None:
    st = hass.states.get(MP)
    assert st.state == MediaPlayerState.PLAYING
    assert st.attributes[ATTR_MEDIA_VOLUME_LEVEL] == 0.3
    assert st.attributes[ATTR_MEDIA_VOLUME_MUTED] is False
    assert st.attributes[ATTR_INPUT_SOURCE_LIST] == ["NDR 2", "Deutschlandfunk"]
    assert st.attributes[ATTR_INPUT_SOURCE] == "NDR 2"
    assert st.attributes[ATTR_MEDIA_TITLE] == "Song - Band"

    assert hass.states.get("sensor.kuche_soc_temperature").state == "51.2"
    assert hass.states.get("sensor.kuche_wi_fi_signal").state == "-58"
    assert hass.states.get("sensor.kuche_next_alarm").state == "2026-10-02T05:30:00+00:00"
    assert hass.states.get("sensor.kuche_active_source").state == "radio"
    assert hass.states.get("sensor.kuche_voice_assistant").state == "idle"
    assert hass.states.get("sensor.kuche_last_heard").state == "wie spät ist es"
    assert hass.states.get("switch.kuche_voice_microphone").state == "on"
    for name in ("briefing", "listen", "stop_all", "chime"):
        assert hass.states.get(f"button.kuche_{name}") is not None

    # Push: Spotify übernimmt
    status = copy.deepcopy(STATUS)
    status["sources"] = [
        {"name": "spotify", "state": "playing", "title": "Track", "artist": "Artist", "album": "Album"},
        {"name": "radio", "state": "paused", "muted": True},
    ]
    status["activeSource"] = "spotify"
    status["player"] = {"kind": "", "state": "idle"}
    status["volume"] = 55
    status["muted"] = True
    _push(hass, setup, status)
    await hass.async_block_till_done()
    st = hass.states.get(MP)
    assert st.attributes[ATTR_INPUT_SOURCE] == "spotify"
    assert st.attributes[ATTR_MEDIA_TITLE] == "Track"
    assert st.attributes["media_artist"] == "Artist"
    assert st.attributes[ATTR_MEDIA_VOLUME_LEVEL] == 0.55
    assert st.attributes[ATTR_MEDIA_VOLUME_MUTED] is True
    assert hass.states.get("sensor.kuche_active_source").state == "spotify"


async def test_media_services(hass: HomeAssistant, setup, aioclient_mock) -> None:
    async def call(service: str, data: dict[str, Any], domain: str = MP_DOMAIN) -> tuple[str, Any]:
        await hass.services.async_call(domain, service, {ATTR_ENTITY_ID: MP, **data}, blocking=True)
        return _last_post(aioclient_mock)

    assert await call(SERVICE_VOLUME_SET, {ATTR_MEDIA_VOLUME_LEVEL: 0.42}) == ("/api/volume", {"volume": 42})
    assert await call(SERVICE_VOLUME_UP, {}) == ("/api/volume", {"delta": 5})
    assert await call(SERVICE_VOLUME_MUTE, {ATTR_MEDIA_VOLUME_MUTED: True}) == ("/api/mute", {"muted": True})
    assert await call(SERVICE_MEDIA_STOP, {}) == ("/api/radio/stop", {})
    assert await call(SERVICE_SELECT_SOURCE, {ATTR_INPUT_SOURCE: "Deutschlandfunk"}) == (
        "/api/radio/play",
        {"index": 1},
    )
    with pytest.raises(ServiceValidationError):
        await call(SERVICE_SELECT_SOURCE, {ATTR_INPUT_SOURCE: "gibt es nicht"})

    url = "https://example.com/stream.mp3"
    play = {ATTR_MEDIA_CONTENT_ID: url, ATTR_MEDIA_CONTENT_TYPE: "music"}
    assert await call(SERVICE_PLAY_MEDIA, play) == (
        "/api/radio/url",
        {"name": "Home Assistant", "url": url, "uuid": ""},
    )
    assert await call(SERVICE_PLAY_MEDIA, {**play, ATTR_MEDIA_ANNOUNCE: True}) == (
        "/api/announce",
        {"url": url, "name": "Home Assistant"},
    )


async def test_play_media_source(hass: HomeAssistant, setup, aioclient_mock) -> None:
    from homeassistant.components.media_source import PlayMedia

    resolved = PlayMedia(url="http://ha.lan:8123/api/tts_proxy/abc.mp3", mime_type="audio/mpeg")
    with patch(
        "custom_components.leuchtfeuer.media_player.media_source.async_resolve_media", return_value=resolved
    ) as resolve:
        await hass.services.async_call(
            MP_DOMAIN,
            SERVICE_PLAY_MEDIA,
            {
                ATTR_ENTITY_ID: MP,
                ATTR_MEDIA_CONTENT_ID: "media-source://tts/cloud?message=Hallo",
                ATTR_MEDIA_CONTENT_TYPE: "music",
                ATTR_MEDIA_ANNOUNCE: True,
            },
            blocking=True,
        )
    assert resolve.call_args.args[1] == "media-source://tts/cloud?message=Hallo"
    assert _last_post(aioclient_mock) == ("/api/announce", {"url": resolved.url, "name": "Home Assistant"})


async def test_custom_services(hass: HomeAssistant, setup, aioclient_mock) -> None:
    await hass.services.async_call(
        "leuchtfeuer", "announce", {ATTR_ENTITY_ID: MP, "tone": "bell", "volume": 40}, blocking=True
    )
    assert _last_post(aioclient_mock) == ("/api/announce", {"tone": "bell", "volume": 40})
    await hass.services.async_call("leuchtfeuer", "action", {ATTR_ENTITY_ID: MP, "action": "radio_next"}, blocking=True)
    assert _last_post(aioclient_mock) == ("/api/action", {"action": "radio_next"})
    await hass.services.async_call("leuchtfeuer", "briefing", {ATTR_ENTITY_ID: MP}, blocking=True)
    assert _last_post(aioclient_mock) == ("/api/briefing/start", {})
    with pytest.raises(ServiceValidationError):
        await hass.services.async_call("leuchtfeuer", "announce", {ATTR_ENTITY_ID: MP}, blocking=True)


async def test_button_and_switch(hass: HomeAssistant, setup, aioclient_mock) -> None:
    await hass.services.async_call("button", "press", {ATTR_ENTITY_ID: "button.kuche_listen"}, blocking=True)
    assert _last_post(aioclient_mock) == ("/api/action", {"action": "voice"})

    calls = len(aioclient_mock.mock_calls)
    # Mikrofon ist an: "an" schickt nichts, "aus" schaltet um
    await hass.services.async_call(
        "switch", "turn_on", {ATTR_ENTITY_ID: "switch.kuche_voice_microphone"}, blocking=True
    )
    assert len(aioclient_mock.mock_calls) == calls
    await hass.services.async_call(
        "switch", SERVICE_TURN_OFF, {ATTR_ENTITY_ID: "switch.kuche_voice_microphone"}, blocking=True
    )
    assert _last_post(aioclient_mock) == ("/api/action", {"action": "voice_mute"})


async def test_disconnect_and_reauth(hass: HomeAssistant, aioclient_mock, mock_entry) -> None:
    """Strom bricht ab -> unverfügbar; Status kommt -> verfügbar; 401 -> Reauth."""
    aioclient_mock.get(f"{HOST}/api/status", json=STATUS)
    aioclient_mock.get(f"{HOST}/api/settings", json=SETTINGS)
    mock_entry.add_to_hass(hass)

    step = asyncio.Queue()

    async def events(self):
        while True:
            item = await step.get()
            if isinstance(item, Exception):
                raise item
            yield "status", item

    with (
        patch(EVENTS, events),
        patch("custom_components.leuchtfeuer.coordinator.BACKOFF_MIN", 0),
    ):
        assert await hass.config_entries.async_setup(mock_entry.entry_id)
        await hass.async_block_till_done()
        assert hass.states.get(MP).state == MediaPlayerState.PLAYING

        await step.put(CannotConnect("weg"))
        await hass.async_block_till_done()
        assert hass.states.get(MP).state == STATE_UNAVAILABLE
        assert hass.states.get("sensor.kuche_soc_temperature").state == STATE_UNAVAILABLE

        status = copy.deepcopy(STATUS)
        status["sources"], status["player"] = [], {"kind": "", "state": "idle"}
        await step.put(status)
        await hass.async_block_till_done()
        assert hass.states.get(MP).state == MediaPlayerState.IDLE

        await step.put(InvalidAuth("401"))
        await hass.async_block_till_done()
        assert hass.states.get(MP).state == STATE_UNAVAILABLE
        flows = hass.config_entries.flow.async_progress()
        assert any(f["context"]["source"] == SOURCE_REAUTH for f in flows)

        assert await hass.config_entries.async_unload(mock_entry.entry_id)
        assert mock_entry.state is ConfigEntryState.NOT_LOADED


async def test_setup_auth_failed(hass: HomeAssistant, aioclient_mock, mock_entry) -> None:
    aioclient_mock.get(f"{HOST}/api/status", status=401, json={"error": "invalid API key"})
    mock_entry.add_to_hass(hass)
    assert not await hass.config_entries.async_setup(mock_entry.entry_id)
    assert mock_entry.state is ConfigEntryState.SETUP_ERROR
