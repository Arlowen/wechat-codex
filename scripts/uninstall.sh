#!/usr/bin/env bash

set -euo pipefail

BIN_NAME="wechat-codex"
INSTALL_DIR="${INSTALL_DIR:-}"
DEFAULT_INSTALL_DIR="$HOME/.wechat-codex"
PURGE_DATA="${WECHAT_CODEX_PURGE_DATA:-1}"
RUNTIME_DIR="${WECHAT_CODEX_RUNTIME_DIR:-$HOME/.wechat-codex}"
LAUNCHER_FILE_NAME=".launcher-path"

log() {
  printf '[uninstall] %s\n' "$*"
}

launcher_metadata_path() {
  local install_dir="$1"
  printf '%s/%s\n' "$install_dir" "$LAUNCHER_FILE_NAME"
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

resolve_binary_path() {
  local candidate

  if [ -n "$INSTALL_DIR" ]; then
    candidate="$INSTALL_DIR/$BIN_NAME"
    if [ -f "$candidate" ] || [ -L "$candidate" ]; then
      resolve_path "$candidate"
      return
    fi
  fi

  candidate="$(type -P "$BIN_NAME" 2>/dev/null || true)"
  if [ -n "$candidate" ] && { [ -f "$candidate" ] || [ -L "$candidate" ]; }; then
    resolve_path "$candidate"
    return
  fi

  for candidate in \
    "$DEFAULT_INSTALL_DIR/$BIN_NAME" \
    "/opt/homebrew/bin/$BIN_NAME" \
    "/usr/local/bin/$BIN_NAME" \
    "$HOME/.local/bin/$BIN_NAME" \
    "$HOME/bin/$BIN_NAME"; do
    if [ -f "$candidate" ] || [ -L "$candidate" ]; then
      resolve_path "$candidate"
      return
    fi
  done
}

maybe_stop_service() {
  local binary_path="$1"

  if [ ! -x "$binary_path" ]; then
    return
  fi

  if "$binary_path" status >/dev/null 2>&1; then
    "$binary_path" stop >/dev/null 2>&1 || true
  fi
}

main() {
  local binary_path install_dir launcher_path metadata_path

  binary_path="$(resolve_binary_path)"
  if [ -n "$binary_path" ]; then
    install_dir="$(dirname "$binary_path")"
  else
    install_dir="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
  fi
  metadata_path="$(launcher_metadata_path "$install_dir")"

  if [ -f "$metadata_path" ]; then
    launcher_path="$(cat "$metadata_path")"
    if [ -L "$launcher_path" ]; then
      rm -f "$launcher_path"
      log "removed launcher $launcher_path"
    fi
  fi

  if [ -z "$binary_path" ]; then
    log "no installed $BIN_NAME binary found"
  else
    maybe_stop_service "$binary_path"
    rm -f "$binary_path"
    log "removed $binary_path"
  fi

  if [ "$PURGE_DATA" = "0" ]; then
    log "runtime data kept at $RUNTIME_DIR"
    log "set WECHAT_CODEX_PURGE_DATA=1 to remove runtime data as well"
  else
    rm -rf "$RUNTIME_DIR"
    log "removed runtime data at $RUNTIME_DIR"
    log "set WECHAT_CODEX_PURGE_DATA=0 to keep runtime data"
  fi
}

main "$@"
