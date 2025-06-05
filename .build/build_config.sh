#!/usr/bin/env bash
set -euo pipefail

. .build/gum_helper.sh

start="${1:-.}"

tmpfile="$(mktemp)"
if ! find "$start" -type f -name 'conf.proto' -print0 > "$tmpfile"; then
  echo_color "✖ Error: find 命令执行失败" red
  rm -f "$tmpfile"
  exit 1
fi

declare -a path_list=()
while IFS= read -r -d '' file; do
  dir="$(dirname "$file")"
  if real="$(cd "$dir" && pwd -P)"; then
    path_list+=("$real")
  fi
done < "$tmpfile"
rm -f "$tmpfile"

declare -A seen
unique_paths=()
for p in "${path_list[@]}"; do
  if [[ -n "$p" && -z "${seen[$p]:-}" ]]; then
    seen["$p"]=1
    unique_paths+=("$p")
  fi
done

shopt -s nullglob
for path in "${unique_paths[@]}"; do
  echo_color "◌ Build: ${path}..." white

  (
    cd "$path" || { echo_color "✖ Error: 无法进入目录 $path" red; exit 1; }

    proto_files=( *.proto )
    if (( ${#proto_files[@]} == 0 )); then
      echo_color "→ Skipped (no .proto in $path)" yellow
      exit 0
    fi

    protoc \
      --proto_path=. \
      --proto_path=../../../../third_party \
      --proto_path=../../../../proto \
      --go_out=paths=source_relative:. \
      "${proto_files[@]}"
  )
done
shopt -u nullglob

echo_color "✔ All .proto in '$start' have been processed." green
