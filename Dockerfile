FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/proxyfleet ./cmd/proxyfleet

FROM golang:1.26-alpine AS tgwebproxy
ARG TGWEBPROXY_COMMIT=44aa88ac176ee89a9bf8cdf1b76227d8c653f2a1
ARG TGWEBPROXY_SHA256=b03fe9aee0e843895f5849db9bca547c4609fc05d52fa3ee13a1aca84d94959b
RUN apk add --no-cache ca-certificates curl tar \
    && curl -fsSL --retry 3 -o /tmp/tgwebproxy.tar.gz "https://codeload.github.com/kesevone/tgwebproxy/tar.gz/${TGWEBPROXY_COMMIT}" \
    && echo "${TGWEBPROXY_SHA256}  /tmp/tgwebproxy.tar.gz" | sha256sum -c - \
    && mkdir -p /src /out \
    && tar -xzf /tmp/tgwebproxy.tar.gz -C /src --strip-components=1
WORKDIR /src
RUN go mod download \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tgwebproxy ./cmd/tgwebproxy \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tgwebproxy-probe ./cmd/tgwebproxy-probe \
    && cp LICENSE /out/TGWEBPROXY-LICENSE

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && addgroup -S proxyfleet && adduser -S -G proxyfleet proxyfleet
COPY --from=builder /out/proxyfleet /usr/local/bin/proxyfleet
COPY --from=tgwebproxy /out/tgwebproxy /usr/local/bin/tgwebproxy
COPY --from=tgwebproxy /out/tgwebproxy-probe /usr/local/bin/tgwebproxy-probe
COPY --from=tgwebproxy /out/TGWEBPROXY-LICENSE /usr/share/licenses/tgwebproxy/LICENSE
RUN mkdir -p /data && chown proxyfleet:proxyfleet /data
USER proxyfleet
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 CMD ["/usr/local/bin/proxyfleet", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/proxyfleet"]
CMD ["server"]
