# Hookary developer entry points.
# Every target here is expected to work on a clean checkout after `make setup`.

SHELL := /bin/bash
.DEFAULT_GOAL := help

BACKEND := backend
FRONTEND := frontend

# Source the root .env, but pass only these public values into Flutter. Using
# --dart-define-from-file=.env would compile JWT, R2, and OpenAI secrets into
# the client bundle.
define flutter-with-auth
	@test -f .env || { echo ".env is missing — run make setup"; exit 1; }
	@set -a; . ./.env; set +a; \
	case "$$GOOGLE_CLIENT_ID_WEB" in \
		""|CHANGE_ME*) echo "GOOGLE_CLIENT_ID_WEB is not configured in .env"; exit 1 ;; \
	esac; \
	cd $(FRONTEND) && $(1) \
		--dart-define=HOOKARY_API_BASE_URL="$${HOOKARY_API_BASE_URL:-http://localhost:8080}" \
		--dart-define=HOOKARY_GOOGLE_CLIENT_ID_WEB="$$GOOGLE_CLIENT_ID_WEB" \
		--dart-define=HOOKARY_PRODUCTION="$${HOOKARY_PRODUCTION:-false}"
endef

# The containerized site talks to nginx on its own origin. nginx then resolves
# the Compose-only `api` hostname, so the browser never has to.
define flutter-web-for-docker
	@test -f .env || { echo ".env is missing — run make setup"; exit 1; }
	@set -a; . ./.env; set +a; \
	case "$$GOOGLE_CLIENT_ID_WEB" in \
		""|CHANGE_ME*) echo "GOOGLE_CLIENT_ID_WEB is not configured in .env"; exit 1 ;; \
	esac; \
	cd $(FRONTEND) && flutter build web --release --no-wasm-dry-run \
		--dart-define=HOOKARY_API_BASE_URL="$${HOOKARY_DOCKER_API_BASE_URL:-http://localhost:8000}" \
		--dart-define=HOOKARY_GOOGLE_CLIENT_ID_WEB="$$GOOGLE_CLIENT_ID_WEB" \
		--dart-define=HOOKARY_PRODUCTION="$${HOOKARY_PRODUCTION:-false}"
endef

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: ## Create .env from the example if absent, and fetch Dart deps
	@test -f .env || { cp .env.example .env; echo "created .env — fill in the CHANGE_ME values"; }
	@cd $(FRONTEND) && flutter pub get

# ---------------------------------------------------------------------------
# Stack
# ---------------------------------------------------------------------------

.PHONY: up
up: web-docker ## Build and start the whole stack, including Flutter web
	@for service in api ingest analyze render publish; do \
		docker compose build $$service || exit $$?; \
	done
	docker compose up -d --no-build

.PHONY: down
down: ## Stop the stack, keeping volumes
	docker compose down

.PHONY: reset
reset: ## Stop the stack and delete all local data
	docker compose down -v

.PHONY: ps
ps: ## Show container status
	docker compose ps

.PHONY: logs
logs: ## Tail logs from every service
	docker compose logs -f

.PHONY: health
health: ## Curl every service health endpoint
	@printf ":8000  "; \
	curl -s -o /dev/null -w "web=%{http_code}" http://localhost:$${HOOKARY_WEB_PORT:-8000}/healthz; \
	curl -s -o /dev/null -w "  api-via-web=%{http_code}\n" http://localhost:$${HOOKARY_WEB_PORT:-8000}/health/ready
	@for p in 8080 8081 8082 8083 8084; do \
		printf ":%s  " $$p; \
		curl -s -o /dev/null -w "live=%{http_code}" http://localhost:$$p/health/live; \
		curl -s -o /dev/null -w "  ready=%{http_code}\n" http://localhost:$$p/health/ready; \
	done

# ---------------------------------------------------------------------------
# Backend
# ---------------------------------------------------------------------------

.PHONY: go-build
go-build: ## Compile every Go service
	cd $(BACKEND) && go build ./...

.PHONY: go-test
go-test: ## Run Go tests that need nothing running
	cd $(BACKEND) && go test ./...

# Session rotation is transactional, recursive, and enforced by constraints.
# A fake would only prove the fake works, so these run against real Postgres —
# a separate database, so a test never touches development data.
TEST_DB := hookary_test
TEST_DB_URL := postgres://hookary:hookary_local_dev@localhost:5432/$(TEST_DB)?sslmode=disable

.PHONY: test-db
test-db: ## Create and migrate the test database
	@docker compose exec -T postgres psql -U hookary -d postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname='$(TEST_DB)'" | grep -q 1 || \
		docker compose exec -T postgres psql -U hookary -d postgres -c "CREATE DATABASE $(TEST_DB)"
	@cd $(BACKEND) && go tool goose -dir migrations postgres "$(TEST_DB_URL)" up

.PHONY: go-test-integration
go-test-integration: test-db ## Run every Go test, including those needing Postgres
	@set -a; . ./.env; set +a; \
	cd $(BACKEND) && \
	TEST_DATABASE_URL="$(TEST_DB_URL)" \
	TEST_S3_ENDPOINT="http://localhost:$${MINIO_API_PORT:-9000}" \
	TEST_S3_BUCKET="$${S3_BUCKET:-hookary-media-dev}" \
	TEST_S3_ACCESS_KEY_ID="$${S3_ACCESS_KEY_ID:-hookary_minio}" \
	TEST_S3_SECRET_ACCESS_KEY="$${S3_SECRET_ACCESS_KEY:-hookary_minio_local_dev}" \
	TEST_NATS_URL="nats://localhost:$${NATS_PORT:-4222}" \
	go test ./... -count=1

