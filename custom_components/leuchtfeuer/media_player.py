"""Media-Player: Lautstärke, Sender, Streams und Durchsagen."""

from __future__ import annotations

from typing import Any

import voluptuous as vol

from homeassistant.components import media_source
from homeassistant.components.media_player import (
    ATTR_MEDIA_ANNOUNCE,
    ATTR_MEDIA_EXTRA,
    BrowseMedia,
    MediaPlayerEntity,
    MediaPlayerEntityFeature,
    MediaPlayerState,
    MediaType,
    async_process_play_media_url,
)
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ServiceValidationError
from homeassistant.helpers import config_validation as cv, entity_platform
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import (
    ACTIONS,
    ATTR_ACTION,
    ATTR_TONE,
    ATTR_URL,
    ATTR_VOLUME,
    SERVICE_ACTION,
    SERVICE_ANNOUNCE,
    SERVICE_BRIEFING,
    TONES,
)
from .coordinator import LeuchtfeuerConfigEntry, LeuchtfeuerCoordinator
from .entity import LeuchtfeuerEntity

PARALLEL_UPDATES = 0
VOLUME_STEP = 5

FEATURES = (
    MediaPlayerEntityFeature.VOLUME_SET
    | MediaPlayerEntityFeature.VOLUME_MUTE
    | MediaPlayerEntityFeature.VOLUME_STEP
    | MediaPlayerEntityFeature.STOP
    | MediaPlayerEntityFeature.SELECT_SOURCE
    | MediaPlayerEntityFeature.PLAY_MEDIA
    | MediaPlayerEntityFeature.BROWSE_MEDIA
    | MediaPlayerEntityFeature.MEDIA_ANNOUNCE
)

# Zustand des eigenen Players (player.state)
_PLAYER_STATE = {
    "playing": MediaPlayerState.PLAYING,
    "buffering": MediaPlayerState.BUFFERING,
    "reconnecting": MediaPlayerState.BUFFERING,
}


def active_source(status: dict[str, Any]) -> dict[str, Any] | None:
    """Die Quelle im Vordergrund (activeSource), sonst die erste spielende."""
    sources = [s for s in status.get("sources") or [] if isinstance(s, dict)]
    name = status.get("activeSource") or ""
    for src in sources:
        if name and src.get("name") == name:
            return src
    return sources[0] if sources else None


def media_state(status: dict[str, Any]) -> MediaPlayerState:
    """Bildet activeSource/sources/player auf einen Media-Player-Zustand ab."""
    states = {s.get("state") for s in status.get("sources") or [] if isinstance(s, dict)}
    if "playing" in states:
        return MediaPlayerState.PLAYING
    if "paused" in states:
        return MediaPlayerState.PAUSED
    player = status.get("player") or {}
    if player.get("kind") and player.get("state") in _PLAYER_STATE:
        return _PLAYER_STATE[player["state"]]
    return MediaPlayerState.IDLE


