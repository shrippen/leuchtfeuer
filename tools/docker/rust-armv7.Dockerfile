# Rust-Crossbuild für den Invoke: armv7 musl, statisch. C-Crosscompiler aus muslcc unter
# /opt/tc (nicht im PATH, damit Build-Skripte den Host-gcc nehmen).
FROM muslcc/x86_64:armv7l-linux-musleabihf AS tc
FROM rust:1-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends cmake clang pkg-config file && rm -rf /var/lib/apt/lists/* \
 && rustup target add armv7-unknown-linux-musleabihf
COPY --from=tc / /opt/tc
ENV CARGO_TARGET_ARMV7_UNKNOWN_LINUX_MUSLEABIHF_LINKER=/opt/tc/bin/gcc \
    CC_armv7_unknown_linux_musleabihf=/opt/tc/bin/gcc \
    AR_armv7_unknown_linux_musleabihf=/opt/tc/bin/ar \
    CFLAGS_armv7_unknown_linux_musleabihf="-march=armv7-a -mfpu=neon -mfloat-abi=hard"
