#!/usr/bin/env bash
# Install dynamic-pdb -- the Dynamic PDB CLI.
#
# Usage:
#   curl -fsSL https://dynamicpdb.com/install.sh | bash
#   curl -fsSL https://dynamicpdb.com/install.sh | bash -s -- --version v0.1.0
#
# Options:
#   --version VERSION   pin to a release tag (e.g. v0.1.0); default: latest
#   --dir DIR           install directory (default: auto -- first writable of
#                       /usr/local/bin, $HOME/.local/bin, $HOME/bin)
#   --help, -h          show this help and exit
#
# Environment overrides:
#   DYNAMIC_PDB_VERSION       same as --version
#   DYNAMIC_PDB_INSTALL_DIR   same as --dir
#   DYNAMIC_PDB_BASE_URL      base URL hosting /releases/... (default: https://dynamicpdb.com)

set -euo pipefail

BASE_URL="${DYNAMIC_PDB_BASE_URL:-https://dynamicpdb.com}"
INSTALL_DIR="${DYNAMIC_PDB_INSTALL_DIR:-}"
VERSION="${DYNAMIC_PDB_VERSION:-}"

red()  { printf '\033[31m%s\033[0m' "$*"; }
cyan() { printf '\033[36m%s\033[0m' "$*"; }
err()  { printf '%s %s\n' "$(red 'error:')" "$*" >&2; exit 1; }
info() { printf '%s %s\n' "$(cyan '==>')" "$*"; }

print_help() {
  cat <<'EOF'
Install dynamic-pdb -- the Dynamic PDB CLI.

Usage:
  curl -fsSL https://dynamicpdb.com/install.sh | bash
  curl -fsSL https://dynamicpdb.com/install.sh | bash -s -- --version v0.1.0

Options:
  --version VERSION   pin to a release tag (e.g. v0.1.0); default: latest
  --dir DIR           install directory (default: auto -- first writable of
                      /usr/local/bin, $HOME/.local/bin, $HOME/bin)
  --help, -h          this message

Environment overrides:
  DYNAMIC_PDB_VERSION, DYNAMIC_PDB_INSTALL_DIR, DYNAMIC_PDB_BASE_URL
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --dir)     INSTALL_DIR="$2"; shift 2 ;;
    --help|-h) print_help; exit 0 ;;
    *) err "unknown option: $1 (try --help)" ;;
  esac
done

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) ;;
  *) err "unsupported OS: $OS (Windows users: download manually from $BASE_URL/releases/)" ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) err "unsupported architecture: $ARCH" ;;
esac

for tool in curl tar; do
  command -v "$tool" >/dev/null || err "$tool is required but not on PATH"
done

if [ -z "$VERSION" ]; then
  info "resolving latest release from $BASE_URL"
  VERSION=$(curl -fsSL "$BASE_URL/releases/latest.txt" | tr -d '[:space:]' || true)
  [ -n "$VERSION" ] || err "could not resolve latest release from $BASE_URL/releases/latest.txt"
fi

writable_dir() {
  local d="$1"
  [ -d "$d" ] && [ -w "$d" ]
}

if [ -n "$INSTALL_DIR" ]; then
  [ -d "$INSTALL_DIR" ] || err "install dir does not exist: $INSTALL_DIR"
  [ -w "$INSTALL_DIR" ] || err "install dir $INSTALL_DIR is not writable. Pick a writable --dir or unset DYNAMIC_PDB_INSTALL_DIR for auto-detect."
else
  for candidate in /usr/local/bin "$HOME/.local/bin" "$HOME/bin"; do
    if writable_dir "$candidate"; then
      INSTALL_DIR="$candidate"
      break
    fi
  done
  if [ -z "$INSTALL_DIR" ]; then
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR" || err "could not create install dir $INSTALL_DIR"
  fi
  info "installing to $INSTALL_DIR"
fi

VERSION_NO_V="${VERSION#v}"
ARCHIVE="dynamic-pdb_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
RELEASE_URL="$BASE_URL/releases/$VERSION"
ARCHIVE_URL="$RELEASE_URL/$ARCHIVE"
SUMS_URL="$RELEASE_URL/checksums.txt"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

info "downloading $ARCHIVE_URL"
curl -fsSL "$ARCHIVE_URL" -o "$TMP/$ARCHIVE"

verify_checksum() {
  local file="$1" sums="$2" expected actual
  expected=$(grep " $(basename "$file")\$" "$sums" | awk '{print $1}' || true)
  [ -n "$expected" ] || { printf 'no checksum entry for %s\n' "$(basename "$file")" >&2; return 1; }
  if command -v sha256sum >/dev/null; then
    actual=$(sha256sum "$file" | awk '{print $1}')
  elif command -v shasum >/dev/null; then
    actual=$(shasum -a 256 "$file" | awk '{print $1}')
  else
    printf 'no sha256 tool available; skipping verification\n' >&2
    return 0
  fi
  [ "$expected" = "$actual" ] || { printf 'expected=%s actual=%s\n' "$expected" "$actual" >&2; return 1; }
}

if curl -fsSL "$SUMS_URL" -o "$TMP/checksums.txt"; then
  info "verifying checksum"
  verify_checksum "$TMP/$ARCHIVE" "$TMP/checksums.txt" || err "checksum mismatch for $ARCHIVE"
else
  printf 'warning: could not download checksums.txt; skipping verification\n' >&2
fi

info "extracting release"
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
[ -f "$TMP/dynamic-pdb" ] || err "dynamic-pdb binary not found in archive"

mv "$TMP/dynamic-pdb" "$INSTALL_DIR/dynamic-pdb"
chmod +x "$INSTALL_DIR/dynamic-pdb"
info "installed dynamic-pdb $VERSION to $INSTALL_DIR/dynamic-pdb"

"$INSTALL_DIR/dynamic-pdb" --version

case ":${PATH:-}:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf '\n%s %s is not on your PATH. Add it, e.g.:\n  export PATH="%s:$PATH"\n' \
       "$(cyan 'note:')" "$INSTALL_DIR" "$INSTALL_DIR" ;;
esac