async def async_setup_entry(
    hass: HomeAssistant,
    entry: LeuchtfeuerConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Media-Player und Entity-Services anlegen."""
    async_add_entities([LeuchtfeuerMediaPlayer(entry.runtime_data)])

    platform = entity_platform.async_get_current_platform()
    platform.async_register_entity_service(
        SERVICE_ANNOUNCE,
        {
            vol.Optional(ATTR_URL): cv.string,
            vol.Optional(ATTR_TONE): vol.In(TONES),
            vol.Optional(ATTR_VOLUME): vol.All(vol.Coerce(int), vol.Range(min=1, max=100)),
        },
        "async_leuchtfeuer_announce",
    )
    platform.async_register_entity_service(
        SERVICE_ACTION,
        {vol.Required(ATTR_ACTION): vol.In(ACTIONS)},
        "async_leuchtfeuer_action",
    )
    platform.async_register_entity_service(SERVICE_BRIEFING, None, "async_leuchtfeuer_briefing")


class LeuchtfeuerMediaPlayer(LeuchtfeuerEntity, MediaPlayerEntity):
    """Der Lautsprecher als Media-Player."""

    _attr_name = None  # Gerätename
    _attr_supported_features = FEATURES
    _attr_media_content_type = MediaType.MUSIC
    _attr_volume_step = VOLUME_STEP / 100

    def __init__(self, coordinator: LeuchtfeuerCoordinator) -> None:
        super().__init__(coordinator, "media_player")

    @property
    def state(self) -> MediaPlayerState:
        return media_state(self.status)

    @property
    def volume_level(self) -> float | None:
        st = self.status
        if not st.get("volumeKnown", True) or st.get("volume") is None:
            return None
        return max(0, min(100, int(st["volume"]))) / 100

    @property
    def is_volume_muted(self) -> bool | None:
        return bool(self.status.get("muted")) if "muted" in self.status else None

    @property
    def source_list(self) -> list[str]:
        return list(self.coordinator.data.stations) if self.coordinator.data else []

    @property
    def source(self) -> str | None:
        st = self.status
        player = st.get("player") or {}
        src = active_source(st)
        name = src.get("name") if src else (player.get("kind") or None)
        if name == "radio" or (not name and player.get("kind") == "radio"):
            return player.get("name") or "radio"
        return name

    @property
    def media_title(self) -> str | None:
        src = active_source(self.status) or {}
        player = self.status.get("player") or {}
        if src.get("title"):
            return src["title"]
        if src.get("name") in (None, "radio", "alarm", "briefing") and player.get("kind"):
            return player.get("title") or player.get("name") or None
        return None

    @property
    def media_artist(self) -> str | None:
        src = active_source(self.status) or {}
        if src.get("artist"):
            return src["artist"]
        player = self.status.get("player") or {}
        # Webradio: Sendername als "Künstler", wenn der Titel aus dem Stream kommt
        if src.get("name") in (None, "radio") and player.get("kind") == "radio" and player.get("title"):
            return player.get("name") or None
        return None

    @property
    def media_album_name(self) -> str | None:
        return (active_source(self.status) or {}).get("album") or None

    # ---- Steuerung ----

    async def async_set_volume_level(self, volume: float) -> None:
        await self._call(self.coordinator.client.set_volume(round(volume * 100)))

    async def async_volume_up(self) -> None:
        await self._call(self.coordinator.client.volume_delta(VOLUME_STEP))

    async def async_volume_down(self) -> None:
        await self._call(self.coordinator.client.volume_delta(-VOLUME_STEP))

    async def async_mute_volume(self, mute: bool) -> None:
        await self._call(self.coordinator.client.set_mute(mute))

    async def async_media_stop(self) -> None:
        await self._call(self.coordinator.client.radio_stop())

    async def async_select_source(self, source: str) -> None:
        try:
            index = self.source_list.index(source)
        except ValueError as err:
            raise ServiceValidationError(f"Unbekannter Sender: {source}") from err
        await self._call(self.coordinator.client.radio_play(index))

    async def _resolve(self, media_id: str) -> str:
        """media-source:// auflösen und relative HA-Adressen absolut machen."""
        if media_source.is_media_source_id(media_id):
            item = await media_source.async_resolve_media(self.hass, media_id, self.entity_id)
            media_id = item.url
        return async_process_play_media_url(self.hass, media_id)

    async def async_play_media(self, media_type: MediaType | str, media_id: str, **kwargs: Any) -> None:
        """Durchsage (announce=True) oder Stream als Webradio."""
        url = await self._resolve(media_id)
        extra = kwargs.get(ATTR_MEDIA_EXTRA) or {}
        title = extra.get("title") or "Home Assistant"
        if kwargs.get(ATTR_MEDIA_ANNOUNCE):
            await self._call(self.coordinator.client.announce(url=url, name=title))
        else:
            await self._call(self.coordinator.client.radio_url(title, url))

    async def async_browse_media(
        self, media_content_type: MediaType | str | None = None, media_content_id: str | None = None
    ) -> BrowseMedia:
        """Medienquellen von Home Assistant, nur Audio."""
        return await media_source.async_browse_media(
            self.hass,
            media_content_id,
            content_filter=lambda item: item.media_content_type.startswith("audio/"),
        )

    # ---- Entity-Services ----

    async def async_leuchtfeuer_announce(
        self, url: str | None = None, tone: str | None = None, volume: int | None = None
    ) -> None:
        """leuchtfeuer.announce: Adresse/Medienquelle oder Ton."""
        if not url and not tone:
            raise ServiceValidationError("url oder tone angeben / give url or tone")
        target = await self._resolve(url) if url else None
        await self._call(self.coordinator.client.announce(url=target, tone=None if url else tone, volume=volume))

    async def async_leuchtfeuer_action(self, action: str) -> None:
        """leuchtfeuer.action."""
        await self._call(self.coordinator.client.action(action))

    async def async_leuchtfeuer_briefing(self) -> None:
        """leuchtfeuer.briefing."""
        await self._call(self.coordinator.client.briefing_start())
