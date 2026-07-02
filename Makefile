TAILWIND_VERSION := v4.1.11
TAILWIND := bin/tailwindcss

.PHONY: tools generate css build serve db dev runner-images test test-integration seed check clean

tools: $(TAILWIND)
	@command -v templ >/dev/null || go install github.com/a-h/templ/cmd/templ@latest

$(TAILWIND):
	mkdir -p bin
	curl -fsSL -o $(TAILWIND) https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-linux-x64
	chmod +x $(TAILWIND)

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

clean:
	rm -f getcracked internal/web/static/app.css
