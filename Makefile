TAILWIND_VERSION := v4.1.11
TAILWIND := bin/tailwindcss
SQL_PROXY_VERSION := v2.15.2
SQL_PROXY := bin/cloud-sql-proxy

.PHONY: tools generate css build serve db dev runner-images test test-integration seed check clean

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
	go build -o getcracked ./cmd/getcracked

db:
	docker compose up -d --wait postgres

dev: db generate css
	$(TAILWIND) -i assets/input.css -o internal/web/static/app.css --watch &
	templ generate --watch --proxy=http://localhost:8080 --cmd="go run ./cmd/getcracked serve"

runner-images:
	docker build -t gc-runner-go internal/grader/runners/go
	docker build -t gc-runner-python internal/grader/runners/python

test:
	go test ./...

test-integration: db
	TEST_DATABASE_URL=postgres://getcracked:getcracked@localhost:5432/getcracked?sslmode=disable go test ./...

seed:
	go run ./cmd/getcracked seed seed/intro-to-go.md

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
	docker push $(AR)/getcracked:$(TAG)
	docker push $(AR)/gc-runner-go:$(TAG)
	docker push $(AR)/gc-runner-python:$(TAG)
	@echo "pushed tag $(TAG)"

deploy: push-images
	cd infra && tofu apply -var project_id=$(PROJECT) -var region=$(REGION) -var image_tag=$(TAG)

infra-validate:
	cd infra && tofu fmt -check && tofu validate

clean:
	rm -f getcracked internal/web/static/app.css
