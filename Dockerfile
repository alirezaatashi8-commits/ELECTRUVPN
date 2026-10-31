# syntax=docker/dockerfile:1
# ELECTRUVPN panel image. Pinned to the same Go toolchain as install.sh.

FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY . .
ENV GOTOOLCHAIN=local GOFLAGS=-trimpath
RUN go mod tidy \
 && CGO_ENABLED=1 go build -mod=readonly -o /out/electruvpn-server ./cmd/server
RUN sh docker/fetch-vendor.sh /src/web/assets/vendor

FROM debian:bookworm-slim AS cores
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends curl unzip ca-certificates \
 && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    case "${TARGETARCH:-amd64}" in \
      amd64) XRAY_ZIP=Xray-linux-64.zip; SB_ARCH=amd64 ;; \
      arm64) XRAY_ZIP=Xray-linux-arm64-v8a.zip; SB_ARCH=arm64 ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --retry 5 -o /tmp/xray.zip "https://github.com/XTLS/Xray-core/releases/latest/download/${XRAY_ZIP}"; \
    mkdir -p /out/bin /out/share/xray; \
    unzip -q /tmp/xray.zip -d /tmp/xray; \
    install -m 0755 /tmp/xray/xray /out/bin/xray; \
    install -m 0644 /tmp/xray/geoip.dat /tmp/xray/geosite.dat /out/share/xray/; \
    SB_TAG="$(curl -fsSIL -o /dev/null -w '%{url_effective}' https://github.com/SagerNet/sing-box/releases/latest | sed 's#.*/tag/##')"; \
    curl -fsSL --retry 5 -o /tmp/sb.tgz "https://github.com/SagerNet/sing-box/releases/download/${SB_TAG}/sing-box-${SB_TAG#v}-linux-${SB_ARCH}.tar.gz"; \
    mkdir -p /tmp/sb; tar -xzf /tmp/sb.tgz -C /tmp/sb; \
    install -m 0755 "$(find /tmp/sb -type f -name sing-box | head -n1)" /out/bin/sing-box

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl iproute2 wireguard-tools tzdata tini \
 && rm -rf /var/lib/apt/lists/*
COPY --from=cores /out/bin/xray /usr/local/bin/xray
COPY --from=cores /out/bin/sing-box /usr/local/bin/sing-box
COPY --from=cores /out/share/xray /usr/local/share/xray
WORKDIR /opt/electruvpn
COPY --from=build /out/electruvpn-server /opt/electruvpn/electruvpn-server
COPY --from=build /src/web /opt/electruvpn/web
COPY scripts/geo-update.sh /opt/electruvpn/geo-update.sh
COPY docker/entrypoint.sh /usr/local/bin/electruvpn-entrypoint
ENV GIN_MODE=release \
    ELECTRU_PANEL_PORT=2087 \
    ELECTRU_PANEL_BIND_ADDR=0.0.0.0 \
    ELECTRU_DB_PATH=/data/electruvpn.db \
    ELECTRU_XRAY_CONFIG=/data/xray-config.json \
    ELECTRU_SINGBOX_CONFIG=/data/sing-box.json \
    ELECTRU_XRAY_BIN=/usr/local/bin/xray \
    ELECTRU_SINGBOX_BIN=/usr/local/bin/sing-box \
    ELECTRU_GEO_DIR=/usr/local/share/xray \
    ELECTRU_LOG_FORMAT=json
VOLUME ["/data"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/electruvpn-entrypoint"]
CMD ["/opt/electruvpn/electruvpn-server"]
