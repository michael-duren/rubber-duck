#!/bin/sh
# Two modes; see the go runner's run.sh for the protocol.
# Tests are a plain C program: test_solution.c has main(), is linked with
# solution.c, and exits non-zero on failure. A compile error surfaces the
# same way as a test failure (non-zero exit, compiler output as the log).
set -e
export HOME=/tmp
mkdir -p /tmp/job
cd /tmp/job
if [ -n "$INPUT_URL" ]; then
	curl -fsS "$INPUT_URL" | tar -x
else
	tar -xf -
fi
run_tests() {
	cc -std=c17 -Wall -O1 -o /tmp/job/test_bin solution.c test_solution.c \
		&& /tmp/job/test_bin
}
if [ -z "$OUTPUT_URL" ]; then
	run_tests
	exit $?
fi
set +e
run_tests > /tmp/out.txt 2>&1
code=$?
set -e
{ echo "$code"; cat /tmp/out.txt; } > /tmp/result.txt
curl -fsS -X PUT -H "Content-Type: text/plain" --upload-file /tmp/result.txt "$OUTPUT_URL"
