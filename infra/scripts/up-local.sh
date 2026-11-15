#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

usage() {
  printf 'Usage: bash infra/scripts/up-local.sh [--import-ai]\n' >&2
}

if [[ $# -gt 1 ]]; then
  usage
  exit 2
fi

case "${1:-}" in
  "") import_ai=false ;;
  --import-ai) import_ai=true ;;
  *)
    usage
    exit 2
    ;;
esac

if [[ ! -f .env ]]; then
  printf 'Create .env from .env.example and fill in the local settings.\n' >&2
  exit 1
fi

run_docker() {
  if [[ "${DOCKER_USE_SUDO:-0}" == "1" ]]; then
    sudo docker "$@"
  else
    docker "$@"
  fi
}

printf '\n== Check Docker and Compose ==\n'
run_docker info >/dev/null
run_docker compose --profile tools config --quiet

printf '\n== Generate protobuf and build images ==\n'
bash infra/scripts/generate-proto.sh
run_docker compose build
run_docker compose --profile tools build auth-migrate

printf '\n== Prepare PostgreSQL ==\n'
run_docker compose up --quiet-pull --wait db
run_docker compose exec -T db \
  psql -U postgres -d postgres -f /docker-entrypoint-initdb.d/01-init.sql

migration_log=$(mktemp "${TMPDIR:-/tmp}/avito-migrations.XXXXXX")
printf '\nFull migration log: %s\n' "$migration_log"

run_migration() {
  run_docker compose --profile tools run --rm --no-deps --quiet-pull "$@" 2>&1 |
    tee -a "$migration_log" |
    sed '/^go: downloading /d'
}

printf '\n== Apply Auth migrations ==\n'
run_migration auth-migrate

printf '\n== Apply AI migrations ==\n'
run_migration ai-migrate


if [[ "$import_ai" == true ]]; then
  printf '\n== Import the AI knowledge corpus ==\n'
  run_docker compose run --rm --no-deps --quiet-pull \
    -e "CONTEXT_CONCURRENCY=${CONTEXT_CONCURRENCY:-1}" \
    -e "CONTEXT_MIN_INTERVAL_SECONDS=${CONTEXT_MIN_INTERVAL_SECONDS:-5}" \
    -e "IMPORT_TIMEOUT_SECONDS=${IMPORT_TIMEOUT_SECONDS:-7200}" \
    ai /app/.venv/bin/python -m ai_service.rag.import_documents \
    /app/knowledge/documents.json
fi

printf '\n== Start services ==\n'
run_docker compose up --quiet-pull -d
run_docker compose ps -a

printf '\nFrontend: http://127.0.0.1:8085/\n'
if ! run_docker compose exec -T ai /app/.venv/bin/python - <<'PY'
import time
from urllib.error import HTTPError, URLError
from urllib.request import urlopen

url = "http://127.0.0.1:8080/readyz"

for attempt in range(10):
    try:
        with urlopen(url, timeout=3) as response:
            print(f"AI readiness: HTTP {response.status} {response.read().decode()}")
        break
    except HTTPError as error:
        print(f"AI readiness: HTTP {error.code} {error.read().decode()}")
        break
    except (URLError, TimeoutError):
        if attempt == 9:
            print("AI readiness: no response from /readyz")
        else:
            time.sleep(1)
PY
then
  printf 'AI readiness: could not run the check inside the container.\n' >&2
fi