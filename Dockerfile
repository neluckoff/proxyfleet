FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/proxyfleet ./cmd/proxyfleet

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && addgroup -S proxyfleet && adduser -S -G proxyfleet proxyfleet
COPY --from=builder /out/proxyfleet /usr/local/bin/proxyfleet
RUN mkdir -p /data && chown proxyfleet:proxyfleet /data
USER proxyfleet
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/proxyfleet"]
CMD ["server"]
