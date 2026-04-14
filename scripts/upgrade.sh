#!/usr/bin/env bash

set -euo pipefail

REPO="${WECHAT_CODEX_REPO:-Arlowen/wechat-codex}"
VERSION="${WECHAT_CODEX_VERSION:-latest}"
BASE_URL="${WECHAT_CODEX_BASE_URL:-}"
INSTALL_SCRIPT_URL="${WECHAT_CODEX_INSTALL_SCRIPT_URL:-}"
BIN_NAME="wechat-codex"
INSTALL_DIR="${INSTALL_DIR:-}"
DEFAULT_INSTALL_DIR="$HOME/.wechat-codex"

log() {
  printf '[upgrade] %s\n' "$*"
}

fail() {
  printf '[upgrade] ERROR: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

resolve_path() {
  local path="$1"
  local target dir

  if ! command -v readlink >/dev/null 2>&1; then
    printf '%s\n' "$path"
    return
  fi

  while [ -L "$path" ]; do
    target="$(readlink "$path")"
    if [ "${target#/}" != "$target" ]; then
      path="$target"
    else
      dir="$(cd "$(dirname "$path")" && pwd -P)"
      path="$dir/$target"
    fi
  done

  if [ -e "$path" ] || [ -L "$path" ]; then
    dir="$(cd "$(dirname "$path")" && pwd -P)"
    printf '%s/%s\n' "$dir" "$(basename "$path")"
  else
    printf '%s\n' "$path"
  fi
}

resolve_existing_binary() {
  local candidate

  if [ -n "$INSTALL_DIR" ]; then
    candidate="$INSTALL_DIR/$BIN_NAME"
    if [ -f "$candidate" ] || [ -L "$candidate" ]; then
      resolve_path "$candidate"
      return
    fi
  fi

  for candidate in \
    "$(type -P "$BIN_NAME" 2>/dev/null || true)" \
    "$DEFAULT_INSTALL_DIR/$BIN_NAME" \
    "/opt/homebrew/bin/$BIN_NAME" \
    "/usr/local/bin/$BIN_NAME" \
    "$HOME/.local/bin/$BIN_NAME" \
    "$HOME/bin/$BIN_NAME"; do
    if [ -n "$candidate" ] && { [ -f "$candidate" ] || [ -L "$candidate" ]; }; then
      resolve_path "$candidate"
      return
    fi
  done
}

local_install_script() {
  local self_path self_dir

  self_path="${BASH_SOURCE[0]:-}"
  [ -n "$self_path" ] || return 1

  self_dir="$(cd "$(dirname "$self_path")" && pwd -P)"
  if [ -f "$self_dir/install.sh" ]; then
    printf '%s/install.sh\n' "$self_dir"
    return 0
  fi

  return 1
}

install_script_url() {
  if [ -n "$INSTALL_SCRIPT_URL" ]; then
    printf '%s\n' "$INSTALL_SCRIPT_URL"
    return
  fi

  printf 'https://raw.githubusercontent.com/%s/main/scripts/install.sh\n' "$REPO"
}

service_running() {
  local binary_path="$1"

  if [ -x "$binary_path" ] && "$binary_path" status >/dev/null 2>&1; then
    return 0
  fi

  return 1
}

run_install_script() {
  local target_install_dir="$1"
  local install_script raw_url

  if install_script="$(local_install_script)"; then
    log "using bundled install.sh"
    if [ -n "$target_install_dir" ]; then
      INSTALL_DIR="$target_install_dir" \
      WECHAT_CODEX_REPO="$REPO" \
      WECHAT_CODEX_VERSION="$VERSION" \
      WECHAT_CODEX_BASE_URL="$BASE_URL" \
      bash "$install_script"
    else
      WECHAT_CODEX_REPO="$REPO" \
      WECHAT_CODEX_VERSION="$VERSION" \
      WECHAT_CODEX_BASE_URL="$BASE_URL" \
      bash "$install_script"
    fi
    return
  fi

  need_cmd curl
  raw_url="$(install_script_url)"
  log "fetching install.sh from ${raw_url}"
  if [ -n "$target_install_dir" ]; then
    curl -fsSL "$raw_url" | INSTALL_DIR="$target_install_dir" \
      WECHAT_CODEX_REPO="$REPO" \
      WECHAT_CODEX_VERSION="$VERSION" \
      WECHAT_CODEX_BASE_URL="$BASE_URL" \
      bash
  else
    curl -fsSL "$raw_url" | WECHAT_CODEX_REPO="$REPO" \
      WECHAT_CODEX_VERSION="$VERSION" \
      WECHAT_CODEX_BASE_URL="$BASE_URL" \
      bash
  fi
}

main() {
  local current_binary_path target_install_dir was_running=0

  current_binary_path="$(resolve_existing_binary || true)"
  if [ -n "$INSTALL_DIR" ]; then
    target_install_dir="$INSTALL_DIR"
  elif [ -n "$current_binary_path" ]; then
    target_install_dir="$(dirname "$current_binary_path")"
  else
    target_install_dir=""
  fi

  if [ -n "$current_binary_path" ]; then
    log "detected existing binary at $current_binary_path"
    if service_running "$current_binary_path"; then
      was_running=1
    fi
  else
    log "no existing installation found, running install flow"
  fi

  if [ -n "$target_install_dir" ]; then
    log "upgrading in $target_install_dir"
  fi

  run_install_script "$target_install_dir"

  if [ "$was_running" -eq 1 ]; then
    log "daemon was running before upgrade"
    log "restart it to load the new binary: $BIN_NAME stop && $BIN_NAME start -d"
  fi
}

main "$@"
