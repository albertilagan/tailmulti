#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
export GOPATH="$root/.cache/go" GOCACHE="$root/.cache/go-build" GOTOOLCHAIN=auto
cd "$root/engine"
exec go "$@"
