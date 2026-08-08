#!/usr/bin/env bash
# setup.sh — interactive installer for bilihtmltopdf, the drop-in
# wkhtmltopdf replacement.
#
#   curl -fsSL https://raw.githubusercontent.com/rvanbaalen/bilihtmltopdf/main/setup.sh | bash
#
# Every step explains what it is about to do and asks before doing it.
# Non-interactive use (CI): BILI_YES=1 curl ... | bash   (answers yes to all)
set -euo pipefail

REPO="rvanbaalen/bilihtmltopdf"
# Override via env for custom locations, e.g. per-user installs:
#   BILI_INSTALL_ROOT=$HOME/.local/opt BILI_BIN_LINK=$HOME/.local/bin/wkhtmltopdf
INSTALL_ROOT="${BILI_INSTALL_ROOT:-/opt}"
BIN_LINK="${BILI_BIN_LINK:-/usr/local/bin/wkhtmltopdf}"

# ── helpers ──────────────────────────────────────────────────────────

say()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
info() { printf '  %s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

# Piped installs (`curl | bash`) have the script on stdin, so prompts
# must read the terminal directly.
confirm() {
  local q="$1" a
  if [[ "${BILI_YES:-0}" == "1" ]]; then
    info "$q [auto-yes]"
    return 0
  fi
  # -r /dev/tty is true even without a controlling terminal; only an
  # actual open attempt reveals whether prompting can work.
  if ! { printf '  %s [y/N] ' "$q" >/dev/tty; } 2>/dev/null; then
    die "no terminal available for confirmation; re-run with BILI_YES=1 to accept all steps"
  fi
  read -r a </dev/tty 2>/dev/null ||
    die "no terminal available for confirmation; re-run with BILI_YES=1 to accept all steps"
  [[ "$a" == "y" || "$a" == "Y" ]]
}

SUDO=""
as_root() {
  if [[ -n "$SUDO" ]]; then $SUDO "$@"; else "$@"; fi
}

# ── 1. detect platform and version ───────────────────────────────────

say "bilihtmltopdf installer"
info "A drop-in replacement for the wkhtmltopdf CLI, rendering with a"
info "bundled headless Chromium for modern CSS support."
info "Project: https://github.com/$REPO"

case "$(uname -s)" in
  Linux)  GOOS="linux" ;;
  Darwin) GOOS="darwin" ;;
  *) die "unsupported OS: $(uname -s) — use the release archives at https://github.com/$REPO/releases" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  GOARCH="amd64" ;;
  aarch64|arm64) GOARCH="arm64" ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need curl
need tar

VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
  sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' | head -1)"
[[ -n "$VERSION" ]] || die "could not determine the latest release version"

ASSET="bilihtmltopdf_${VERSION}_${GOOS}_${GOARCH}.tar.gz"
DEST="$INSTALL_ROOT/bilihtmltopdf_${VERSION}_${GOOS}_${GOARCH}"

say "Install plan"
info "Detected platform : $GOOS/$GOARCH"
info "Version to install: $VERSION"
info "Download          : https://github.com/$REPO/releases/download/v$VERSION/$ASSET"
info "Install directory : $DEST"
info "Command symlink   : $BIN_LINK -> $DEST/wkhtmltopdf"
if [[ "$GOOS/$GOARCH" == "darwin/amd64" ]]; then
  info "Note: darwin/amd64 archives are not published; Apple Silicon only."
fi
confirm "Continue with this plan?" || { info "aborted, nothing changed."; exit 0; }

# sudo is only needed when the target locations are not writable.
needs_root() {
  [[ ! -w "$INSTALL_ROOT" && ! -w "$(dirname "$INSTALL_ROOT")" ]] && return 0
  local bindir; bindir="$(dirname "$BIN_LINK")"
  [[ -d "$bindir" && ! -w "$bindir" ]] && return 0
  [[ ! -d "$bindir" && ! -w "$(dirname "$bindir")" ]] && return 0
  [[ ( -e "$BIN_LINK" || -L "$BIN_LINK" ) && ! -w "$(dirname "$BIN_LINK")" ]] && return 0
  return 1
}
if [[ $(id -u) -ne 0 ]] && needs_root; then
  command -v sudo >/dev/null 2>&1 || die "not root and sudo not available"
  SUDO="sudo"
  say "Privileges"
  info "Writing to $INSTALL_ROOT and $BIN_LINK requires sudo; you may be"
  info "asked for your password by sudo itself."
  confirm "Allow the installer to use sudo for those steps?" || { info "aborted, nothing changed."; exit 0; }
fi

# ── 2. handle an existing wkhtmltopdf ────────────────────────────────

