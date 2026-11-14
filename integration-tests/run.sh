#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
project="aibody-it-$(date +%s)-$$"
compose=(docker compose -p "$project" -f compose.yml)
cleanup() {
  status=$?
  trap - EXIT
  if [ "$status" -ne 0 ]; then "${compose[@]}" logs --no-color; fi
  "${compose[@]}" down --volumes --remove-orphans || true
  exit "$status"
}
docker info >/dev/null
trap cleanup EXIT
"${compose[@]}" build
"${compose[@]}" run --rm tests "$@"
