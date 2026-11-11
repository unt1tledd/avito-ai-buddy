#!/usr/bin/env bash
set -euo pipefail

profile_dir=$(mktemp -d)
trap 'rm -rf -- "$profile_dir"' EXIT

covered_total=0
statements_total=0

for module in "$@"; do
  printf '\n=== Coverage: %s ===\n' "$module"

  packages=$(cd "$module" && go list ./...)
  selected=$(printf '%s\n' "$packages" | awk '
    NF &&
    $0 !~ /\/(mock|mocks|sqlc|entity|errors|port|adapter|adapters|config|migrations)(\/|$)/ &&
    $0 !~ /\/(cmd\/app|internal\/(app|controller|controller\/converter))$/
  ')

  if [[ -z "$selected" ]]; then
    printf 'TOTAL %s: n/a (no selected packages)\n' "$module"
    continue
  fi

  mapfile -t package_list <<< "$selected"
  profile="$profile_dir/coverage.out"
  (cd "$module" && go test -race -count=1 -coverprofile="$profile" "${package_list[@]}")

  counts=$(awk '
    NR > 1 && NF == 3 {
      if (!($1 in seen)) {
        seen[$1] = 1
        statements += $2
      }
      if ($3 > 0 && !covered[$1]) {
        covered[$1] = 1
        executed += $2
      }
    }
    END { printf "%d %d\n", executed, statements }
  ' "$profile")
  read -r covered statements <<< "$counts"

  if (( statements == 0 )); then
    printf 'TOTAL %s: n/a (no statements)\n' "$module"
  else
    go tool cover -func="$profile" | awk -v module="$module" '
      $1 == "total:" { printf "TOTAL %s: %s\n", module, $NF }
    '
  fi

  covered_total=$((covered_total + covered))
  statements_total=$((statements_total + statements))
done

awk -v covered="$covered_total" -v total="$statements_total" 'BEGIN {
  if (total > 0) {
    printf "\nTOTAL GO: %.1f%% (%d/%d statements)\n", 100 * covered / total, covered, total
  } else {
    print "\nTOTAL GO: n/a (no statements)"
  }
}'