if [[ -e "$BIN_LINK" || -L "$BIN_LINK" ]]; then
  say "Existing wkhtmltopdf found at $BIN_LINK"
  CURRENT_VERSION="$("$BIN_LINK" --version 2>/dev/null | head -1 || true)"
  info "Current version reports: ${CURRENT_VERSION:-unknown}"
  if [[ "$CURRENT_VERSION" == bilihtmltopdf* ]]; then
    info "That is a previous bilihtmltopdf install; it will be replaced."
    confirm "Replace it with $VERSION?" || { info "aborted, nothing changed."; exit 0; }
    as_root rm -f "$BIN_LINK"
  else
    info "It will be renamed to wkhtmltopdf.orig so you can roll back with:"
    info "  sudo rm $BIN_LINK && sudo mv $BIN_LINK.orig $BIN_LINK"
    confirm "Rename the existing binary to wkhtmltopdf.orig?" || { info "aborted, nothing changed."; exit 0; }
    as_root mv "$BIN_LINK" "$BIN_LINK.orig"
  fi
fi

# ── 3. download and install ──────────────────────────────────────────

say "Downloading $ASSET"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -fL --progress-bar -o "$TMP/$ASSET" \
  "https://github.com/$REPO/releases/download/v$VERSION/$ASSET"

say "Installing"
info "Extracting to $DEST and linking $BIN_LINK"
confirm "Proceed?" || { info "aborted; download discarded, nothing changed."; exit 0; }
as_root mkdir -p "$INSTALL_ROOT"
as_root rm -rf "$DEST"
as_root tar xzf "$TMP/$ASSET" -C "$INSTALL_ROOT"
as_root mkdir -p "$(dirname "$BIN_LINK")"
as_root ln -sf "$DEST/wkhtmltopdf" "$BIN_LINK"

# ── 4. runtime libraries: install only what is actually missing ─────
# The bundled renderer links against a handful of system libraries
# (NSS for TLS, GBM, expat, ...). Rather than guessing, ask the linker.

SHELL_BIN="$DEST/lib/chrome-headless-shell/chrome-headless-shell"
if [[ "$GOOS" == "linux" && -f "$SHELL_BIN" ]] && command -v ldd >/dev/null 2>&1; then
  MISSING="$(ldd "$SHELL_BIN" 2>/dev/null | awk '/not found/{print $1}' | sort -u || true)"
  if [[ -n "$MISSING" ]]; then
    say "Missing runtime libraries"
    info "The bundled renderer needs system libraries this machine lacks:"
    while IFS= read -r lib; do info "  - $lib"; done <<<"$MISSING"
    if command -v apt-get >/dev/null 2>&1; then
      info "The following usually covers all of them on Debian/Ubuntu:"
      info "  apt-get install -y libnss3 libnspr4 libgbm1 libexpat1 libxkbcommon0 fontconfig"
      if confirm "Run that apt-get install now?"; then
        as_root apt-get update
        as_root apt-get install -y libnss3 libnspr4 libgbm1 libexpat1 libxkbcommon0 fontconfig
        MISSING="$(ldd "$SHELL_BIN" 2>/dev/null | awk '/not found/{print $1}' | sort -u || true)"
        if [[ -n "$MISSING" ]]; then
          info "Still missing: $(echo "$MISSING" | tr '\n' ' ')"
          info "Find the package per library with: apt-file search <name> (or packages.ubuntu.com)."
        fi
      else
        info "Skipped. Rendering will fail until these libraries are installed."
      fi
    else
      info "Install them with your distribution's package manager."
    fi
  else
    say "Runtime libraries"
    info "All libraries the renderer needs are already present — nothing to install."
  fi
fi

# ── 5. verify ────────────────────────────────────────────────────────

say "Verifying"
INSTALLED="$("$BIN_LINK" --version)"
info "$BIN_LINK --version -> $INSTALLED"
[[ "$INSTALLED" == "bilihtmltopdf $VERSION" ]] || die "unexpected version output"

if confirm "Run a test conversion (renders a small HTML page to /tmp/bilihtmltopdf-test.pdf)?"; then
  printf '<h1 style="color:green">bilihtmltopdf works</h1>' > "$TMP/test.html"
  "$BIN_LINK" -q "$TMP/test.html" /tmp/bilihtmltopdf-test.pdf
  info "Wrote /tmp/bilihtmltopdf-test.pdf — open it to confirm."
fi

say "Done"
info "Installed : $INSTALLED"
info "Uninstall : sudo rm $BIN_LINK && sudo rm -rf $DEST"
if [[ -e "$BIN_LINK.orig" ]]; then
  info "Rollback  : sudo rm $BIN_LINK && sudo mv $BIN_LINK.orig $BIN_LINK"
fi
info "Docs      : https://github.com/$REPO#readme"
