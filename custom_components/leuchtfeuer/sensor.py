"""Sensoren aus /api/status."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from typing import Any

from homeassistant.components.sensor import (
    SensorDeviceClass,
    SensorEntity,
    SensorEntityDescription,
    SensorStateClass,
)
from homeassistant.const import SIGNAL_STRENGTH_DECIBELS_MILLIWATT, EntityCategory, UnitOfTemperature
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.util import dt as dt_util

from .const import SOURCES, VOICE_STATES
from .coordinator import LeuchtfeuerConfigEntry, LeuchtfeuerCoordinator
from .entity import LeuchtfeuerEntity

PARALLEL_UPDATES = 0


def _temp(st: dict[str, Any]) -> float | None:
    t = (st.get("sys") or {}).get("tempC")
    return round(float(t), 1) if t else None


def _rssi(st: dict[str, Any]) -> int | None:
    r = (st.get("wifi") or {}).get("rssi")
    return int(r) if r else None  # 0 = unbekannt


def _next_alarm(st: dict[str, Any]) -> datetime | None:
    return dt_util.parse_datetime(st["nextAlarm"]) if st.get("nextAlarm") else None


def _active_source(st: dict[str, Any]) -> str | None:
    src = st.get("activeSource") or "none"
    return src if src in SOURCES or src == "none" else None


def _voice_state(st: dict[str, Any]) -> str | None:
    state = (st.get("voice") or {}).get("state") or "off"
    return state if state in VOICE_STATES else None


def _last_heard(st: dict[str, Any]) -> str | None:
    text = (st.get("voice") or {}).get("lastHeard") or None
    return text[:255] if text else None


@dataclass(frozen=True, kw_only=True)
class LeuchtfeuerSensorDescription(SensorEntityDescription):
    """Sensor mit Wertfunktion."""

    value_fn: Callable[[dict[str, Any]], Any]
    attrs_fn: Callable[[dict[str, Any]], dict[str, Any]] | None = None


SENSORS: tuple[LeuchtfeuerSensorDescription, ...] = (
    LeuchtfeuerSensorDescription(
        key="soc_temperature",
        translation_key="soc_temperature",
        device_class=SensorDeviceClass.TEMPERATURE,
        native_unit_of_measurement=UnitOfTemperature.CELSIUS,
        state_class=SensorStateClass.MEASUREMENT,
        value_fn=_temp,
    ),
    LeuchtfeuerSensorDescription(
        key="wifi_signal",
        translation_key="wifi_signal",
        device_class=SensorDeviceClass.SIGNAL_STRENGTH,
        native_unit_of_measurement=SIGNAL_STRENGTH_DECIBELS_MILLIWATT,
        state_class=SensorStateClass.MEASUREMENT,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=_rssi,
        attrs_fn=lambda st: {"ssid": (st.get("wifi") or {}).get("ssid")},
    ),
    LeuchtfeuerSensorDescription(
        key="next_alarm",
        translation_key="next_alarm",
        device_class=SensorDeviceClass.TIMESTAMP,
        value_fn=_next_alarm,
        attrs_fn=lambda st: {"name": st.get("nextAlarmName") or None},
    ),
    LeuchtfeuerSensorDescription(
        key="active_source",
        translation_key="active_source",
        device_class=SensorDeviceClass.ENUM,
        options=["none", *SOURCES],
        value_fn=_active_source,
    ),
    LeuchtfeuerSensorDescription(
        key="voice_state",
        translation_key="voice_state",
        device_class=SensorDeviceClass.ENUM,
        options=VOICE_STATES,
        value_fn=_voice_state,
    ),
    LeuchtfeuerSensorDescription(
        key="last_heard",
        translation_key="last_heard",
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=_last_heard,
        attrs_fn=lambda st: {"answer": (st.get("voice") or {}).get("lastAnswer") or None},
    ),
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: LeuchtfeuerConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Sensoren anlegen."""
    async_add_entities(LeuchtfeuerSensor(entry.runtime_data, d) for d in SENSORS)


class LeuchtfeuerSensor(LeuchtfeuerEntity, SensorEntity):
    """Ein Wert aus dem Status."""

    entity_description: LeuchtfeuerSensorDescription

    def __init__(self, coordinator: LeuchtfeuerCoordinator, description: LeuchtfeuerSensorDescription) -> None:
        super().__init__(coordinator, description.key)
        self.entity_description = description

    @property
    def native_value(self) -> Any:
        return self.entity_description.value_fn(self.status)

    @property
    def extra_state_attributes(self) -> dict[str, Any] | None:
        fn = self.entity_description.attrs_fn
        return fn(self.status) if fn else None
