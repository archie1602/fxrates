.PHONY: build run test fmt vet check migrate-create migrate-up migrate-down migrate-version docker-up docker-down docker-logs docker-ps

build:
	go build -o bin/fxrates ./cmd/api

run:
	go run ./cmd/api

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

check:
	go mod verify
	npx --yes @redocly/cli@2.46.0 lint openapi.yaml --skip-rule info-license-strict --skip-rule no-server-example.com
	@unformatted="$$(git ls-files -z '*.go' | xargs -0 gofmt -l)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	go test -race ./...

migrate-create:
	migrate create -ext sql -dir migrations -seq $(NAME)

migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down 1

migrate-version:
	migrate -path migrations -database "$(DATABASE_URL)" version

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f app

docker-ps:
	docker compose ps
