SHELL := /bin/zsh

API_ADDR ?= 127.0.0.1:8080
API_ORIGIN ?= http://$(API_ADDR)
FRONTEND_HOST ?= 127.0.0.1
FRONTEND_PORT ?= 5173
APP_PORT ?= 8080
FRONTEND_DIR := frontend
FRONTEND_DEPS := $(FRONTEND_DIR)/node_modules/.package-lock.json
API_PORT := $(lastword $(subst :, ,$(API_ADDR)))
DATABASE_URL ?= postgres://athlete:athlete@127.0.0.1:5432/athlete_log?sslmode=disable
DATA_DIR ?= ./data
APP_DB ?= sqlite
SQLITE_PATH ?= $(DATA_DIR)/athlete-log.db
DOCS_OUT_DIR ?= docs-site
DOCS_ADDR ?= 127.0.0.1:8090
DOCS_VENV ?= .venv-docs
DOCS_MKDOCS := $(DOCS_VENV)/bin/mkdocs
DOCS_DEPS := $(DOCS_VENV)/.deps-installed

.PHONY: help deps build build-api build-frontend docs docs-deps docs-serve check-api-port run run-dev run-api run-frontend app-build app-down deploy deploy-frontend deploy-backend db-up db-wait db-down db-reset migrate test clean backup restore

help:
	@echo "athlete-log"
	@echo ""
	@echo "Targets:"
	@echo "  make run            Build and run app with Docker Compose"
	@echo "  make run-dev        Install deps and run local API + frontend"
	@echo "  make build          Build Go API binary and frontend assets"
	@echo "  make docs           Generate static HTML docs from Markdown"
	@echo "  make docs-deps      Install MkDocs into a local docs virtualenv"
	@echo "  make docs-serve     Generate and serve docs locally"
	@echo "  make test           Run Go tests and frontend production build"
	@echo "  make run-api        Run only the Go API"
	@echo "  make run-frontend   Run only the Vite frontend"
	@echo "  make app-build          Build the Docker app image"
	@echo "  make app-down           Stop the Docker app stack"
	@echo "  make deploy-frontend    Rebuild frontend; running container picks it up immediately"
	@echo "  make deploy-backend     Rebuild Go binary, copy into container, restart"
	@echo "  make deploy             Rebuild everything and restart (frontend + backend)"
	@echo "  make db-up          Start local Postgres"
	@echo "  make db-wait        Wait until local Postgres is ready"
	@echo "  make db-down        Stop local Postgres"
	@echo "  make db-reset       Recreate local Postgres data"
	@echo "  make migrate        Run Postgres migrations"
	@echo "  make clean          Remove generated build outputs"
	@echo "  make backup         Dump the Postgres database to backups/"
	@echo "  make restore FILE=  Restore from a .sql.gz backup file"
	@echo ""
	@echo "Config:"
	@echo "  API_ADDR=$(API_ADDR)"
	@echo "  API_ORIGIN=$(API_ORIGIN)"
	@echo "  FRONTEND_HOST=$(FRONTEND_HOST)"
	@echo "  FRONTEND_PORT=$(FRONTEND_PORT)"
	@echo "  APP_PORT=$(APP_PORT)"
	@echo "  DATABASE_URL=$(DATABASE_URL)"
	@echo "  DATA_DIR=$(DATA_DIR)"
	@echo "  APP_DB=$(APP_DB)"
	@echo "  SQLITE_PATH=$(SQLITE_PATH)"
	@echo "  DOCS_OUT_DIR=$(DOCS_OUT_DIR)"
	@echo "  DOCS_ADDR=$(DOCS_ADDR)"
	@echo "  DOCS_VENV=$(DOCS_VENV)"

deps: $(FRONTEND_DEPS)

$(FRONTEND_DEPS): $(FRONTEND_DIR)/package.json $(FRONTEND_DIR)/package-lock.json
	npm --prefix $(FRONTEND_DIR) install

build: build-api build-frontend

build-api:
	mkdir -p bin
	GOOS=linux GOARCH=arm64 go build -o bin/athlete-log ./cmd/server

build-frontend: deps
	npm --prefix $(FRONTEND_DIR) run build

docs-deps: $(DOCS_DEPS)

$(DOCS_DEPS): docs/requirements.txt
	python3 -m venv "$(DOCS_VENV)"
	"$(DOCS_VENV)/bin/python" -m pip install -r docs/requirements.txt
	touch "$(DOCS_DEPS)"

docs: docs-deps
	"$(DOCS_MKDOCS)" build --site-dir "$(DOCS_OUT_DIR)"

