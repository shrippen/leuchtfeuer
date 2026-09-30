# Crossbuild gegen die glibc 2.23 des Invoke (Ubuntu 16.04 = glibc 2.23), armhf.
# Header aus Xenial-armhf-Paketen; gelinkt wird gegen die Bibliotheken des Geräts
# (Sysroot /sysroot, beim Build eingehängt), damit nur vorhandene Symbole benutzt werden.
FROM ubuntu:16.04
RUN sed -i 's/^deb /deb [arch=amd64] /' /etc/apt/sources.list \
 && echo 'deb [arch=armhf] http://ports.ubuntu.com/ubuntu-ports xenial main universe' > /etc/apt/sources.list.d/armhf.list \
 && echo 'deb [arch=armhf] http://ports.ubuntu.com/ubuntu-ports xenial-updates main universe' >> /etc/apt/sources.list.d/armhf.list \
 && dpkg --add-architecture armhf && apt-get update \
 && apt-get install -y --no-install-recommends gcc-arm-linux-gnueabihf g++-arm-linux-gnueabihf make automake autoconf libtool pkg-config file wget ca-certificates git xz-utils bzip2 \
    libglib2.0-dev:armhf libgstreamer1.0-dev:armhf libgstreamer-plugins-base1.0-dev:armhf libasound2-dev:armhf \
 && rm -rf /var/lib/apt/lists/*
