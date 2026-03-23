#!/usr/bin/env bash

set -e

SCRIPT_START_TS="$(date +%s)"

_log() {
  local level="$1"
  local color="$2"
  shift 2

  local now
  now="$(date '+%Y-%m-%d %H:%M:%S')"

  local prefix="[$level]"
  if [[ -t 1 ]]; then
    printf '\033[%sm%-10s\033[0m%s %s\n' "$color" "$prefix" "$now" "$*"
  else
    printf '%-10s%s %s\n' "$prefix" "$now" "$*"
  fi
}

log_info()    { _log "INFO"    "34" "$@"; }
log_success() { _log "SUCCESS" "32" "$@"; }
log_error()   { _log "ERROR"   "31" "$@" >&2; }

print_run_summary() {
  local message="$1"
  local elapsed end_at
  elapsed="$(( $(date +%s) - SCRIPT_START_TS ))"
  end_at="$(date '+%Y-%m-%d %H:%M:%S %Z')"

  log_success "$message"
  log_info "Elapsed: ${elapsed}s"
  log_info "Completed at: ${end_at}"
}

cd "$(dirname "$0")"

BIN_PATH="bin/wechat-codex"

if [ -f "$BIN_PATH" ]; then
  log_info "Cleaning up existing binary at $BIN_PATH..."
  rm -f "$BIN_PATH"
fi

log_info "Building wechat-codex..."
mkdir -p bin
go build -o "$BIN_PATH" .

if [ -f "$BIN_PATH" ]; then
  print_run_summary "Build successful! You can run it with $BIN_PATH"
else
  log_error "Build failed: Binary not found at $BIN_PATH"
  exit 1
fi
