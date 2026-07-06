TAILWIND_VERSION := v4.1.11
TAILWIND := bin/tailwindcss
SQL_PROXY_VERSION := v2.15.2
SQL_PROXY := bin/cloud-sql-proxy

.PHONY: tools generate css build serve db dev runner-images test test-integration seed publish apikey apikey-prod check clean

tools: $(TAILWIND) $(SQL_PROXY)
	@command -v templ >/dev/null || go install github.com/a-h/templ/cmd/templ@latest

$(TAILWIND):
	mkdir -p bin
	curl -fsSL -o $(TAILWIND) https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-linux-x64
	chmod +x $(TAILWIND)

$(SQL_PROXY):
	mkdir -p bin
	curl -fsSL -o $(SQL_PROXY) https://storage.googleapis.com/cloud-sql-connectors/cloud-sql-proxy/$(SQL_PROXY_VERSION)/cloud-sql-proxy.linux.amd64
	chmod +x $(SQL_PROXY)

generate:
	templ generate

css: $(TAILWIND)
	$(TAILWIND) -i assets/input.css -o internal/web/static/app.css --minify

build: generate css
	go build -o duckserver ./cmd/duckserver

db:
	docker compose up -d --wait postgres

dev: db generate css
	$(TAILWIND) -i assets/input.css -o internal/web/static/app.css --watch &
	templ generate --watch --proxy=http://localhost:8080 --cmd="go run ./cmd/duckserver serve"

runner-images:
	docker build -t gc-runner-go internal/grader/runners/go
	docker build -t gc-runner-python internal/grader/runners/python
	docker build -t gc-runner-c internal/grader/runners/c

test:
	go test ./...

test-integration: db
	TEST_DATABASE_URL=postgres://getcracked:getcracked@localhost:5432/getcracked?sslmode=disable go test ./...

seed:
	go run ./cmd/duckserver seed seed/intro-to-go.md
	go run ./cmd/duckserver seed courses/embedded-pico-c.md
	go run ./cmd/duckserver seed courses/build-a-hashmap-c.md

# Mint an agent API key (the gc_ kind that authenticates /api/v1 course
# publishing — NOT the gc_u_ user tokens `duck login` mints). The raw key
# prints once; store it (e.g. as the GC_API_KEY repo secret for CD).
KEY_NAME ?= writer-1

apikey:   ## mint against the local compose postgres
	go run ./cmd/duckserver apikey create --name $(KEY_NAME)

apikey-prod: $(SQL_PROXY)   ## mint against prod via cloud-sql-proxy (needs gcloud ADC + tofu state)
	@set -e; \
	conn=$$(tofu -chdir=infra output -raw sql_connection_name); \
	pass=$$(tofu -chdir=infra output -raw db_password); \
	$(SQL_PROXY) "$$conn" --port 5433 & proxy=$$!; \
	trap 'kill $$proxy 2>/dev/null' EXIT; \
	sleep 3; \
	go run ./cmd/duckserver apikey create --name $(KEY_NAME) \
		--db "postgres://getcracked:$$pass@localhost:5433/getcracked?sslmode=disable"

# GC_URL/GC_API_KEY: where to publish and how to authenticate. Defaults
# target a local `make dev` server; override both for prod.
GC_URL ?= http://localhost:8080

publish:
	@test -n "$$GC_API_KEY" || (echo "set GC_API_KEY=<key>" && exit 1)
	@for f in courses/*.md; do \
		echo "publishing $$f"; \
		go run ./cmd/duckserver seed --url $(GC_URL) "$$f" || exit 1; \
	done

# Interactive SQL shells. psql: the compose Postgres from `make dev`.
# psql-prod: fetches the app's DATABASE_URL from Secret Manager (needs
# `gcloud auth login` first), opens a Cloud SQL proxy on :5433, and tears
# it down when psql exits.
psql:
	psql "postgres://getcracked:getcracked@localhost:5432/getcracked?sslmode=disable"

psql-prod: $(SQL_PROXY)
	@test -n "$(PROJECT)" || (echo "set PROJECT=<gcp-project-id>" && exit 1)
	@set -e; \
	url=$$(gcloud secrets versions access latest --secret=gc-database-url --project=$(PROJECT)); \
	conn=$${url##*host=/cloudsql/}; \
	$(SQL_PROXY) "$$conn" --port 5433 --quiet & pid=$$!; \
	trap "kill $$pid 2>/dev/null || true" EXIT INT TERM; \
	i=0; until pg_isready -h localhost -p 5433 -q 2>/dev/null; do \
		i=$$((i+1)); test $$i -lt 30 || (echo "cloud-sql-proxy didn't come up" && exit 1); sleep 0.3; \
	done; \
	psql "$$(echo "$$url" | sed 's#@/#@localhost:5433/#; s#?host=.*##')"

check: generate css
	@git diff --exit-code -- '**/*_templ.go' || (echo "stale templ output: run make generate and commit" && exit 1)
	go vet ./...
	go test ./...

# --- GCP deploy (see README "Deploying to GCP") ---
REGION ?= us-central1
PROJECT ?=
AR = $(REGION)-docker.pkg.dev/$(PROJECT)/getcracked
TAG ?= $(shell git rev-parse --short HEAD)

push-images: runner-images
	@test -n "$(PROJECT)" || (echo "set PROJECT=<gcp-project-id>" && exit 1)
	docker build -t $(AR)/getcracked:$(TAG) .
	docker tag gc-runner-go $(AR)/gc-runner-go:$(TAG)
	docker tag gc-runner-python $(AR)/gc-runner-python:$(TAG)
	docker tag gc-runner-c $(AR)/gc-runner-c:$(TAG)
	docker push $(AR)/getcracked:$(TAG)
	docker push $(AR)/gc-runner-go:$(TAG)
	docker push $(AR)/gc-runner-python:$(TAG)
	docker push $(AR)/gc-runner-c:$(TAG)
	@echo "pushed tag $(TAG)"

deploy: push-images
	cd infra && tofu apply -var project_id=$(PROJECT) -var region=$(REGION) -var image_tag=$(TAG)

infra-validate:
	cd infra && tofu fmt -check && tofu validate

clean:
	rm -f duckserver internal/web/static/app.css

lint: ## run golangci-lint (same version config as CI)
	golangci-lint run ./...
