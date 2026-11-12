#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

export PATH="$HOME/.local/bin:$PATH"

for tool in git go protoc protoc-gen-go protoc-gen-go-grpc \
  protoc-gen-grpc-gateway protoc-gen-validate; do
  if ! command -v "$tool" >/dev/null; then
    printf 'Required tool is missing: %s\n' "$tool" >&2
    exit 1
  fi
done

google_proto_dir=$(mktemp -d)
trap 'rm -rf -- "$google_proto_dir"' EXIT

git init --quiet "$google_proto_dir"
git -C "$google_proto_dir" fetch --quiet --depth=1 \
  https://github.com/googleapis/googleapis.git \
  89c3153888201c9e80bc5ec78d6ffca0debe6b52
git -C "$google_proto_dir" checkout --quiet FETCH_HEAD

GOWORK=off go mod download github.com/envoyproxy/protoc-gen-validate@v1.3.3
validate_proto_dir="$(go env GOMODCACHE)/github.com/envoyproxy/protoc-gen-validate@v1.3.3"

# Use the standard protobuf includes from Ubuntu or Homebrew when present.
set --
if [[ -f /usr/include/google/protobuf/timestamp.proto ]]; then
  set -- -I /usr/include
elif command -v brew >/dev/null 2>&1; then
  if protobuf_prefix=$(brew --prefix protobuf 2>/dev/null) &&
    [[ -f "$protobuf_prefix/include/google/protobuf/timestamp.proto" ]]; then
    set -- -I "$protobuf_prefix/include"
  fi
fi

mkdir -p pkg/generated

protoc \
  --experimental_allow_proto3_optional \
  -I user/api \
  -I chat/api \
  -I onboarding/api \
  -I task/api \
  -I "$google_proto_dir" \
  -I "$validate_proto_dir" \
  "$@" \
  --go_out=pkg/generated \
  --go_opt=paths=source_relative \
  --go-grpc_out=pkg/generated \
  --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false \
  --grpc-gateway_out=pkg/generated \
  --grpc-gateway_opt=paths=source_relative \
  --validate_out=lang=go,paths=source_relative:pkg/generated \
  user/api/user/v1/user.proto \
  chat/api/chat/v1/chat.proto \
  onboarding/api/plan/v1/plan.proto \
  onboarding/api/step/v1/step.proto \
  task/api/task/v1/task.proto

for file in \
  user/v1/user.pb.go \
  chat/v1/chat.pb.go \
  plan/v1/plan_grpc.pb.go \
  step/v1/step.pb.go \
  task/v1/task.pb.go; do
  if [[ ! -f "pkg/generated/$file" ]]; then
    printf 'Expected generated file is missing: pkg/generated/%s\n' "$file" >&2
    exit 1
  fi
done