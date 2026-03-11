SHELL := /bin/bash

.PHONY: run test fmt build build-prod

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	gofmt -w $$(git ls-files --cached --others --exclude-standard '*.go')

build:
	go build ./cmd/server

build-prod:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/pdf-cv ./cmd/server
