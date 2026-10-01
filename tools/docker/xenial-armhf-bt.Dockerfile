# Wie xenial-armhf, zusätzlich dbus-, libsbc-Header (BlueZ, bluez-alsa) und popt/libconfig/openssl (shairport-sync).
FROM invoke-xenial-armhf
RUN apt-get update && apt-get install -y --no-install-recommends libdbus-1-dev:armhf libdbus-1-3:armhf libsbc-dev:armhf libpopt-dev:armhf libconfig-dev:armhf libssl-dev:armhf \
 && rm -rf /var/lib/apt/lists/*
