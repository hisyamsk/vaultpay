.PHONY: migrate-up migrate-down migrate-version

migrate-up:
	migrate -database "$(DATABASE_URL)" -path db/migrations up

migrate-down:
	migrate -database "$(DATABASE_URL)" -path db/migrations down 1

migrate-version:
	migrate -database "$(DATABASE_URL)" -path db/migrations version
