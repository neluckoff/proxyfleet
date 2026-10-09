.PHONY: build test run docker-up docker-down

build:
	go build ./cmd/proxyfleet

test:
	go test ./...

run:
	go run ./cmd/proxyfleet server

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
