.PHONY: test run-api run-worker tidy up down

test:
	go test ./...

tidy:
	go mod tidy

up:
	docker compose up -d --build
	@echo "Console  http://localhost:8080  (token: dev-admin)"

down:
	docker compose down

run-api:
	MOCK_DEPENDENCIES=true HTTP_ADDR=:8080 go run ./cmd/api

run-worker:
	MOCK_DEPENDENCIES=true go run ./cmd/worker
