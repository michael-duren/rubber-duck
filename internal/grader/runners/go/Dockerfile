FROM golang:1.24-alpine

RUN apk add --no-cache curl

ENV GOCACHE=/gocache GOFLAGS=-buildvcs=false GOTOOLCHAIN=local

# Pre-warm the build cache with the testing package so grading runs
# compile in well under the job time limit even with no network.
RUN mkdir -p /warm && cd /warm && go mod init warm \
    && printf 'package warm\n\nimport "testing"\n\nfunc TestWarm(t *testing.T) {}\n' > warm_test.go \
    && go test ./... > /dev/null && rm -rf /warm

COPY run.sh /run.sh
RUN chmod +x /run.sh

USER nobody
ENTRYPOINT ["/run.sh"]
