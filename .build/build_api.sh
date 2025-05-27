#!/usr/bin/env bash

set -eu

. .build/common.sh

GOBIN=$(pwd)/.build/.bin
PATH="$GOBIN:$PATH"

cd proto
buf generate --template buf.gen.go.manual.yaml

ln -sf ../api out
buf generate --template buf.gen.go.tag.maunal.yaml
rm -f out

cd ..

echo_color "✔ All protos have been generated." green

echo
echo -e "\e[32m"
echo "      ████████ ██     ██   ██████    ██████  ████████  ████████  ████████"
echo "     ██░░░░░░ ░██    ░██  ██░░░░██  ██░░░░██░██░░░░░  ██░░░░░░  ██░░░░░░ "
echo "    ░██       ░██    ░██ ██    ░░  ██    ░░ ░██      ░██       ░██     "
echo "    ░█████████░██    ░██░██       ░██       ░███████ ░█████████░█████████"
echo "    ░░░░░░░░██░██    ░██░██       ░██       ░██░░░░  ░░░░░░░░██░░░░░░░░██"
echo "           ░██░██    ░██░░██    ██░░██    ██░██             ░██       ░██"
echo "     ████████ ░░███████  ░░██████  ░░██████ ░████████ ████████  ████████ "
echo "    ░░░░░░░░   ░░░░░░░    ░░░░░░    ░░░░░░  ░░░░░░░░ ░░░░░░░░  ░░░░░░░░  "
echo -e "\e[0m"
echo


