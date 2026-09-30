# Crosscompiler armv7 (musl, hardfloat) + make/perl für Invoke-Werkzeuge
FROM muslcc/x86_64:armv7l-linux-musleabihf AS tc
FROM alpine:3.24
RUN apk add --no-cache make perl file bash autoconf automake libtool pkgconf wget
COPY --from=tc / /opt/tc
ENV PATH=/opt/tc/bin:$PATH
