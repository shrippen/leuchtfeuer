# Wie xenial-armhf, zusätzlich dbus- und libsbc-Header für BlueZ und bluez-alsa.
FROM invoke-xenial-armhf
RUN apt-get update && apt-get install -y --no-install-recommends libdbus-1-dev:armhf libdbus-1-3:armhf libsbc-dev:armhf \
 && rm -rf /var/lib/apt/lists/*
