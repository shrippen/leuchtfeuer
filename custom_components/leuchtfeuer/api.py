"""Kleiner asynchroner Client für die JSON-API des Leuchtfeuer-Lautsprechers."""

from __future__ import annotations

import codecs
from collections.abc import AsyncIterator
import json
from typing import Any

import aiohttp
from yarl import URL

from .const import REQUEST_TIMEOUT, SSE_READ_TIMEOUT


class LeuchtfeuerError(Exception):
    """Allgemeiner Fehler der API."""


class CannotConnect(LeuchtfeuerError):
    """Lautsprecher nicht erreichbar."""


class InvalidAuth(LeuchtfeuerError):
    """API-Schlüssel ungültig (401)."""


class Forbidden(LeuchtfeuerError):
    """Mit diesem Schlüssel nicht erlaubt (403, z. B. Schlüssel nur zum Lesen)."""


def normalize_url(host: str) -> str:
    """Macht aus "invoke.lan", "192.168.1.23:8080" oder "https://x/" eine Basis-URL ohne Schrägstrich."""
    host = host.strip()
    if "://" not in host:
        host = f"http://{host}"
    url = URL(host)
    if url.scheme not in ("http", "https") or not url.host:
        raise ValueError(f"ungültige Adresse: {host}")
    # Pfad, Query und Fragment verwerfen; Standardport weglassen
    return str(url.origin()).rstrip("/")


def host_id(base_url: str) -> str:
    """Ersatzkennung aus der Adresse (wenn kein Zeroconf-id bekannt ist)."""
    url = URL(base_url)
    host = (url.host or "").lower()
    return host if url.is_default_port() else f"{host}:{url.port}"


class SSEParser:
    """Zerlegt einen Server-Sent-Events-Strom in (Ereignis, Daten)-Paare."""

    def __init__(self) -> None:
        self._buf = ""
        self._event = ""
        self._data: list[str] = []

    def feed(self, chunk: str) -> list[tuple[str, str]]:
        """Nimmt ein Stück Text an und liefert alle damit vollständigen Ereignisse."""
        self._buf += chunk.replace("\r\n", "\n").replace("\r", "\n")
        out: list[tuple[str, str]] = []
        while "\n" in self._buf:
            line, self._buf = self._buf.split("\n", 1)
            if line == "":
                # Leerzeile beendet ein Ereignis
                if self._data:
                    out.append((self._event or "message", "\n".join(self._data)))
                self._event, self._data = "", []
                continue
            if line.startswith(":"):
                continue  # Kommentar / Keepalive
            field, _, value = line.partition(":")
            value = value.removeprefix(" ")
            if field == "event":
                self._event = value
            elif field == "data":
                self._data.append(value)
        return out


