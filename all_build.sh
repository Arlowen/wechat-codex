#!/usr/bin/env bash

set -e

cd "$(dirname "$0")"

echo "[info] Building wechat-codex for macOS..."
mkdir -p bin
go build -o bin/wechat-codex .

echo "[ok] Build successful! You can run it with bin/wechat-codex"
