"""Tests für api.py: SSE-Rahmen, Adressen, Fehlerabbildung.

Ausführen (Repo-Wurzel):
    tests/homeassistant/run.sh
oder direkt:
    tests/homeassistant/run.sh
"""

from __future__ import annotations

import pytest

from homeassistant.helpers.aiohttp_client import async_get_clientsession

from custom_components.leuchtfeuer.api import (
    CannotConnect,
    Forbidden,
    InvalidAuth,
    LeuchtfeuerClient,
    SSEParser,
    host_id,
    normalize_url,
)

from lf_testdata import HOST, KEY


def test_sse_parser_basic() -> None:
    p = SSEParser()
    assert p.feed('event: status\ndata: {"volume": 3}\n\n') == [("status", '{"volume": 3}')]


def test_sse_parser_split_chunks_crlf_comments() -> None:
    p = SSEParser()
    out = p.feed("event: sta")
    out += p.feed("tus\r\ndata: {\"a\"")
    assert out == []
    out += p.feed(": 1}\r\n\r\n: keepalive\n\nevent: settings\ndata: {\"changed\": true}\n")
    assert out == [("status", '{"a": 1}')]
    assert p.feed("\n") == [("settings", '{"changed": true}')]


def test_sse_parser_multiline_and_default_event() -> None:
    p = SSEParser()
    assert p.feed("data: a\ndata: b\n\n") == [("message", "a\nb")]
    # Ereignis ohne data wird verworfen, Feldname wird zurückgesetzt
    assert p.feed("event: status\n\ndata:x\n\n") == [("message", "x")]


@pytest.mark.parametrize(
    ("raw", "url", "uid"),
    [
        ("invoke.lan", "http://invoke.lan", "invoke.lan"),
        ("Invoke.LAN/", "http://invoke.lan", "invoke.lan"),
        ("192.168.1.23:8080", "http://192.168.1.23:8080", "192.168.1.23:8080"),
        ("https://192.168.1.23/", "https://192.168.1.23", "192.168.1.23"),
        ("http://invoke.lan:80/api", "http://invoke.lan", "invoke.lan"),
    ],
)
def test_normalize(raw: str, url: str, uid: str) -> None:
    assert normalize_url(raw) == url
    assert host_id(url) == uid


def test_normalize_invalid() -> None:
    with pytest.raises(ValueError):
        normalize_url("ftp://x")


async def test_events_stream(hass, aioclient_mock) -> None:
    body = (
        'event: status\ndata: {"volume": 10}\n\n'
        ": ping\n\n"
        "event: status\ndata: kaputt\n\n"
        'event: settings\ndata: {"changed": true}\n\n'
    )
    aioclient_mock.get(f"{HOST}/api/events", text=body)
    client = LeuchtfeuerClient(async_get_clientsession(hass), HOST, KEY)
    got = []
    with pytest.raises(CannotConnect):  # Strom endet -> Neuverbindung im Koordinator
        async for ev in client.events():
            got.append(ev)
    assert got == [("status", {"volume": 10}), ("settings", {"changed": True})]
    assert aioclient_mock.mock_calls[0][3]["Authorization"] == f"Bearer {KEY}"


async def test_events_401(hass, aioclient_mock) -> None:
    aioclient_mock.get(f"{HOST}/api/events", status=401, json={"error": "API-Schlüssel ungültig"})
    client = LeuchtfeuerClient(async_get_clientsession(hass), HOST, KEY)
    with pytest.raises(InvalidAuth):
        async for _ in client.events():
            pass


async def test_request_errors(hass, aioclient_mock) -> None:
    aioclient_mock.post(f"{HOST}/api/action", status=403, json={"error": "read-only key"})
    aioclient_mock.get(f"{HOST}/api/status", exc=TimeoutError())
    aioclient_mock.post(f"{HOST}/api/announce", json={"ok": True})
    client = LeuchtfeuerClient(async_get_clientsession(hass), HOST, KEY)
    with pytest.raises(Forbidden, match="read-only"):
        await client.action("none")
    with pytest.raises(CannotConnect):
        await client.get_status()
    await client.announce(tone="bell", volume=40)
    assert aioclient_mock.mock_calls[-1][2] == {"tone": "bell", "volume": 40}
