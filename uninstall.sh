#!/usr/bin/env bash

set -euo pipefail

BIN_NAME="wechat-codex"
INSTALL_DIR="${INSTALL_DIR:-}"
PURGE_DATA="${WECHAT_CODEX_PURGE_DATA:-0}"
RUNTIME_DIR="${WECHAT_CODEX_RUNTIME_DIR:-$HOME/.wechat-codex}"

log() {
  printf '[uninstall] %s\n' "$*"
}

resolve_binary_path() {
  local candidate

  if [ -n "$INSTALL_DIR" ]; then
    candidate="$INSTALL_DIR/$BIN_NAME"
    if [ -f "$candidate" ]; then
      printf '%s\n' "$candidate"
      return
    fi
  fi

  candidate="$(type -P "$BIN_NAME" 2>/dev/null || true)"
  if [ -n "$candidate" ] && [ -f "$candidate" ]; then
    printf '%s\n' "$candidate"
    return
  fi

  for candidate in "/usr/local/bin/$BIN_NAME" "$HOME/.local/bin/$BIN_NAME"; do
    if [ -f "$candidate" ]; then
      printf '%s\n' "$candidate"
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
  local binary_path

  binary_path="$(resolve_binary_path)"
  if [ -z "$binary_path" ]; then
    log "no installed $BIN_NAME binary found"
  else
    maybe_stop_service "$binary_path"
    rm -f "$binary_path"
    log "removed $binary_path"
  fi

  if [ "$PURGE_DATA" = "1" ]; then
    rm -rf "$RUNTIME_DIR"
    log "removed runtime data at $RUNTIME_DIR"
  else
    log "runtime data kept at $RUNTIME_DIR"
    log "set WECHAT_CODEX_PURGE_DATA=1 to remove runtime data as well"
  fi
}

main "$@"
