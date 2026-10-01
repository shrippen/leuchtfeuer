"""Leuchtfeuer: Harman Kardon Invoke mit eigener Firmware als Home-Assistant-Integration."""

from __future__ import annotations

from homeassistant.const import CONF_API_KEY, CONF_HOST, Platform
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import LeuchtfeuerClient
from .coordinator import LeuchtfeuerConfigEntry, LeuchtfeuerCoordinator

PLATFORMS: list[Platform] = [
    Platform.BUTTON,
    Platform.MEDIA_PLAYER,
    Platform.SENSOR,
    Platform.SWITCH,
]


def create_client(hass: HomeAssistant, host: str, api_key: str) -> LeuchtfeuerClient:
    """Client mit gemeinsamer Sitzung; bei https ohne Zertifikatsprüfung (selbstsigniert)."""
    session = async_get_clientsession(hass, verify_ssl=not host.startswith("https://"))
    return LeuchtfeuerClient(session, host, api_key)


async def async_setup_entry(hass: HomeAssistant, entry: LeuchtfeuerConfigEntry) -> bool:
    """Eintrag einrichten: Status holen, Ereignisstrom starten, Plattformen laden."""
    client = create_client(hass, entry.data[CONF_HOST], entry.data[CONF_API_KEY])
    coordinator = LeuchtfeuerCoordinator(hass, entry, client)
    await coordinator.async_config_entry_first_refresh()
    entry.runtime_data = coordinator
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    coordinator.start_push()
    return True


async def async_unload_entry(hass: HomeAssistant, entry: LeuchtfeuerConfigEntry) -> bool:
    """Eintrag entladen; der Hintergrund-Task endet mit dem Eintrag."""
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
