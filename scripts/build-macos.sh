#!/bin/sh
set -eu
# 构建macOS x86_64可执行文件；GO_BIN可指定固定工具链路径。
PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
GO_BIN=${GO_BIN:-go}
case "$("$GO_BIN" version)" in *go1.26.10*) ;; *) echo '需要已校验的Go1.26.10工具链' >&2; exit 1;; esac
cd "$PROJECT_ROOT/fe"
yarn install --frozen-lockfile --non-interactive
yarn build
cp -R dist "$PROJECT_ROOT/server/listen/http_server/"
mkdir -p "$PROJECT_ROOT/output"
cd "$PROJECT_ROOT/server"
GOOS=darwin GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0 GOTOOLCHAIN=local "$GO_BIN" build -trimpath \
 -ldflags '-s -w -X main.version=v2.9.7-local.1 -X main.gitHash=a6049bf+local-patch -X main.goVersion=go1.26.10' \
 -o "$PROJECT_ROOT/output/pmail-darwin-amd64" .
shasum -a 256 "$PROJECT_ROOT/output/pmail-darwin-amd64"
