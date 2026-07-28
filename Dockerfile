FROM --platform=$BUILDPLATFORM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce AS geodata

RUN set -eux; \
    mkdir /aster-config; \
    wget -qO /aster-config/geoip.metadb --header='Accept: application/octet-stream' \
      https://api.github.com/repos/MetaCubeX/meta-rules-dat/releases/assets/490783907; \
    echo 'a2b0deeed0a37613afb6e8da008c9dcfa58482a9d6bfff2295200c85641ecae9  /aster-config/geoip.metadb' | sha256sum -c -; \
    wget -qO /aster-config/geosite.dat --header='Accept: application/octet-stream' \
      https://api.github.com/repos/MetaCubeX/meta-rules-dat/releases/assets/490783923; \
    echo '4136378ccabdba1a3cedfd6fff98b66b78a8e9574c198db896a2ce3a24b950ba  /aster-config/geosite.dat' | sha256sum -c -; \
    wget -qO /aster-config/geoip.dat --header='Accept: application/octet-stream' \
      https://api.github.com/repos/MetaCubeX/meta-rules-dat/releases/assets/490783892; \
    echo 'cdf411fce977a1f48adb6a3b224e3e2bd7eccfcd4d6e2e30c6dc443f1a0e8e52  /aster-config/geoip.dat' | sha256sum -c -

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce AS builder
ARG TARGETPLATFORM

COPY docker/file-name.sh /aster/file-name.sh
WORKDIR /aster
COPY bin/ bin/
RUN set -eux; \
    sed -i 's/\r$//' file-name.sh bin/version.txt; \
    FILE_NAME="$(sh file-name.sh)"; \
    test -f "bin/${FILE_NAME}.gz"; \
    mv "bin/${FILE_NAME}.gz" aster-core.gz; \
    gzip -d aster-core.gz; \
    chmod 0755 aster-core

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
LABEL org.opencontainers.image.source="https://github.com/Miku0139oao/aster-core"

RUN apk add --no-cache ca-certificates tzdata iptables

VOLUME ["/root/.config/mihomo"]

COPY --from=geodata /aster-config/ /root/.config/mihomo/
COPY --from=builder /aster/aster-core /aster-core
ENTRYPOINT ["/aster-core"]
