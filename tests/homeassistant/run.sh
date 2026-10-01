#!/bin/sh
# Tests der Home-Assistant-Integration (custom_components/leuchtfeuer).
# Legt bei Bedarf ein venv an (Standard: ~/.cache/leuchtfeuer-ha-venv, änderbar mit HA_VENV) und startet pytest.
# Aufruf aus beliebigem Verzeichnis: tests/homeassistant/run.sh [pytest-Optionen]
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
VENV=${HA_VENV:-${XDG_CACHE_HOME:-$HOME/.cache}/leuchtfeuer-ha-venv}

if [ ! -x "$VENV/bin/python" ]; then
	PY=""
	for c in python3.13 python3.12; do
		if command -v "$c" >/dev/null 2>&1; then PY=$c; break; fi
	done
	if [ -z "$PY" ]; then
		echo "übersprungen: Python 3.12 oder 3.13 nicht gefunden (Home Assistant braucht >= 3.12)"
		exit 0
	fi
	echo "lege venv an: $VENV ($PY)"
	if command -v uv >/dev/null 2>&1; then
		uv venv -q -p "$(command -v "$PY")" "$VENV" &&
			VIRTUAL_ENV=$VENV uv pip install -q pytest-homeassistant-custom-component==0.13.316 || {
			echo "übersprungen: Installation fehlgeschlagen (Netz?)"; exit 0; }
	else
		"$PY" -m venv "$VENV" && "$VENV/bin/pip" install -q pytest-homeassistant-custom-component==0.13.316 || {
			echo "übersprungen: venv/pip nicht verfügbar oder Installation fehlgeschlagen"; exit 0; }
	fi
fi

cd "$HERE"
exec "$VENV/bin/python" -m pytest -c pytest.ini -q -p no:logging --timeout=30 "$@" .
