#!/usr/bin/env bash
set -euo pipefail

gomod_file="$(go env GOMOD 2>/dev/null || true)"
if [[ -z "$gomod_file" || "$gomod_file" == "/dev/null" ]]; then
  echo "Error: go.mod not found, run this in module root" >&2
  exit 1
fi

module_root="$(dirname "$gomod_file")"
cd "$module_root"

if [[ ! -d "api" ]]; then
  echo "Error: api directory not found: ${module_root}/api" >&2
  exit 1
fi

mkdir -p proto

target_path="${1:-api}"
echo "Generating api proto for path: ${target_path}"
buf generate --template buf.api.gen.yaml --path "$target_path"

if [[ -d "proto/api" ]]; then
  cp -R proto/api/. proto/
  rm -rf proto/api
fi

echo "Done: generated to ${module_root}/proto"