.PHONY: go-fmt
go-fmt: ## Format Go code
	cd $(BACKEND) && gofmt -w .

# ---------------------------------------------------------------------------
# Migrations
#
# goose runs from the host against the compose Postgres, using DATABASE_URL
# from .env. Migrations are never applied automatically on service startup:
# schema changes are a deliberate step, not a side effect of a deploy.
# ---------------------------------------------------------------------------

GOOSE := cd $(BACKEND) && set -a && . ../.env && set +a && \
	go tool goose -dir migrations postgres "$$DATABASE_URL"

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	@$(GOOSE) up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	@$(GOOSE) down

.PHONY: migrate-status
migrate-status: ## Show which migrations have been applied
	@$(GOOSE) status

.PHONY: migrate-reset
migrate-reset: ## Roll back every migration
	@$(GOOSE) reset

.PHONY: migrate-create
migrate-create: ## Create a migration: make migrate-create name=add_widgets
	@test -n "$(name)" || { echo "usage: make migrate-create name=add_widgets"; exit 1; }
	@cd $(BACKEND) && go tool goose -dir migrations -s create $(name) sql

.PHONY: go-lint
go-lint: ## Vet Go code and fail on unformatted files
	cd $(BACKEND) && go vet ./...
	@unformatted=$$(cd $(BACKEND) && gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "not gofmt'd:"; echo "$$unformatted"; exit 1; fi

.PHONY: sqlc
sqlc: ## Regenerate typed Go queries from backend/db/queries
	docker run --rm \
		-v "$(CURDIR)/$(BACKEND):/src" \
		-w /src \
		sqlc/sqlc:1.31.0 generate

# ---------------------------------------------------------------------------
# Frontend
# ---------------------------------------------------------------------------

.PHONY: flutter-test
flutter-test: ## Run Flutter tests
	cd $(FRONTEND) && flutter test

.PHONY: flutter-lint
flutter-lint: ## Analyze Dart code
	cd $(FRONTEND) && flutter analyze

.PHONY: run-web
run-web: ## Run Flutter web on the OAuth-registered local origin
	$(call flutter-with-auth,flutter run -d chrome --web-hostname=localhost --web-port=$${HOOKARY_WEB_PORT:-8000})

.PHONY: run-mobile
run-mobile: ## Run Flutter on a mobile device: make run-mobile device=<id>
	@test -n "$(device)" || { echo "usage: make run-mobile device=<flutter-device-id>"; exit 1; }
	$(call flutter-with-auth,flutter run -d "$(device)")

.PHONY: android-fingerprints
android-fingerprints: ## Print the SHA-1s to register as Android OAuth clients
	@KEYTOOL="/Applications/Android Studio.app/Contents/jbr/Contents/Home/bin/keytool"; \
	command -v keytool >/dev/null 2>&1 && KEYTOOL=keytool; \
	echo "Package name: com.hookary.app"; \
	echo; \
	echo "debug   ($$HOME/.android/debug.keystore)"; \
	"$$KEYTOOL" -list -v -keystore "$$HOME/.android/debug.keystore" \
		-alias androiddebugkey -storepass android -keypass android 2>/dev/null \
		| grep "SHA1:" | sed 's/^[[:space:]]*/  /' || echo "  (no debug keystore yet)"; \
	echo; \
	echo "release ($$HOME/.keystores/hookary-upload.jks)"; \
	if [ -f "$(FRONTEND)/android/key.properties" ]; then \
		pass=$$(grep '^storePassword=' $(FRONTEND)/android/key.properties | cut -d= -f2-); \
		"$$KEYTOOL" -list -v -keystore "$$HOME/.keystores/hookary-upload.jks" \
			-alias hookary-upload -storepass "$$pass" 2>/dev/null \
			| grep "SHA1:" | sed 's/^[[:space:]]*/  /'; \
	else \
		echo "  (no android/key.properties — see key.properties.example)"; \
	fi
	@echo
	@echo "Both must be registered: debug and release builds are signed differently."
	@echo "See docs/google-auth-setup.md"

.PHONY: web
web: ## Build Flutter web with the default renderer
	$(call flutter-with-auth,flutter build web --release)

.PHONY: web-docker
web-docker: ## Build Flutter web for the nginx Compose service
	$(flutter-web-for-docker)

.PHONY: web-wasm
web-wasm: ## Build Flutter web with WebAssembly
	$(call flutter-with-auth,flutter build web --release --wasm)

# ---------------------------------------------------------------------------
# Contracts
# ---------------------------------------------------------------------------

.PHONY: contracts-lint
contracts-lint: ## Validate the OpenAPI contract
	cd contracts && npx --yes @redocly/cli@latest lint openapi.yaml --config=redocly.yaml

# ---------------------------------------------------------------------------
# Aggregate
# ---------------------------------------------------------------------------

.PHONY: lint
lint: go-lint flutter-lint contracts-lint ## Lint everything

.PHONY: test
test: go-test flutter-test ## Test everything

.PHONY: check
check: lint test ## What CI runs
