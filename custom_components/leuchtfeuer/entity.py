"""Gemeinsame Basis der Leuchtfeuer-Entitäten."""

from __future__ import annotations

from collections.abc import Awaitable
from typing import Any

from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .api import LeuchtfeuerError
from .const import DOMAIN, MANUFACTURER, MODEL
from .coordinator import LeuchtfeuerCoordinator


class LeuchtfeuerEntity(CoordinatorEntity[LeuchtfeuerCoordinator]):
    """Ein Gerät je Lautsprecher, Name/Version aus dem Status."""

    _attr_has_entity_name = True

    def __init__(self, coordinator: LeuchtfeuerCoordinator, key: str) -> None:
        super().__init__(coordinator)
        entry = coordinator.config_entry
        uid = entry.unique_id or entry.entry_id
        self._attr_unique_id = f"{uid}_{key}"
        status = coordinator.data.status if coordinator.data else {}
        self._attr_device_info = DeviceInfo(
            identifiers={(DOMAIN, uid)},
            name=status.get("name") or entry.title,
            manufacturer=MANUFACTURER,
            model=MODEL,
            sw_version=status.get("version") or None,
            configuration_url=coordinator.client.base_url,
        )

    @property
    def status(self) -> dict[str, Any]:
        """Letzter Status (leer, solange keiner da ist)."""
        return self.coordinator.data.status if self.coordinator.data else {}

    async def _call(self, aw: Awaitable[Any]) -> None:
        """API-Aufruf; Fehler als HomeAssistantError melden."""
        try:
            await aw
        except LeuchtfeuerError as err:
            raise HomeAssistantError(f"Leuchtfeuer: {err}") from err
