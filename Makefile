.PHONY: build test generate web

build:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" -o softwarr ./cmd/softwarr

test:
	go test ./...

generate:
	go tool sqlc generate

web:
	cd web && npm ci && npm run build
