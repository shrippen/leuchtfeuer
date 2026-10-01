"""Schalter: Mikrofon des Sprachassistenten."""

from __future__ import annotations

from typing import Any

from homeassistant.components.switch import SwitchEntity
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .coordinator import LeuchtfeuerConfigEntry, LeuchtfeuerCoordinator
from .entity import LeuchtfeuerEntity

PARALLEL_UPDATES = 0


async def async_setup_entry(
    hass: HomeAssistant,
    entry: LeuchtfeuerConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Schalter anlegen."""
    async_add_entities([LeuchtfeuerMicSwitch(entry.runtime_data)])


class LeuchtfeuerMicSwitch(LeuchtfeuerEntity, SwitchEntity):
    """An = Mikrofon offen (voice.muted false); umschalten über die Aktion voice_mute."""

    _attr_translation_key = "voice_microphone"

    def __init__(self, coordinator: LeuchtfeuerCoordinator) -> None:
        super().__init__(coordinator, "voice_microphone")

    @property
    def is_on(self) -> bool | None:
        voice = self.status.get("voice")
        return None if voice is None else not voice.get("muted", False)

    async def _set(self, on: bool) -> None:
        # voice_mute schaltet um: nur senden, wenn der Zustand abweicht
        current = self.is_on
        if current is not None and current != on:
            await self._call(self.coordinator.client.action("voice_mute"))

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self._set(True)

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self._set(False)
