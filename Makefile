.PHONY: help build fmt fmt-check vet test check up down logs migrate api worker probe

help:
	@echo "Targets: build fmt check up down logs migrate api worker probe"

build:
	go build -trimpath -o bin/api ./cmd/api
	go build -trimpath -o bin/worker ./cmd/worker
	go build -trimpath -o bin/admin ./cmd/admin

fmt:
	gofmt -w cmd internal

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (echo "Run make fmt"; exit 1)

vet:
	go vet ./...

test:
	go test -race -count=1 ./...

check: fmt-check vet test build

up:
	docker compose up --build -d --wait

down:
	docker compose down

logs:
	docker compose logs -f api worker

migrate:
	go run ./cmd/admin migrate

api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

probe:
	go run ./cmd/admin probe
