#!/usr/bin/env bash

set -euo pipefail

REPO="${WECHAT_CODEX_REPO:-Arlowen/wechat-codex}"
VERSION="${WECHAT_CODEX_VERSION:-latest}"
BASE_URL="${WECHAT_CODEX_BASE_URL:-}"
BIN_NAME="wechat-codex"
INSTALL_DIR="${INSTALL_DIR:-}"
DEFAULT_INSTALL_DIR="$HOME/.wechat-codex"
TMP_DIR=""

log() {
  printf '[install] %s\n' "$*"
}

fail() {
  printf '[install] ERROR: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux\n' ;;
    Darwin) printf 'darwin\n' ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

choose_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR"
    printf '%s\n' "$INSTALL_DIR"
    return
  fi

  if mkdir -p "$DEFAULT_INSTALL_DIR" 2>/dev/null; then
    printf '%s\n' "$DEFAULT_INSTALL_DIR"
    return
  fi

  fail "cannot create install directory: $DEFAULT_INSTALL_DIR, please set INSTALL_DIR=/path/to/bin"
}

download_base_url() {
  if [ -n "$BASE_URL" ]; then
    printf '%s\n' "$BASE_URL"
    return
  fi

  if [ "$VERSION" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download\n' "$REPO"
  else
    printf 'https://github.com/%s/releases/download/%s\n' "$REPO" "$VERSION"
  fi
}

verify_checksum() {
  local asset="$1"
  local checksum_url="$2"
  local checksum_file="$TMP_DIR/checksums.txt"
  local checksum_line="$TMP_DIR/checksum.line"

  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    log "sha256 tool not found, skip checksum verification"
    return
  fi

  if ! curl -fsSL "$checksum_url" -o "$checksum_file"; then
    log "checksums.txt not found, skip checksum verification"
    return
  fi

  if ! grep "[[:space:]]$asset\$" "$checksum_file" >"$checksum_line"; then
    log "checksum for $asset not found, skip checksum verification"
    return
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMP_DIR" && sha256sum -c "$(basename "$checksum_line")")
  else
    (cd "$TMP_DIR" && shasum -a 256 -c "$(basename "$checksum_line")")
  fi
}

main() {
  local os arch install_dir asset base_url archive_url checksum_url archive_path binary_path

  trap cleanup EXIT

  need_cmd curl
  need_cmd tar
  need_cmd mktemp
  need_cmd uname

  os="$(detect_os)"
  arch="$(detect_arch)"
  install_dir="$(choose_install_dir)"
  asset="${BIN_NAME}_${os}_${arch}.tar.gz"
  base_url="$(download_base_url)"
  archive_url="${base_url}/${asset}"
  checksum_url="${base_url}/checksums.txt"

  TMP_DIR="$(mktemp -d)"
  archive_path="$TMP_DIR/$asset"

  log "downloading ${archive_url}"
  curl -fsSL "$archive_url" -o "$archive_path"

  verify_checksum "$asset" "$checksum_url"

  tar -xzf "$archive_path" -C "$TMP_DIR"
  binary_path="$(find "$TMP_DIR" -type f -name "$BIN_NAME" | head -n 1)"
  [ -n "$binary_path" ] || fail "binary not found in release archive"

  cp "$binary_path" "$install_dir/$BIN_NAME"
  chmod 0755 "$install_dir/$BIN_NAME"

  log "installed to $install_dir/$BIN_NAME"
  if ! printf ':%s:' "$PATH" | grep -q ":$install_dir:"; then
    log "$install_dir is not in PATH"
    log "add this line to your shell profile: export PATH=\"$install_dir:\$PATH\""
  fi
  log "verify with: $BIN_NAME version"
}

main "$@"
