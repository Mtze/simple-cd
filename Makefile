# Common tasks. Run `make help` for a list.
DEV_DB := postgres://simplecd:simplecd@127.0.0.1:5434/simplecd?sslmode=disable
# Use BASE_URL=http://127.0.0.1:5173 when working with the Vite dev server.
BASE_URL ?= http://127.0.0.1:8080
KIND_VERSION := v0.33.0
KIND := $(CURDIR)/bin/kind

.PHONY: help build web test test-go test-web lint dev-up dev-down dev-seed run e2e-up e2e e2e-down

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

web: ## Build the UI into web/dist
	cd web && npm ci && npm run build

build: ## Build both binaries into bin/ (build the UI first to embed it)
	CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...

test: test-go test-web ## Run all tests

test-go: ## Go tests; DB tests need SIMPLECD_TEST_DATABASE_URL (make dev-up provides one)
	SIMPLECD_TEST_DATABASE_URL=$${SIMPLECD_TEST_DATABASE_URL:-$(DEV_DB)} go test -race ./...

test-web: ## UI typecheck and tests
	cd web && npm run typecheck && npm test

lint: ## gofmt and go vet
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...

dev-up: ## Start Postgres and Keycloak for local development
	docker compose -f dev/compose.yaml up -d
	@test -f dev/github-app-dev.pem || openssl genrsa -out dev/github-app-dev.pem 2048 2>/dev/null

dev-down: ## Stop the development stack
	docker compose -f dev/compose.yaml down

dev-seed: ## Fill the development database with demo data
	SIMPLECD_DATABASE_URL='$(DEV_DB)' go run ./dev/seed

run: build ## Run the server locally against the development stack
	SIMPLECD_CONFIG=dev/config.yaml \
	SIMPLECD_DATABASE_URL='$(DEV_DB)' \
	SIMPLECD_BASE_URL=$(BASE_URL) \
	SIMPLECD_LISTEN=127.0.0.1:8080 \
	SIMPLECD_OIDC_ISSUER=http://127.0.0.1:8081/realms/simple-cd \
	SIMPLECD_OIDC_CLIENT_ID=simple-cd \
	SIMPLECD_OIDC_CLIENT_SECRET=dev-secret \
	SIMPLECD_GITHUB_APP_ID=1 \
	SIMPLECD_GITHUB_PRIVATE_KEY_FILE=dev/github-app-dev.pem \
	SIMPLECD_GITHUB_WEBHOOK_SECRET=dev-webhook-secret \
	SIMPLECD_RUNNER=process \
	SIMPLECD_REGISTRY_CHECK=off \
	SIMPLECD_EXECUTOR_BINARY=$(CURDIR)/bin/simple-cd-executor \
	./bin/simple-cd

$(KIND):
	GOBIN=$(CURDIR)/bin go install sigs.k8s.io/kind@$(KIND_VERSION)

e2e-up: $(KIND) ## Start the end-to-end environment (kind, Vault, Keycloak, simple-cd)
	KIND=$(KIND) e2e/up.sh

e2e: ## Run the end-to-end test against the environment from e2e-up
	go test -tags e2e -count=1 -v ./e2e/

e2e-down: $(KIND) ## Delete the end-to-end environment
	KIND=$(KIND) e2e/down.sh
