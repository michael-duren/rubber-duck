TAILWIND_VERSION := v4.1.11
TAILWIND := bin/tailwindcss
SQL_PROXY_VERSION := v2.15.2
SQL_PROXY := bin/cloud-sql-proxy

.PHONY: tools generate css build duck install uninstall serve db dev runner-images test test-integration seed import-courses-prod export-courses check clean editor-bundle

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

# Rebuild the vendored CodeMirror editor bundle. Unlike app.css (built in CI
# by the tailwind standalone binary), internal/web/static/cm6.js is a
# COMMITTED artifact — the web app has no JS bundler and CI has no Node. Run
# this only when changing editor/src or bumping CodeMirror, then commit the
# regenerated cm6.js. Needs Node >= 18. See editor/README.md.
editor-bundle:
	cd editor && npm ci && npm run build

build: generate css
	go build -o duckserver ./cmd/duckserver

# --- duck CLI (the local learner/author companion) ---
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
MANDIR ?= $(PREFIX)/share/man

duck:
	go build -o duck ./cmd/duck

# install builds the duck CLI and installs it alongside its man page, so
# `man duck` works after a `make install`. Override the location with
# PREFIX=... (default /usr/local) or DESTDIR=... when packaging.
install: duck
	install -Dm755 duck $(DESTDIR)$(BINDIR)/duck
	install -Dm644 manpages/duck.1 $(DESTDIR)$(MANDIR)/man1/duck.1
	@echo "installed duck to $(DESTDIR)$(BINDIR) and its man page to $(DESTDIR)$(MANDIR)/man1"

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/duck $(DESTDIR)$(MANDIR)/man1/duck.1

db:
	docker compose up -d --wait postgres

# WARN: USE WITH CAUTION - wipes this project's containers and pgdata volume
prune:
	docker compose down -v

dev: db generate css
	$(TAILWIND) -i assets/input.css -o internal/web/static/app.css --watch &
# First boot of a fresh database: once the server answers (it runs
# migrations on start), seed the quickstart courses. Skipped whenever any
# course exists (seed is idempotent anyway; this just avoids the wait).
	( i=0; until curl -sf -o /dev/null http://localhost:8080/; do \
	    i=$$((i+1)); test $$i -lt 120 || exit 0; sleep 0.5; done; \
	  n="$$(docker compose exec -T postgres psql -U duckserver -d duckserver -tAc 'select count(*) from courses' 2>/dev/null)"; \
	  if [ "$$n" = "0" ]; then echo "dev: empty database, seeding quickstart courses"; $(MAKE) seed; fi ) &
	templ generate --watch --proxy=http://localhost:8080 --cmd="go run ./cmd/duckserver serve"

runner-images:
	docker build -t gc-runner-go internal/grader/runners/go
	docker build -t gc-runner-python internal/grader/runners/python
	docker build -t gc-runner-c internal/grader/runners/c

test:
	go test ./...

test-integration: db
	TEST_DATABASE_URL=postgres://duckserver:duckserver@localhost:5432/duckserver?sslmode=disable go test ./...

# Seed writes straight to the local compose Postgres (no server round trip,
# no credentials): the quickstart fixture plus every course in courses/, so
# local dev has the same catalog as prod. Idempotent — unchanged documents
# are skipped, so re-running never bumps variant versions.
seed:
	@for f in seed/intro-to-go.md courses/*.md; do \
		echo "seeding $$f"; \
		go run ./cmd/duckserver seed "$$f" || exit 1; \
	done

# BREAK-GLASS ONLY: import courses/*.md straight into the prod database,
# bypassing the proposal/review workflow (needs gcloud ADC + tofu state).
# The normal way content reaches prod is a proposal getting approved on the
# site; this exists for bootstrap and disaster recovery. Imports are
# idempotent (unchanged documents are skipped) and unattributed.
import-courses-prod: $(SQL_PROXY)
	@set -e; \
	conn=$$(tofu -chdir=infra output -raw sql_connection_name); \
	pass=$$(tofu -chdir=infra output -raw db_password); \
	$(SQL_PROXY) "$$conn" --port 5433 & proxy=$$!; \
	trap 'kill $$proxy 2>/dev/null' EXIT; \
	sleep 3; \
	for f in courses/*.md; do \
		echo "importing $$f"; \
		go run ./cmd/duckserver seed --db "postgres://getcracked:$$pass@localhost:5433/getcracked?sslmode=disable" "$$f" || exit 1; \
	done

# Regenerate the courses/ mirror from a running server's /api/v1/export
# (the DB is the source of truth; courses/ is a synced copy kept fresh by
# .github/workflows/course-sync.yml — this target is the same script for
# local use).
#
#   make export-courses DUCK_URL=http://localhost:8080  # against `make dev`
DUCK_URL ?= https://duckgc.com

export-courses:
	DUCK_BASE_URL=$(DUCK_URL) ./scripts/export-courses.sh

# Interactive SQL shells. psql: the compose Postgres from `make dev`.
# psql-prod: fetches the app's DATABASE_URL from Secret Manager (needs
# `gcloud auth login` first), opens a Cloud SQL proxy on :5433, and tears
# it down when psql exits.
psql:
	psql "postgres://duckserver:duckserver@localhost:5432/duckserver?sslmode=disable"

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
	rm -f duckserver duck internal/web/static/app.css

lint: ## run golangci-lint (same version config as CI)
	golangci-lint run ./...
