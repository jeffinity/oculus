#!/usr/bin/env bash
set -euo pipefail

. .build/gum_helper.sh

if [[ -x ".kitty/.bin/golangci-lint" ]]; then
  lint_cmd=".kitty/.bin/golangci-lint"
elif command -v golangci-lint &>/dev/null; then
  lint_cmd="golangci-lint"
else
  echo_color "⚠ golangci-lint not found, please execute \`kitty install\` first" yellow
  exit 1
fi

spin_exec "GOOS=linux $lint_cmd run ./..." GOOS=linux $lint_cmd run ./...

echo_color "✔ lint finished." green
