"""Gemeinsame Fixtures für die Tests der Leuchtfeuer-Integration.

Ausführen (aus dem Repo-Wurzelverzeichnis):
    tests/homeassistant/run.sh
oder direkt:
    tests/homeassistant/run.sh
"""

from __future__ import annotations

import copy
from pathlib import Path
import sys
from typing import Any

import pytest

# custom_components/ liegt im Repo-Wurzelverzeichnis
ROOT = Path(__file__).resolve().parents[2]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from pytest_homeassistant_custom_component.common import MockConfigEntry  # noqa: E402

from homeassistant.const import CONF_API_KEY, CONF_HOST  # noqa: E402

from lf_testdata import DEVICE_ID, HOST, KEY, STATUS  # noqa: E402


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations):
    """custom_components/ für alle Tests freigeben."""
    yield


@pytest.fixture
def status() -> dict[str, Any]:
    return copy.deepcopy(STATUS)


@pytest.fixture
def mock_entry() -> MockConfigEntry:
    return MockConfigEntry(
        domain="leuchtfeuer",
        title="Küche",
        unique_id=DEVICE_ID,
        data={CONF_HOST: HOST, CONF_API_KEY: KEY},
    )
