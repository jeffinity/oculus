#!/usr/bin/env bash

set -eu

. .build/gum_helper.sh


gen_proto() {

  sleep 1  # 展示 loading 效果最小时间

  (
    cd proto || {
      echo_color "✖ Error: 找不到 proto 目录" red
      return 1
    }

    if ! buf generate --template buf.gen.go.manual.yaml; then
      echo_color "✖ Error: buf generate (manual) 失败" red
      return 1
    fi

    if ! ln -sf ../api out; then
      echo_color "✖ Error: ln -sf ../api out 失败" red
      return 1
    fi

    if ! buf generate --template buf.gen.go.tag.maunal.yaml; then
      echo_color "✖ Error: buf generate (tag) 失败" red
      rm -f out
      return 1
    fi

    rm -f out
  )
}

spin_exec "gen proto..." gen_proto

echo_color "✔ All protos have been generated." green
