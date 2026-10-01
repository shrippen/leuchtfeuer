"""Push-Koordinator: hält den Status aus dem Ereignisstrom /api/events aktuell."""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
import logging
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ConfigEntryAuthFailed
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .api import InvalidAuth, LeuchtfeuerClient, LeuchtfeuerError
from .const import BACKOFF_MAX, BACKOFF_MIN, DOMAIN

_LOGGER = logging.getLogger(__name__)

type LeuchtfeuerConfigEntry = ConfigEntry[LeuchtfeuerCoordinator]


@dataclass
class LeuchtfeuerData:
    """Letzter bekannter Zustand des Lautsprechers."""

    status: dict[str, Any] = field(default_factory=dict)
    stations: list[str] = field(default_factory=list)


def stations_from_settings(settings: dict[str, Any]) -> list[str]:
    """Sendernamen aus GET /api/settings (settings.radio[].name)."""
    radio = (settings.get("settings") or {}).get("radio") or []
    return [str(s.get("name") or s.get("url") or f"#{i + 1}") for i, s in enumerate(radio) if isinstance(s, dict)]


class LeuchtfeuerCoordinator(DataUpdateCoordinator[LeuchtfeuerData]):
    """Erster Abruf per GET, danach nur noch Push über Server-Sent Events."""

    config_entry: LeuchtfeuerConfigEntry

    def __init__(
        self, hass: HomeAssistant, entry: LeuchtfeuerConfigEntry, client: LeuchtfeuerClient
    ) -> None:
        super().__init__(
            hass,
            _LOGGER,
            config_entry=entry,
            name=DOMAIN,
            update_interval=None,  # kein Polling, der Lautsprecher schiebt
        )
        self.client = client
        self._sse_task: asyncio.Task | None = None

    async def _async_update_data(self) -> LeuchtfeuerData:
        """Vollständiger Abruf (beim Start)."""
        try:
            status = await self.client.get_status()
            settings = await self.client.get_settings()
        except InvalidAuth as err:
            raise ConfigEntryAuthFailed(str(err)) from err
        except LeuchtfeuerError as err:
            raise UpdateFailed(str(err)) from err
        return LeuchtfeuerData(status=status, stations=stations_from_settings(settings))

    def start_push(self) -> None:
        """Ereignisstrom im Hintergrund starten (endet mit dem Config-Entry)."""
        self._sse_task = self.config_entry.async_create_background_task(
            self.hass, self._async_sse_loop(), f"{DOMAIN}_events_{self.config_entry.entry_id}"
        )

    async def _async_refresh_stations(self) -> None:
        try:
            settings = await self.client.get_settings()
        except LeuchtfeuerError as err:
            _LOGGER.debug("Senderliste nicht lesbar: %s", err)
            return
        data = self.data or LeuchtfeuerData()
        self.async_set_updated_data(LeuchtfeuerData(status=data.status, stations=stations_from_settings(settings)))

    async def _async_sse_loop(self) -> None:
        """Liest /api/events; bei Abbruch Entitäten unverfügbar, Neuversuch mit 2 → 60 s."""
        backoff = BACKOFF_MIN
        reconnect = False
        while True:
            try:
                async for event, payload in self.client.events():
                    backoff = BACKOFF_MIN
                    if event == "status" and isinstance(payload, dict):
                        stations = self.data.stations if self.data else []
                        self._async_sync_device(payload)
                        self.async_set_updated_data(LeuchtfeuerData(status=payload, stations=stations))
                        if reconnect:
                            # Senderliste kann sich während der Trennung geändert haben
                            reconnect = False
                            await self._async_refresh_stations()
                    elif event == "settings":
                        await self._async_refresh_stations()
            except InvalidAuth as err:
                _LOGGER.warning("Leuchtfeuer %s: API-Schlüssel abgelehnt", self.client.base_url)
                self.async_set_update_error(err)
                self.config_entry.async_start_reauth(self.hass)
                return
            except LeuchtfeuerError as err:
                if self.last_update_success:
                    _LOGGER.info("Leuchtfeuer %s getrennt: %s", self.client.base_url, err)
                self.async_set_update_error(err)
            reconnect = True
            await asyncio.sleep(backoff)
            backoff = min(backoff * 2, BACKOFF_MAX)

    def _async_sync_device(self, status: dict[str, Any]) -> None:
        """Name/Version im Geräteregister nachführen (z. B. nach einem Update)."""
        old = self.data.status if self.data else {}
        if status.get("version") == old.get("version") and status.get("name") == old.get("name"):
            return
        uid = self.config_entry.unique_id or self.config_entry.entry_id
        reg = dr.async_get(self.hass)
        if device := reg.async_get_device(identifiers={(DOMAIN, uid)}):
            reg.async_update_device(device.id, sw_version=status.get("version") or None)
