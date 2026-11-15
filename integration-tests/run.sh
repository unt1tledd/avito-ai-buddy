#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

docker_cmd=(docker)
if [[ "${DOCKER_USE_SUDO:-0}" == "1" ]]; then
  docker_cmd=(sudo docker)
fi

project="aibody-it-$(date +%s)-$$"
compose=("${docker_cmd[@]}" compose -p "$project" -f compose.yml)

cleanup() {
  local status=$?
  trap - EXIT

  if (( status != 0 )); then
    "${compose[@]}" logs --no-color || true
  fi

  if ! "${compose[@]}" down --volumes --remove-orphans; then
    if (( status == 0 )); then
      status=1
    fi
  fi

  exit "$status"
}

"${docker_cmd[@]}" info >/dev/null
trap cleanup EXIT

"${compose[@]}" build
"${compose[@]}" run --rm tests "$@"