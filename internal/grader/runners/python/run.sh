#!/bin/sh
# Receives solution.py + test_solution.py as a tar stream on stdin.
set -e
export HOME=/tmp
mkdir -p /tmp/job
cd /tmp/job
tar -xf -
exec python -m pytest -q
