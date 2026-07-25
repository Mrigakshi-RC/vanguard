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

worker:
	go run ./cmd/worker

sqlc:
	sqlc generate

# ============================================================================
# PERFORMANCE & RATE LIMIT TESTING
# ============================================================================

.PHONY: load-test 

load-test:
	@echo "Validating environment and running performance test..."
	@go run scripts/loadtest.go

# ============================================================================
# KUBERNETES (MINIKUBE)
# ============================================================================

.PHONY: k8s-build k8s-deploy k8s-up

EDGE_IMAGE ?= vanguard-edge:latest
WORKER_IMAGE ?= vanguard-worker:latest
DOCKER_NCREDS ?= $(HOME)/.docker-nocreds

k8s-build:
	@mkdir -p $(DOCKER_NCREDS)
	@test -f $(DOCKER_NCREDS)/config.json || echo '{}' > $(DOCKER_NCREDS)/config.json
	DOCKER_CONFIG=$(DOCKER_NCREDS) docker build -f Dockerfile.edge -t $(EDGE_IMAGE) .
	DOCKER_CONFIG=$(DOCKER_NCREDS) docker build -f Dockerfile.worker -t $(WORKER_IMAGE) .
	minikube image load $(EDGE_IMAGE)
	minikube image load $(WORKER_IMAGE)

k8s-deploy:
	kubectl apply -f k8s/configmap.yaml
	kubectl apply -f k8s/redis/
	kubectl apply -f k8s/edge/
	kubectl apply -f k8s/worker/

k8s-up:
	docker compose up postgres -d
	$(MAKE) migrate-up
	$(MAKE) k8s-build
	$(MAKE) k8s-deploy