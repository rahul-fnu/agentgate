.PHONY: build test lint format

build:
	go build -o agentgate ./cmd/agentgate

test:
	go test ./... -v

lint:
	gofmt -l ./cmd ./internal

format:
	gofmt -w ./cmd ./internal
