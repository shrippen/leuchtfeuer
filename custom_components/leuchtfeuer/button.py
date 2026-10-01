"""Tasten: Briefing, Zuhören, Alles stoppen, Gong (POST /api/action)."""

from __future__ import annotations

from dataclasses import dataclass

from homeassistant.components.button import ButtonEntity, ButtonEntityDescription
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .coordinator import LeuchtfeuerConfigEntry, LeuchtfeuerCoordinator
from .entity import LeuchtfeuerEntity

PARALLEL_UPDATES = 0


@dataclass(frozen=True, kw_only=True)
class LeuchtfeuerButtonDescription(ButtonEntityDescription):
    """Taste mit zugehöriger Aktion."""

    action: str


BUTTONS: tuple[LeuchtfeuerButtonDescription, ...] = (
    LeuchtfeuerButtonDescription(key="briefing", translation_key="briefing", action="briefing"),
    LeuchtfeuerButtonDescription(key="voice", translation_key="voice", action="voice"),
    LeuchtfeuerButtonDescription(key="stop_all", translation_key="stop_all", action="stop_all"),
    LeuchtfeuerButtonDescription(key="chime", translation_key="chime", action="chime"),
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: LeuchtfeuerConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Tasten anlegen."""
    async_add_entities(LeuchtfeuerButton(entry.runtime_data, d) for d in BUTTONS)


class LeuchtfeuerButton(LeuchtfeuerEntity, ButtonEntity):
    """Eine Aktions-Taste."""

    entity_description: LeuchtfeuerButtonDescription

    def __init__(self, coordinator: LeuchtfeuerCoordinator, description: LeuchtfeuerButtonDescription) -> None:
        super().__init__(coordinator, description.key)
        self.entity_description = description

    async def async_press(self) -> None:
        await self._call(self.coordinator.client.action(self.entity_description.action))
