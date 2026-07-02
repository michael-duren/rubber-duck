#!/bin/sh
# Two modes; see the go runner's run.sh for the protocol.
set -e
export HOME=/tmp
mkdir -p /tmp/job
cd /tmp/job
if [ -n "$INPUT_URL" ]; then
	curl -fsS "$INPUT_URL" | tar -x
else
	tar -xf -
fi
if [ -z "$OUTPUT_URL" ]; then
	exec python -m pytest -q
fi
set +e
python -m pytest -q > /tmp/out.txt 2>&1
code=$?
set -e
{ echo "$code"; cat /tmp/out.txt; } > /tmp/result.txt
curl -fsS -X PUT -H "Content-Type: text/plain" --upload-file /tmp/result.txt "$OUTPUT_URL"
