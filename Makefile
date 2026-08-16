.PHONY: test lint build dev
test:
	go test ./...
lint:
	golangci-lint run
build:
	go build -o bin/memoryd ./cmd/memoryd
	go build -o bin/memory ./cmd/memory
dev:
	docker compose -f deploy/compose/docker-compose.yml up --build
