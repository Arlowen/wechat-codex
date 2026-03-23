#!/usr/bin/env bash

set -e

cd "$(dirname "$0")"

echo "[info] Building wechat-codex for macOS..."
go build -o wechat-codex .

echo "[ok] Build successful! You can run it with ./wechat-codex"
