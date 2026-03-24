#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAG_NAME="${1:-${GITHUB_REF_NAME:-}}"
OUTPUT_PATH="${2:-${RELEASE_NOTES_PATH:-$ROOT_DIR/dist/release-notes.md}}"
REPOSITORY="${GITHUB_REPOSITORY:-}"

fail() {
  printf '[release-notes] ERROR: %s\n' "$*" >&2
  exit 1
}

[ -n "$TAG_NAME" ] || fail "tag name is required"
git -C "$ROOT_DIR" rev-parse -q --verify "refs/tags/$TAG_NAME" >/dev/null 2>&1 || fail "tag not found: $TAG_NAME"

mkdir -p "$(dirname "$OUTPUT_PATH")"

previous_tag="$(
  git -C "$ROOT_DIR" tag --merged "$TAG_NAME" --sort=-version:refname \
    | awk -v current="$TAG_NAME" '$0 != current { print; exit }'
)"

if [ -n "$previous_tag" ]; then
  revision_range="${previous_tag}..${TAG_NAME}"
else
  revision_range="$TAG_NAME"
fi

{
  printf '## What'\''s Changed\n\n'

  git -C "$ROOT_DIR" log --reverse --format='%H%x09%s' "$revision_range" \
    | while IFS=$'\t' read -r sha subject; do
        short_sha="${sha:0:7}"
        if [ -n "$REPOSITORY" ]; then
          printf -- '- %s ([`%s`](https://github.com/%s/commit/%s))\n' \
            "$subject" "$short_sha" "$REPOSITORY" "$sha"
        else
          printf -- '- %s (`%s`)\n' "$subject" "$short_sha"
        fi
      done

  printf '\n'

  if [ -n "$REPOSITORY" ] && [ -n "$previous_tag" ]; then
    printf '**Full Changelog**: [%s...%s](https://github.com/%s/compare/%s...%s)\n' \
      "$previous_tag" "$TAG_NAME" "$REPOSITORY" "$previous_tag" "$TAG_NAME"
  elif [ -n "$REPOSITORY" ]; then
    printf '**Commit History**: [View all commits](https://github.com/%s/commits/%s)\n' \
      "$REPOSITORY" "$TAG_NAME"
  fi
} > "$OUTPUT_PATH"

printf '[release-notes] wrote %s\n' "$OUTPUT_PATH"
