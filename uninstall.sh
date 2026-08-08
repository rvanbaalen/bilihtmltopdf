#!/usr/bin/env bash
# uninstall.sh — interactive uninstaller for bilihtmltopdf.
#
#   curl -fsSL https://raw.githubusercontent.com/rvanbaalen/bilihtmltopdf/main/uninstall.sh | bash
#
# Removes the wkhtmltopdf symlink and the /opt install directories, and
# restores a wkhtmltopdf.orig backup if the installer created one.
# Every step explains itself and asks first. Non-interactive: BILI_YES=1.
set -euo pipefail

# Same override points as setup.sh; must match the values used to install.
INSTALL_ROOT="${BILI_INSTALL_ROOT:-/opt}"
BIN_LINK="${BILI_BIN_LINK:-/usr/local/bin/wkhtmltopdf}"

say()  { printf '\n\033[1m%s\033[0m\n' "$*"; }
info() { printf '  %s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

confirm() {
  local q="$1" a
  if [[ "${BILI_YES:-0}" == "1" ]]; then
    info "$q [auto-yes]"
    return 0
  fi
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

# ── 1. discover what is installed ────────────────────────────────────

say "bilihtmltopdf uninstaller"

LINK_STATE="absent"
LINK_TARGET=""
if [[ -L "$BIN_LINK" ]]; then
  LINK_TARGET="$(readlink "$BIN_LINK")"
  case "$LINK_TARGET" in
    "$INSTALL_ROOT"/bilihtmltopdf_*) LINK_STATE="ours" ;;
    *) LINK_STATE="foreign" ;;
  esac
elif [[ -e "$BIN_LINK" ]]; then
  if "$BIN_LINK" --version 2>/dev/null | head -1 | grep -q '^bilihtmltopdf'; then
    LINK_STATE="ours"
  else
    LINK_STATE="foreign"
  fi
fi

shopt -s nullglob
INSTALL_DIRS=("$INSTALL_ROOT"/bilihtmltopdf_*)
shopt -u nullglob

BACKUP=""
[[ -e "$BIN_LINK.orig" ]] && BACKUP="$BIN_LINK.orig"

if [[ "$LINK_STATE" == "absent" && ${#INSTALL_DIRS[@]} -eq 0 && -z "$BACKUP" ]]; then
  info "Nothing to do: no bilihtmltopdf symlink, install directories, or"
  info "backup found at $BIN_LINK / $INSTALL_ROOT."
  exit 0
fi

say "Uninstall plan"
case "$LINK_STATE" in
  ours)    info "Remove command      : $BIN_LINK${LINK_TARGET:+ -> $LINK_TARGET}" ;;
  foreign) info "Keep command        : $BIN_LINK is not bilihtmltopdf; it will NOT be touched" ;;
  absent)  info "Command             : $BIN_LINK not present" ;;
esac
if [[ ${#INSTALL_DIRS[@]} -gt 0 ]]; then
  info "Remove install dirs : ${INSTALL_DIRS[*]}"
else
  info "Install dirs        : none found under $INSTALL_ROOT"
fi
if [[ -n "$BACKUP" ]]; then
  if [[ "$LINK_STATE" == "foreign" ]]; then
    info "Backup              : $BACKUP exists but $BIN_LINK is already a"
    info "                      non-bilihtmltopdf binary; the backup stays untouched."
  else
    info "Restore original    : $BACKUP -> $BIN_LINK"
  fi
else
  info "Restore original    : no $BIN_LINK.orig backup found, nothing to restore"
fi
confirm "Continue with this plan?" || { info "aborted, nothing changed."; exit 0; }

# sudo only when the affected locations are not writable.
needs_root() {
  local d
  for d in "${INSTALL_DIRS[@]}"; do
    [[ ! -w "$(dirname "$d")" ]] && return 0
  done
  [[ ( -e "$BIN_LINK" || -L "$BIN_LINK" || -n "$BACKUP" ) && ! -w "$(dirname "$BIN_LINK")" ]] && return 0
  return 1
}
if [[ $(id -u) -ne 0 ]] && needs_root; then
  command -v sudo >/dev/null 2>&1 || die "not root and sudo not available"
  SUDO="sudo"
  say "Privileges"
  info "Removing from $INSTALL_ROOT and $(dirname "$BIN_LINK") requires sudo."
  confirm "Allow the uninstaller to use sudo for those steps?" || { info "aborted, nothing changed."; exit 0; }
fi

# ── 2. remove the command symlink ────────────────────────────────────

if [[ "$LINK_STATE" == "ours" ]]; then
  say "Removing $BIN_LINK"
  confirm "Remove the wkhtmltopdf command?" || { info "kept $BIN_LINK."; exit 0; }
  as_root rm -f "$BIN_LINK"
fi

# ── 3. restore the backed-up original ────────────────────────────────

if [[ -n "$BACKUP" && "$LINK_STATE" != "foreign" ]]; then
  say "Restoring original wkhtmltopdf"
  ORIG_VERSION="$("$BACKUP" --version 2>/dev/null | head -1 || echo unknown)"
  info "$BACKUP reports: $ORIG_VERSION"
  if confirm "Restore it to $BIN_LINK?"; then
    as_root mv "$BACKUP" "$BIN_LINK"
    info "Restored. $BIN_LINK --version -> $("$BIN_LINK" --version 2>/dev/null | head -1 || echo '?')"
  else
    info "Left the backup at $BACKUP."
  fi
fi

# ── 4. remove install directories ────────────────────────────────────

if [[ ${#INSTALL_DIRS[@]} -gt 0 ]]; then
  say "Removing install directories"
  for d in "${INSTALL_DIRS[@]}"; do
    if confirm "Remove $d?"; then
      as_root rm -rf "$d"
    else
      info "kept $d"
    fi
  done
fi

say "Done"
if [[ -e "$BIN_LINK" || -L "$BIN_LINK" ]]; then
  info "wkhtmltopdf now: $("$BIN_LINK" --version 2>/dev/null | head -1 || echo 'present but not runnable')"
else
  info "No wkhtmltopdf command remains at $BIN_LINK."
fi
