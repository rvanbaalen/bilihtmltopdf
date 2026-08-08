#!/usr/bin/env bash
# fetch-headless-shell.sh — download chrome-headless-shell (Chrome for
# Testing) for every release platform into
#
#   third_party/headless-shell/<goos>_<goarch>/lib/chrome-headless-shell/
#
# the layout .goreleaser.yaml bundles next to the wkhtmltopdf binary so
# FindChrome's bundled lookup works offline. Runs as a goreleaser
# `before` hook; safe to run standalone.
#
# Idempotent: platforms already stamped with the requested version are
# skipped. Platforms with no upstream build get a NOTICE.txt instead.
#
# Usage:
#   scripts/fetch-headless-shell.sh [goos_goarch...]   # default: all
#   CHROME_VERSION=139.0.7258.66 scripts/fetch-headless-shell.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST_ROOT="$ROOT/third_party/headless-shell"
CFT_BASE="https://googlechromelabs.github.io/chrome-for-testing"
DL_BASE="https://storage.googleapis.com/chrome-for-testing-public"

need() { command -v "$1" >/dev/null 2>&1 || { echo "error: $1 is required" >&2; exit 1; }; }
need curl
need python3
need unzip

# goos_goarch:cft-platform; empty cft-platform = no upstream build.
ALL_PLATFORMS=(
  linux_amd64:linux64
  darwin_amd64:mac-x64
  darwin_arm64:mac-arm64
  windows_amd64:win64
  linux_arm64:
  windows_arm64:
)

# Resolve the version to fetch: pinned via CHROME_VERSION, else Stable.
if [[ -n "${CHROME_VERSION:-}" ]]; then
  VERSION="$CHROME_VERSION"
else
  VERSION="$(curl -fsSL "$CFT_BASE/last-known-good-versions.json" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["channels"]["Stable"]["version"])')"
fi
echo "chrome-headless-shell version: $VERSION"

# Optional positional filter: only fetch the named goos_goarch targets.
selected=("$@")
wants() {
  [[ ${#selected[@]} -eq 0 ]] && return 0
  local t
  for t in "${selected[@]}"; do [[ "$t" == "$1" ]] && return 0; done
  return 1
}

for entry in "${ALL_PLATFORMS[@]}"; do
  target="${entry%%:*}"
  cft="${entry#*:}"
  wants "$target" || continue

  dest="$DEST_ROOT/$target"
  shell_dir="$dest/lib/chrome-headless-shell"

  if [[ -z "$cft" ]]; then
    mkdir -p "$shell_dir"
    cat >"$shell_dir/NOTICE.txt" <<'EOF'
No chrome-headless-shell build is published for this platform
(https://googlechromelabs.github.io/chrome-for-testing/ covers
linux64, mac-x64, mac-arm64, win32 and win64 only).

wkhtmltopdf falls back to a system Chrome/Chromium/Edge install, or to
the binary pointed at by the WKHTMLTOPDF_CHROME environment variable.
EOF
    echo "- $target: no upstream build, wrote NOTICE.txt"
    continue
  fi

  stamp="$dest/.version"
  if [[ -f "$stamp" && "$(cat "$stamp")" == "$VERSION" ]]; then
    echo "- $target: already at $VERSION, skipping"
    continue
  fi

  echo "- $target: downloading chrome-headless-shell-$cft.zip"
  tmp="$(mktemp -d)"
  curl -fSL --progress-bar -o "$tmp/shell.zip" \
    "$DL_BASE/$VERSION/$cft/chrome-headless-shell-$cft.zip"
  unzip -q "$tmp/shell.zip" -d "$tmp"

  rm -rf "$shell_dir"
  mkdir -p "$(dirname "$shell_dir")"
  mv "$tmp/chrome-headless-shell-$cft" "$shell_dir"

  bin="$shell_dir/chrome-headless-shell"
  [[ "$target" == windows_* ]] && bin+=".exe"
  [[ -f "$bin" ]] || { echo "error: $bin missing after extraction" >&2; exit 1; }
  chmod +x "$bin" 2>/dev/null || true

  echo "$VERSION" >"$stamp"
  rm -rf "$tmp"
done

echo "done: $DEST_ROOT"
