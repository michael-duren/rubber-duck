#!/bin/sh
# Receives solution.go + solution_test.go as a tar stream on stdin.
set -e
export HOME=/tmp
mkdir -p /tmp/job
cd /tmp/job
tar -xf -
go mod init challenge > /dev/null 2>&1
exec go test ./...
