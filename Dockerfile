# Builder is debian-based: the tailwind standalone binary needs glibc.
FROM golang:1.26 AS build
WORKDIR /src

ARG TAILWIND_VERSION=v4.1.11
ADD https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/tailwindcss-linux-x64 /usr/local/bin/tailwindcss
RUN chmod +x /usr/local/bin/tailwindcss

COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020

COPY . .
RUN templ generate \
    && tailwindcss -i assets/input.css -o internal/web/static/app.css --minify \
    && CGO_ENABLED=0 go build -o /duckserver ./cmd/duckserver

# Runtime needs the docker CLI: the M1 grader shells out to the host daemon
# through the mounted socket.
FROM alpine:3.22
RUN apk add --no-cache docker-cli ca-certificates
COPY --from=build /duckserver /usr/local/bin/duckserver
EXPOSE 8080
ENTRYPOINT ["duckserver"]
CMD ["serve"]
