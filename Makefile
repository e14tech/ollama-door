# Build a static ollama-door. VERSION is baked in with -ldflags.
VERSION ?= 1.0.2

.PHONY: build test fmt

build:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o ollama-door .

test:
	go test -race ./...

fmt:
	gofmt -w .
