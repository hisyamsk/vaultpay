.PHONY: migrate-up migrate-down migrate-version docker-build docker-build-dev compose-up compose-down compose-logs compose-dev-up compose-dev-down compose-dev-logs

IMAGE_NAME ?= vaultpay-api
IMAGE_TAG ?= local
GO_VERSION ?= 1.25
DELVE_VERSION ?= v1.26.3

docker-build:
	docker build --build-arg GO_VERSION=$(GO_VERSION) -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-build-dev:
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg DELVE_VERSION=$(DELVE_VERSION) -f Dockerfile.dev -t $(IMAGE_NAME):dev .

compose-up:
	docker compose up --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f api

compose-dev-up:
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build

compose-dev-down:
	docker compose -f docker-compose.yml -f docker-compose.dev.yml down

migrate-up:
	migrate -database "$(DATABASE_URL)" -path db/migrations up

migrate-down:
	migrate -database "$(DATABASE_URL)" -path db/migrations down 1

migrate-version:
	migrate -database "$(DATABASE_URL)" -path db/migrations version