class LeuchtfeuerClient:
    """Zugriff auf /api/* mit API-Schlüssel."""

    def __init__(self, session: aiohttp.ClientSession, base_url: str, api_key: str) -> None:
        self._session = session
        self.base_url = base_url.rstrip("/")
        self._headers = {"Authorization": f"Bearer {api_key}"}

    async def _request(self, method: str, path: str, body: dict[str, Any] | None = None) -> Any:
        try:
            async with self._session.request(
                method,
                f"{self.base_url}{path}",
                json=body,
                headers=self._headers,
                timeout=aiohttp.ClientTimeout(total=REQUEST_TIMEOUT),
            ) as resp:
                if resp.status == 401:
                    raise InvalidAuth(await _error_text(resp))
                if resp.status == 403:
                    raise Forbidden(await _error_text(resp))
                if resp.status >= 400:
                    raise LeuchtfeuerError(f"HTTP {resp.status}: {await _error_text(resp)}")
                return await resp.json(content_type=None)
        except (aiohttp.ClientError, TimeoutError) as err:
            raise CannotConnect(str(err) or type(err).__name__) from err
        except ValueError as err:
            raise LeuchtfeuerError(f"ungültige Antwort: {err}") from err

    async def get_status(self) -> dict[str, Any]:
        """GET /api/status."""
        return await self._request("GET", "/api/status")

    async def get_settings(self) -> dict[str, Any]:
        """GET /api/settings."""
        return await self._request("GET", "/api/settings")

    async def post(self, path: str, body: dict[str, Any] | None = None) -> Any:
        """POST mit JSON-Körper."""
        return await self._request("POST", path, body or {})

    async def action(self, action: str) -> None:
        """POST /api/action."""
        await self.post("/api/action", {"action": action})

    async def set_volume(self, volume: int) -> None:
        """Lautstärke 0-100."""
        await self.post("/api/volume", {"volume": max(0, min(100, int(volume)))})

    async def volume_delta(self, delta: int) -> None:
        """Lautstärke relativ ändern."""
        await self.post("/api/volume", {"delta": int(delta)})

    async def set_mute(self, muted: bool) -> None:
        """Stummschalten."""
        await self.post("/api/mute", {"muted": bool(muted)})

    async def radio_play(self, index: int) -> None:
        """Gespeicherten Sender abspielen."""
        await self.post("/api/radio/play", {"index": int(index)})

    async def radio_url(self, name: str, url: str) -> None:
        """Beliebigen Stream als Webradio abspielen."""
        await self.post("/api/radio/url", {"name": name, "url": url, "uuid": ""})

    async def radio_stop(self) -> None:
        """Player (und Briefing) anhalten."""
        await self.post("/api/radio/stop")

    async def announce(
        self,
        url: str | None = None,
        tone: str | None = None,
        volume: int | None = None,
        name: str | None = None,
    ) -> None:
        """Durchsage über der Musik."""
        body: dict[str, Any] = {}
        if url:
            body["url"] = url
        if tone:
            body["tone"] = tone
        if volume:
            body["volume"] = int(volume)
        if name:
            body["name"] = name
        await self.post("/api/announce", body)

    async def briefing_start(self) -> None:
        """Briefing starten."""
        await self.post("/api/briefing/start")

    async def events(self) -> AsyncIterator[tuple[str, Any]]:
        """Liest /api/events und liefert (Ereignis, JSON-Daten), bis die Verbindung endet."""
        timeout = aiohttp.ClientTimeout(total=None, connect=REQUEST_TIMEOUT, sock_read=SSE_READ_TIMEOUT)
        headers = {**self._headers, "Accept": "text/event-stream"}
        try:
            async with self._session.get(
                f"{self.base_url}/api/events", headers=headers, timeout=timeout
            ) as resp:
                if resp.status == 401:
                    raise InvalidAuth(await _error_text(resp))
                if resp.status >= 400:
                    raise LeuchtfeuerError(f"HTTP {resp.status}: {await _error_text(resp)}")
                parser = SSEParser()
                decoder = _Utf8Decoder()
                stream = resp.content
                while True:
                    chunk = await stream.readany()
                    if not chunk:
                        break
                    for event, data in parser.feed(decoder.decode(chunk)):
                        try:
                            yield event, json.loads(data)
                        except ValueError:
                            continue  # kaputter Rahmen: überspringen
        except (aiohttp.ClientError, TimeoutError) as err:
            raise CannotConnect(str(err) or type(err).__name__) from err
        raise CannotConnect("Ereignisstrom beendet")


class _Utf8Decoder:
    """UTF-8-Dekodierung über Stückgrenzen hinweg (Umlaute können geteilt ankommen)."""

    def __init__(self) -> None:
        self._dec = codecs.getincrementaldecoder("utf-8")(errors="replace")

    def decode(self, chunk: bytes) -> str:
        return self._dec.decode(chunk)


async def _error_text(resp: aiohttp.ClientResponse) -> str:
    """Fehlermeldung aus {"error": "..."} oder dem Text."""
    try:
        text = await resp.text()
    except (aiohttp.ClientError, TimeoutError, UnicodeDecodeError):
        return f"HTTP {resp.status}"
    try:
        data = json.loads(text)
    except ValueError:
        return text.strip()[:200] or f"HTTP {resp.status}"
    if isinstance(data, dict) and data.get("error"):
        return str(data["error"])
    return f"HTTP {resp.status}"
