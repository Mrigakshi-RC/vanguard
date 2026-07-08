.PHONY: build run test tidy

BINARY := vanguard
MAIN   := ./cmd/vanguard
GOOSE ?= goose
DB    ?= user=vanguard password=vanguard dbname=vanguard host=localhost port=5432 sslmode=disable

up:
	docker compose up -d

down:
	docker compose down
	
build:
	go build -o bin/$(BINARY) $(MAIN)

run: build
	./bin/$(BINARY)

test:
	go test ./...

tidy:
	go mod tidy

migrate-up:
	$(GOOSE) -dir db/migrations postgres "$(DB)" up
migrate-down:
	$(GOOSE) -dir db/migrations postgres "$(DB)" down
migrate-status:
	$(GOOSE) -dir db/migrations postgres "$(DB)" status