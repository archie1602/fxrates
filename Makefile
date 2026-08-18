.PHONY: build run test test-integration fmt vet vuln check migrate-create migrate-up migrate-down migrate-version docker-up docker-down docker-logs docker-ps

build:
	go build -o bin/fxrates ./cmd/api

run:
	go run ./cmd/api

test:
	go test -short ./...

test-integration:
	@if [ -z "$$TEST_DATABASE_URL" ]; then \
		echo "TEST_DATABASE_URL is required"; \
		exit 1; \
	fi
	@database_path="$${TEST_DATABASE_URL%%\?*}"; \
	case "$$database_path" in \
		*_test) ;; \
		*) echo "TEST_DATABASE_URL must point to a database whose name ends with _test"; exit 1 ;; \
	esac
	@command -v migrate >/dev/null 2>&1 || { \
		echo "migrate CLI is required"; \
		exit 1; \
	}
	migrate -path migrations -database "$$TEST_DATABASE_URL" up
	go test -race -count=1 -run '^TestIntegration' ./internal/storage/postgres

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

check:
	go mod verify
	go mod tidy -diff
	npx --yes @redocly/cli@2.46.0 lint openapi.yaml --skip-rule info-license-strict --skip-rule no-server-example.com
	@unformatted="$$(git ls-files -z '*.go' | xargs -0 gofmt -l)"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	go test -short -race ./...
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

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