docs-serve: docs-deps
	"$(DOCS_MKDOCS)" serve --dev-addr "$(DOCS_ADDR)"

check-api-port:
	@if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:$(API_PORT) -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "Port $(API_PORT) is already in use. Stop the old API/dev server before running this target."; \
		exit 1; \
	fi

run:
	DATA_DIR="$(DATA_DIR)" APP_PORT="$(APP_PORT)" docker compose up --build app

run-dev: deps check-api-port
	@echo "Starting API at http://$(API_ADDR)"
	@echo "Starting frontend at http://$(FRONTEND_HOST):$(FRONTEND_PORT)"
	@APP_DB="$(APP_DB)" DATABASE_URL="$(DATABASE_URL)" SQLITE_PATH="$(SQLITE_PATH)" DATA_DIR="$(DATA_DIR)" go run ./cmd/server -addr $(API_ADDR) & \
	API_PID=$$!; \
	trap 'kill $$API_PID 2>/dev/null || true' INT TERM EXIT; \
	API_PROXY=$(API_ORIGIN) npm --prefix $(FRONTEND_DIR) run dev -- --host $(FRONTEND_HOST) --port $(FRONTEND_PORT)

run-api: check-api-port
	APP_DB="$(APP_DB)" DATABASE_URL="$(DATABASE_URL)" SQLITE_PATH="$(SQLITE_PATH)" DATA_DIR="$(DATA_DIR)" go run ./cmd/server -addr $(API_ADDR)

run-frontend: deps
	API_PROXY=$(API_ORIGIN) npm --prefix $(FRONTEND_DIR) run dev -- --host $(FRONTEND_HOST) --port $(FRONTEND_PORT)

app-build:
	DATA_DIR="$(DATA_DIR)" APP_PORT="$(APP_PORT)" docker compose build app

app-down:
	DATA_DIR="$(DATA_DIR)" APP_PORT="$(APP_PORT)" docker compose down

# ── Hot-deploy targets ────────────────────────────────────────────────────────
# The frontend/dist directory is volume-mounted into the running container
# (see docker-compose.yml), so a local npm build is picked up immediately
# without touching the image.

## Rebuild frontend and serve it from the running container (no restart needed).
deploy-frontend: build-frontend
	@echo "✓ Frontend rebuilt — the running container will serve the new assets."

## Build the Go binary locally, copy it into the running container, restart app.
deploy-backend: build-api
	DATA_DIR="$(DATA_DIR)" APP_PORT="$(APP_PORT)" docker compose cp bin/athlete-log app:/usr/local/bin/athlete-log
	DATA_DIR="$(DATA_DIR)" APP_PORT="$(APP_PORT)" docker compose restart app
	@echo "✓ Backend updated and container restarted."

## Full redeploy: rebuild frontend + backend, restart container.
## Use this after backend changes or first-time setup.
deploy: deploy-frontend deploy-backend

db-up:
	mkdir -p "$(DATA_DIR)/postgres"
	DATA_DIR=$(DATA_DIR) docker compose --profile postgres up -d postgres

db-wait:
	@echo "Waiting for local Postgres..."
	@for attempt in {1..30}; do \
		if DATA_DIR="$(DATA_DIR)" docker compose --profile postgres exec -T postgres pg_isready -U athlete -d athlete_log >/dev/null 2>&1; then \
			echo "Postgres is ready."; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "Postgres did not become ready in time."; \
	exit 1

db-down:
	DATA_DIR=$(DATA_DIR) docker compose --profile postgres down

db-reset: db-down
	test -n "$(DATA_DIR)"
	rm -rf -- "$(DATA_DIR)"
	$(MAKE) db-up DATA_DIR=$(DATA_DIR)

migrate: db-up db-wait
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate

test: deps
	go test ./...
	npm --prefix $(FRONTEND_DIR) run build

backup:
	mkdir -p backups
	DATA_DIR="$(DATA_DIR)" docker compose --profile postgres exec -T postgres pg_dump -U athlete athlete_log \
	  | gzip > backups/athlete_log_$(shell date +%Y%m%d_%H%M%S).sql.gz
	@echo "Backup written to backups/"

restore:
	@test -n "$(FILE)" || (echo "Usage: make restore FILE=backups/athlete_log_YYYYMMDD_HHMMSS.sql.gz" && exit 1)
	gunzip -c "$(FILE)" | DATA_DIR="$(DATA_DIR)" docker compose --profile postgres exec -T postgres psql -U athlete athlete_log

clean:
	rm -rf bin $(FRONTEND_DIR)/dist "$(DOCS_OUT_DIR)"
