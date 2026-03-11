SHELL := /bin/bash

.PHONY: run test fmt build

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	gofmt -w $$(git ls-files --cached --others --exclude-standard '*.go')

build:
	go build ./cmd/server
