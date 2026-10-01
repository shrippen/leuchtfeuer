"""Einrichtung: manuell (Adresse + API-Schlüssel), per Zeroconf oder Neu-Anmeldung."""

from __future__ import annotations

from collections.abc import Mapping
import logging
from typing import Any

import voluptuous as vol

from homeassistant.config_entries import ConfigFlow, ConfigFlowResult
from homeassistant.const import CONF_API_KEY, CONF_HOST, CONF_NAME
from homeassistant.core import HomeAssistant

from . import create_client
from .api import CannotConnect, Forbidden, InvalidAuth, LeuchtfeuerError, host_id, normalize_url
from .const import DOMAIN, TXT_ID, TXT_NAME, TXT_SCHEME, TXT_VERSION

try:  # ab HA 2025.1
    from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo
except ImportError:  # pragma: no cover
    from homeassistant.components.zeroconf import ZeroconfServiceInfo

_LOGGER = logging.getLogger(__name__)

KEY_SCHEMA = vol.Schema({vol.Required(CONF_API_KEY): str})


async def validate(hass: HomeAssistant, host: str, api_key: str) -> dict[str, Any]:
    """Prüft Adresse und Schlüssel: GET /api/status, dann die wirkungslose Aktion "none".

    Die Aktion schlägt mit 403 fehl, wenn der Schlüssel nur lesen darf.
    """
    client = create_client(hass, host, api_key)
    status = await client.get_status()
    await client.action("none")
    return status


def _error_key(err: Exception) -> str:
    if isinstance(err, InvalidAuth):
        return "invalid_auth"
    if isinstance(err, Forbidden):
        return "read_only_key"
    if isinstance(err, CannotConnect):
        return "cannot_connect"
    return "unknown"


class LeuchtfeuerConfigFlow(ConfigFlow, domain=DOMAIN):
    """Config-Flow für Leuchtfeuer."""

    VERSION = 1

    def __init__(self) -> None:
        self._host: str | None = None
        self._device_id: str | None = None
        self._name: str | None = None

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Manuelle Einrichtung."""
        errors: dict[str, str] = {}
        if user_input is not None:
            try:
                host = normalize_url(user_input[CONF_HOST])
            except ValueError:
                errors[CONF_HOST] = "invalid_host"
            else:
                api_key = user_input[CONF_API_KEY].strip()
                self._async_abort_entries_match({CONF_HOST: host})
                try:
                    status = await validate(self.hass, host, api_key)
                except LeuchtfeuerError as err:
                    errors["base"] = _error_key(err)
                except Exception:  # noqa: BLE001
                    _LOGGER.exception("Unerwarteter Fehler")
                    errors["base"] = "unknown"
                else:
                    # /api/status enthält keine Gerätekennung: Adresse als Ersatz
                    await self.async_set_unique_id(host_id(host))
                    self._abort_if_unique_id_configured(updates={CONF_HOST: host})
                    return self.async_create_entry(
                        title=status.get("name") or host_id(host),
                        data={CONF_HOST: host, CONF_API_KEY: api_key},
                    )
        return self.async_show_form(
            step_id="user",
            data_schema=self.add_suggested_values_to_schema(
                vol.Schema({vol.Required(CONF_HOST): str, vol.Required(CONF_API_KEY): str}),
                user_input,
            ),
            errors=errors,
        )

    async def async_step_zeroconf(self, discovery_info: ZeroconfServiceInfo) -> ConfigFlowResult:
        """Gefunden über _leuchtfeuer._tcp."""
        props = {str(k): str(v) for k, v in (discovery_info.properties or {}).items()}
        scheme = "https" if props.get(TXT_SCHEME) == "https" else "http"
        host = discovery_info.host
        if ":" in host:  # IPv6
            host = f"[{host}]"
        port = discovery_info.port
        default = 443 if scheme == "https" else 80
        url = f"{scheme}://{host}" if not port or port == default else f"{scheme}://{host}:{port}"
        url = normalize_url(url)
        device_id = (props.get(TXT_ID) or "").lower() or host_id(url)

        await self.async_set_unique_id(device_id)
        # Neue IP oder Schema übernehmen, wenn das Gerät schon eingerichtet ist
        self._abort_if_unique_id_configured(updates={CONF_HOST: url})
        # Schon manuell unter derselben Adresse eingerichtet (Ersatzkennung aus der Adresse)?
        for entry in self._async_current_entries(include_ignore=False):
            if entry.data.get(CONF_HOST) == url or entry.unique_id == host_id(url):
                return self.async_abort(reason="already_configured")

        self._host = url
        self._device_id = device_id
        self._name = props.get(TXT_NAME) or "Leuchtfeuer"
        self.context["title_placeholders"] = {CONF_NAME: self._name}
        self.context["configuration_url"] = url
        _LOGGER.debug("Leuchtfeuer gefunden: %s (%s, Version %s)", url, device_id, props.get(TXT_VERSION))
        return await self.async_step_zeroconf_confirm()

    async def async_step_zeroconf_confirm(
        self, user_input: dict[str, Any] | None = None
    ) -> ConfigFlowResult:
        """Nach der Erkennung nur noch den API-Schlüssel abfragen."""
        assert self._host is not None
        errors: dict[str, str] = {}
        if user_input is not None:
            api_key = user_input[CONF_API_KEY].strip()
            try:
                status = await validate(self.hass, self._host, api_key)
            except LeuchtfeuerError as err:
                errors["base"] = _error_key(err)
            except Exception:  # noqa: BLE001
                _LOGGER.exception("Unerwarteter Fehler")
                errors["base"] = "unknown"
            else:
                return self.async_create_entry(
                    title=status.get("name") or self._name or self._host,
                    data={CONF_HOST: self._host, CONF_API_KEY: api_key},
                )
        return self.async_show_form(
            step_id="zeroconf_confirm",
            data_schema=KEY_SCHEMA,
            errors=errors,
            description_placeholders={CONF_NAME: self._name or "", CONF_HOST: self._host},
        )

    async def async_step_reauth(self, entry_data: Mapping[str, Any]) -> ConfigFlowResult:
        """Schlüssel abgelehnt (401): neuen anfordern."""
        return await self.async_step_reauth_confirm()

    async def async_step_reauth_confirm(
        self, user_input: dict[str, Any] | None = None
    ) -> ConfigFlowResult:
        """Neuen API-Schlüssel eingeben."""
        entry = self._get_reauth_entry()
        host = entry.data[CONF_HOST]
        errors: dict[str, str] = {}
        if user_input is not None:
            api_key = user_input[CONF_API_KEY].strip()
            try:
                await validate(self.hass, host, api_key)
            except LeuchtfeuerError as err:
                errors["base"] = _error_key(err)
            except Exception:  # noqa: BLE001
                _LOGGER.exception("Unerwarteter Fehler")
                errors["base"] = "unknown"
            else:
                return self.async_update_reload_and_abort(entry, data_updates={CONF_API_KEY: api_key})
        return self.async_show_form(
            step_id="reauth_confirm",
            data_schema=KEY_SCHEMA,
            errors=errors,
            description_placeholders={CONF_NAME: entry.title, CONF_HOST: host},
        )
