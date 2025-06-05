#!/usr/bin/env bash

set -eu

. .build/common.sh

export GOBIN=$(pwd)/.build/.bin
mkdir -p "$GOBIN"

declare -A packages
packages=(
    [protoc-gen-go]="google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6"
    [protoc-gen-go-grpc]="google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1"
    [protoc-gen-go-gin]="github.com/go-dev-frame/sponge/cmd/protoc-gen-go-gin@v1.13.2"
    [protoc-gen-openapiv2]="github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.26.3"
    [protoc-gen-gotag]="github.com/srikrsna/protoc-gen-gotag@v1.0.2"
    [buf]="github.com/bufbuild/buf/cmd/buf@latest"
    [gum]="github.com/charmbracelet/gum@latest"
)

check_binary() {
    local binary=$1
    if [ ! -f "$GOBIN/$binary" ]; then
        return 1 # 不存在返回 1
    fi
    return 0 # 存在返回 0
}

install_package() {
    local package_path=$1
    go install "$package_path"
}

for binary in "${!packages[@]}"; do
    if ! check_binary "$binary"; then
        echo_color "⇊ $binary does not exist, installing..." yellow
        install_package "${packages[$binary]}"
    fi
done

if ! check_binary "protoc"; then

    echo_color "⇊ protoc does not exist, installing..." yellow

    OS=$(go env GOOS)
    ARCH=$(go env GOARCH)
    TMP_DIR="/tmp/protoc"
    rm -rf "$TMP_DIR"
    mkdir -p $TMP_DIR

    case "$OS" in
        linux)
            URL="https://github.com/protocolbuffers/protobuf/releases/download/v31.0/protoc-31.0-linux-x86_64.zip"
            ;;
        darwin)
            case "$ARCH" in
                amd64)
                    URL="https://github.com/protocolbuffers/protobuf/releases/download/v31.0/protoc-31.0-osx-x86_64.zip"
                    ;;
                arm64)
                    URL="https://github.com/protocolbuffers/protobuf/releases/download/v31.0/protoc-31.0-osx-aarch_64.zip"
                    ;;
                *)
                    echo "Unsupported architecture: $OS:$ARCH, Please try to manually install protoc to the .build/.bin"
                    exit 1
            esac
            ;;
        *)
            echo "Unsupported OS: $OS, Please try to manually install protoc to the .build/.bin"
            exit 1
    esac

    echo "download protoc from $URL to $TMP_DIR/protoc.zip"
    curl -L "$URL" -o "$TMP_DIR/protoc.zip"
    unzip -q "$TMP_DIR/protoc.zip" -d "$TMP_DIR"

    mv "$TMP_DIR/bin/protoc" .build/.bin/protoc
    rm -rf "$TMP_DIR"
fi

echo_color "✔ All checks and installations are done." green