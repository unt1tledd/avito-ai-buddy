#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

case "${1:-}" in
  "") import_ai=false ;;
  --import-ai) import_ai=true ;;
  *)
    printf 'Usage: bash infra/scripts/up-local.sh [--import-ai]\n' >&2
    exit 2
    ;;
esac

if [[ ! -f .env ]]; then
  printf 'Create .env from .env.example and fill in the local settings.\n' >&2
  exit 1
fi

export PATH="$HOME/.local/bin:$PATH"

run_docker() {
  if [[ "${DOCKER_USE_SUDO:-0}" == "1" ]]; then
    sudo docker "$@"
  else
    docker "$@"
  fi
}

printf '\n== Check Docker and Compose ==\n'
run_docker info >/dev/null
run_docker compose config --quiet

printf '\n== Generate protobuf and build images ==\n'
bash infra/scripts/generate-proto.sh
run_docker compose build

printf '\n== Prepare PostgreSQL ==\n'
run_docker compose up --wait db
run_docker compose exec -T db \
  psql -U postgres -d postgres -f /docker-entrypoint-initdb.d/01-init.sql

migration_log=$(mktemp "${TMPDIR:-/tmp}/avito-migrations.XXXXXX")
printf '\nFull migration log: %s\n' "$migration_log"

run_migration() {
  run_docker "$@" 2>&1 |
    tee -a "$migration_log" |
    sed '/^go: downloading /d'
}

printf '\n== Apply Auth migrations ==\n'
run_migration compose run --rm --no-deps \
  -v "$repo_root/auth/migrations:/migrations:ro" \
  --entrypoint sh auth -ec \
  'go run github.com/pressly/goose/v3/cmd/goose@v3.27.0 -dir /migrations postgres "$AUTH_DATABASE_URL" up'

printf '\n== Apply AI migrations ==\n'
# PostgreSQL is already healthy after the explicit wait above.
run_migration compose --profile tools run --rm --no-deps ai-migrate

if [[ "$import_ai" == true ]]; then
  printf '\n== Import the AI knowledge corpus ==\n'
  run_docker compose run --rm --no-deps ai \
    /app/.venv/bin/python -m ai_service.rag.import_documents \
    /app/knowledge/documents.json
fi

printf '\n== Start services ==\n'
run_docker compose up -d
run_docker compose ps -a

printf '\nFrontend: http://127.0.0.1:8085/\n'
printf 'AI readiness: http://127.0.0.1:8000/readyz\n'