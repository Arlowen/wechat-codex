#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-$ROOT_DIR/dist}"
VERSION="${1:-${GITHUB_REF_NAME:-dev}}"
COMMIT_SHA="${COMMIT_SHA:-$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || printf 'unknown')}"
BUILD_DATE="${BUILD_DATE:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
TARGETS=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
)

cd "$ROOT_DIR"
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

for target in "${TARGETS[@]}"; do
  read -r goos goarch <<<"$target"
  package_name="wechat-codex_${goos}_${goarch}"
  stage_dir="$DIST_DIR/$package_name"

  printf '==> building %s\n' "$package_name"
  mkdir -p "$stage_dir"

  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath \
      -ldflags="-s -w -X wechat-codex/cmd.Version=$VERSION -X wechat-codex/cmd.Commit=$COMMIT_SHA -X wechat-codex/cmd.BuildDate=$BUILD_DATE" \
      -o "$stage_dir/wechat-codex" .

  mkdir -p "$stage_dir/scripts"
  cp README.md "$stage_dir/README.md"
  cp docs/manual.md "$stage_dir/manual.md"
  cp scripts/install.sh "$stage_dir/scripts/install.sh"
  cp scripts/uninstall.sh "$stage_dir/scripts/uninstall.sh"
  cp scripts/upgrade.sh "$stage_dir/scripts/upgrade.sh"
  chmod 0755 "$stage_dir/wechat-codex" \
    "$stage_dir/scripts/install.sh" \
    "$stage_dir/scripts/uninstall.sh" \
    "$stage_dir/scripts/upgrade.sh"

  tar -C "$DIST_DIR" -czf "$DIST_DIR/$package_name.tar.gz" "$package_name"
  rm -rf "$stage_dir"
done

(
  cd "$DIST_DIR"
  : > checksums.txt
  for file in *.tar.gz; do
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$file" >> checksums.txt
    else
      shasum -a 256 "$file" >> checksums.txt
    fi
  done
)

printf 'release artifacts written to %s\n' "$DIST_DIR"
