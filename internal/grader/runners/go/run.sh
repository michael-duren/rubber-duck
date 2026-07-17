#!/bin/sh
# Two modes:
#   local docker:  tar with solution.go + solution_test.go on stdin,
#                  test exit code is the container exit code.
#   cloud run job: INPUT_URL (signed GET, the tar) and OUTPUT_URL (signed PUT)
#                  are set; the result file's first line is the test exit
#                  code, the rest is output, and the container always exits 0
#                  so an execution failure can only mean infra trouble.
set -e
export HOME=/tmp
mkdir -p /tmp/job
cd /tmp/job
if [ -n "$INPUT_URL" ]; then
	curl -fsS "$INPUT_URL" | tar -x
else
	tar -xf -
fi
go mod init challenge > /dev/null 2>&1
# || true: goimports exits non-zero on code that doesn't parse. Under
# `set -e` that would abort the script before the result upload, turning a
# submission's syntax error into an infra failure in cloud-run mode; let
# `go test` report the compile error as an ordinary failing run instead.
goimports -w solution.go 2> /dev/null || true
if [ -z "$OUTPUT_URL" ]; then
	exec go test -v ./...
fi
set +e
go test -v ./... > /tmp/out.txt 2>&1
code=$?
set -e
{ echo "$code"; cat /tmp/out.txt; } > /tmp/result.txt
curl -fsS -X PUT -H "Content-Type: text/plain" --upload-file /tmp/result.txt "$OUTPUT_URL"
