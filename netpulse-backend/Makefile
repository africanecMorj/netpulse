.PHONY: run agent fmt test build
run:
	go run ./cmd/api
agent:
	go run ./cmd/agent
build:
	go build -o bin/netpulse-api ./cmd/api
	go build -o bin/netpulse-agent ./cmd/agent
fmt:
	gofmt -w ./cmd ./internal
test:
	go test ./...
