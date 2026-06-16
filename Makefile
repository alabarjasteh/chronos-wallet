.PHONY: run test tidy fmt docker-up

run:
	go run ./cmd/server

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal

docker-up:
	docker compose up --build
