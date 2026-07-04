FROM golang:1.24-alpine

RUN apk add --no-cache curl

ENV GOCACHE=/gocache GOFLAGS=-buildvcs=false GOTOOLCHAIN=local

# Pre-warm the build cache with the testing package so grading runs
# compile in well under the job time limit even with no network.
RUN mkdir -p /warm && cd /warm && go mod init warm \
    && printf 'package warm\n\nimport "testing"\n\nfunc TestWarm(t *testing.T) {}\n' > warm_test.go \
    && go test ./... > /dev/null && rm -rf /warm

# Installed at build time so grading runs need no network. Pinned
# because newer x/tools releases require a go toolchain past 1.24.
RUN go install golang.org/x/tools/cmd/goimports@v0.28.0 \
    && mv /go/bin/goimports /usr/local/bin/goimports

COPY run.sh /run.sh
RUN chmod +x /run.sh

# The warmed cache must stay writable by the runtime user: once the image
# is >24h old, `go test` rewrites /gocache/trim.txt and a permission error
# there makes go exit non-zero — grading a correct solution as failed.
# Safe because each grading run's container filesystem is discarded.
RUN chown -R nobody /gocache

USER nobody
ENTRYPOINT ["/run.sh"]
