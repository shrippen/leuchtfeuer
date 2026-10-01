"""Tests für den Config-Flow.

Ausführen (Repo-Wurzel):
    tests/homeassistant/run.sh
oder direkt:
    tests/homeassistant/run.sh
"""

from __future__ import annotations

from ipaddress import ip_address
from unittest.mock import patch

import pytest

from homeassistant import config_entries
from homeassistant.const import CONF_API_KEY, CONF_HOST
from homeassistant.core import HomeAssistant
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from custom_components.leuchtfeuer.const import DOMAIN

from lf_testdata import DEVICE_ID, KEY, STATUS

SETUP = "custom_components.leuchtfeuer.async_setup_entry"


def _zc(ip: str = "192.168.1.23", scheme: str = "http", port: int = 80) -> ZeroconfServiceInfo:
    return ZeroconfServiceInfo(
        ip_address=ip_address(ip),
        ip_addresses=[ip_address(ip)],
        hostname="leuchtfeuer-aabbccddeeff.local.",
        name="Leuchtfeuer-aabbccddeeff._leuchtfeuer._tcp.local.",
        port=port,
        type="_leuchtfeuer._tcp.local.",
        properties={"name": "Küche", "version": "2.4.0", "id": DEVICE_ID, "scheme": scheme},
    )


def _mock_ok(aioclient_mock, base: str) -> None:
    aioclient_mock.get(f"{base}/api/status", json=STATUS)
    aioclient_mock.post(f"{base}/api/action", json={"ok": True})


async def test_user_success(hass: HomeAssistant, aioclient_mock) -> None:
    _mock_ok(aioclient_mock, "http://invoke.lan")
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": config_entries.SOURCE_USER})
    assert result["type"] is FlowResultType.FORM
    with patch(SETUP, return_value=True):
        result = await hass.config_entries.flow.async_configure(
            result["flow_id"], {CONF_HOST: "invoke.lan/", CONF_API_KEY: f" {KEY} "}
        )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["title"] == "Küche"
    assert result["data"] == {CONF_HOST: "http://invoke.lan", CONF_API_KEY: KEY}
    assert result["result"].unique_id == "invoke.lan"
    # Bearer-Schlüssel wurde mitgeschickt; "none" prüft Schreibrecht
    assert aioclient_mock.mock_calls[0][3]["Authorization"] == f"Bearer {KEY}"
    assert aioclient_mock.mock_calls[1][2] == {"action": "none"}


@pytest.mark.parametrize(
    ("kwargs", "error"),
    [
        ({"status": 401, "json": {"error": "API-Schlüssel ungültig"}}, "invalid_auth"),
        ({"exc": TimeoutError()}, "cannot_connect"),
    ],
)
async def test_user_errors(hass: HomeAssistant, aioclient_mock, kwargs, error) -> None:
    aioclient_mock.get("http://invoke.lan/api/status", **kwargs)
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": config_entries.SOURCE_USER})
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_HOST: "invoke.lan", CONF_API_KEY: "lf_x"}
    )
    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": error}

    # danach klappt es
    aioclient_mock.clear_requests()
    _mock_ok(aioclient_mock, "http://invoke.lan")
    with patch(SETUP, return_value=True):
        result = await hass.config_entries.flow.async_configure(
            result["flow_id"], {CONF_HOST: "invoke.lan", CONF_API_KEY: KEY}
        )
    assert result["type"] is FlowResultType.CREATE_ENTRY


async def test_user_read_only_key(hass: HomeAssistant, aioclient_mock) -> None:
    aioclient_mock.get("http://invoke.lan/api/status", json=STATUS)
    aioclient_mock.post("http://invoke.lan/api/action", status=403, json={"error": "read-only key"})
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": config_entries.SOURCE_USER})
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_HOST: "invoke.lan", CONF_API_KEY: KEY}
    )
    assert result["errors"] == {"base": "read_only_key"}


async def test_user_already_configured(hass: HomeAssistant, aioclient_mock, mock_entry) -> None:
    mock_entry.add_to_hass(hass)
    result = await hass.config_entries.flow.async_init(DOMAIN, context={"source": config_entries.SOURCE_USER})
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_HOST: "http://invoke.lan", CONF_API_KEY: KEY}
    )
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"


async def test_zeroconf(hass: HomeAssistant, aioclient_mock) -> None:
    _mock_ok(aioclient_mock, "https://192.168.1.23")
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_ZEROCONF}, data=_zc(scheme="https", port=443)
    )
    assert result["type"] is FlowResultType.FORM
    assert result["step_id"] == "zeroconf_confirm"
    assert result["description_placeholders"] == {"name": "Küche", "host": "https://192.168.1.23"}
    with patch(SETUP, return_value=True):
        result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_API_KEY: KEY})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["data"][CONF_HOST] == "https://192.168.1.23"
    assert result["result"].unique_id == DEVICE_ID


async def test_zeroconf_updates_host(hass: HomeAssistant, mock_entry) -> None:
    mock_entry.add_to_hass(hass)
    with patch(SETUP, return_value=True):
        result = await hass.config_entries.flow.async_init(
            DOMAIN, context={"source": config_entries.SOURCE_ZEROCONF}, data=_zc("192.168.1.50", port=8080)
        )
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"
    assert mock_entry.data[CONF_HOST] == "http://192.168.1.50:8080"


async def test_zeroconf_matches_manual_entry(hass: HomeAssistant) -> None:
    from pytest_homeassistant_custom_component.common import MockConfigEntry

    MockConfigEntry(
        domain=DOMAIN, unique_id="192.168.1.23", data={CONF_HOST: "http://192.168.1.23", CONF_API_KEY: KEY}
    ).add_to_hass(hass)
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_ZEROCONF}, data=_zc()
    )
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"


async def test_reauth(hass: HomeAssistant, aioclient_mock, mock_entry) -> None:
    mock_entry.add_to_hass(hass)
    _mock_ok(aioclient_mock, "http://invoke.lan")
    result = await mock_entry.start_reauth_flow(hass)
    assert result["step_id"] == "reauth_confirm"
    with patch(SETUP, return_value=True):
        result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_API_KEY: "lf_new"})
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "reauth_successful"
    assert mock_entry.data[CONF_API_KEY] == "lf_new"
