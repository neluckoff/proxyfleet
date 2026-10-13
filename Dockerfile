FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/proxyfleet ./cmd/proxyfleet

FROM alpine:3.21 AS telemt
ARG TARGETARCH
ARG TELEMT_VERSION=3.5.14
RUN apk add --no-cache ca-certificates curl tar \
    && case "$TARGETARCH" in \
         amd64) archive="telemt-x86_64-linux-musl.tar.gz"; checksum="fc72921db5773954076cce1522e5e7fc5f8b7f696357697a97f144359f23fdd2" ;; \
         arm64) archive="telemt-aarch64-linux-musl.tar.gz"; checksum="21795430c4165cf964405459714d08eea319edaa5c9027beac4de373c356d84d" ;; \
         *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL --retry 3 -o /tmp/telemt.tar.gz "https://github.com/telemt/telemt/releases/download/${TELEMT_VERSION}/${archive}" \
    && echo "${checksum}  /tmp/telemt.tar.gz" | sha256sum -c - \
    && mkdir -p /out \
    && tar -xzf /tmp/telemt.tar.gz -C /out telemt \
    && curl -fsSL --retry 3 -o /out/TELEMT-LICENSE "https://raw.githubusercontent.com/telemt/telemt/${TELEMT_VERSION}/LICENSE"

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && addgroup -S proxyfleet && adduser -S -G proxyfleet proxyfleet
COPY --from=builder /out/proxyfleet /usr/local/bin/proxyfleet
COPY --from=telemt /out/telemt /usr/local/bin/telemt
COPY --from=telemt /out/TELEMT-LICENSE /usr/share/licenses/telemt/LICENSE
RUN mkdir -p /data && chown proxyfleet:proxyfleet /data
USER proxyfleet
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 CMD ["/usr/local/bin/proxyfleet", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/proxyfleet"]
CMD ["server"]
