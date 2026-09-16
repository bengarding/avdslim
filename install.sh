#!/usr/bin/env bash
#
# AVD-SLIM installer.
#
# Downloads the pinned release tarball and verifies its SHA-256 against the
# checksums.txt published in the same release before anything is extracted or
# executed. Refuses to install on any mismatch.
#
# Override the version with AVDSLIM_VERSION, the install prefix with
# AVDSLIM_INSTALL_DIR.
#
set -euo pipefail

REPO="${AVDSLIM_REPO:-kdbhalala/avdslim}"
VERSION="${AVDSLIM_VERSION:-v1.0.5}"

die() {
  echo "❌ $*" >&2
  exit 1
}

# ------------------------------------------------------------------------------
# Platform detection
# ------------------------------------------------------------------------------
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin) OS_NAME="darwin" ;;
  Linux)  OS_NAME="linux" ;;
  *)      die "Unsupported OS: $OS (avdslim ships darwin and linux builds only)" ;;
esac

case "$ARCH" in
  x86_64|amd64)  ARCH_NAME="amd64" ;;
  arm64|aarch64) ARCH_NAME="arm64" ;;
  *)             die "Unsupported architecture: $ARCH" ;;
esac

TARBALL="avdslim_${VERSION}_${OS_NAME}_${ARCH_NAME}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

# ------------------------------------------------------------------------------
# Pick a SHA-256 tool. Bail out rather than install unverified.
# ------------------------------------------------------------------------------
if command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$1" | awk '{print $1}'; }
elif command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$1" | awk '{print $1}'; }
else
  die "Neither shasum nor sha256sum found. Refusing to install without checksum verification."
fi

echo "⚡ Installing AVD-SLIM (${VERSION}) for ${OS_NAME}/${ARCH_NAME}..."

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
chmod 700 "$WORKDIR"

fetch() {
  # $1 = remote filename, $2 = local destination
  curl -fsSL --proto '=https' --tlsv1.2 --connect-timeout 10 --retry 3 \
    "${BASE_URL}/$1" -o "$2"
}

# ------------------------------------------------------------------------------
# Download artifact + checksum manifest
# ------------------------------------------------------------------------------
fetch "$TARBALL" "$WORKDIR/$TARBALL" \
  || die "Could not download $TARBALL from release $VERSION."
fetch "checksums.txt" "$WORKDIR/checksums.txt" \
  || die "Could not download checksums.txt for release $VERSION. Refusing to install unverified binary."

# ------------------------------------------------------------------------------
# Verify BEFORE extracting. A tarball is untrusted input until this passes.
# ------------------------------------------------------------------------------
# Normalise the filename column before comparing. Different generators emit
# different decoration for the same file: "name", "./name" (shasum on ./*.glob)
# and "*name" (sha256sum binary mode) all refer to the same artifact, and a
# mismatch here would look like a missing checksum entry.
EXPECTED="$(awk -v f="$TARBALL" '
  {
    name = $2
    sub(/^\.\//, "", name)
    sub(/^\*/, "", name)
    if (name == f) { print $1; exit }
  }
' "$WORKDIR/checksums.txt")"
[ -n "$EXPECTED" ] \
  || die "No checksum entry for $TARBALL in checksums.txt. Refusing to install."

# Reject a malformed manifest rather than comparing against garbage.
case "$EXPECTED" in
  [0-9a-fA-F]*) [ "${#EXPECTED}" -eq 64 ] || die "Malformed checksum for $TARBALL: $EXPECTED" ;;
  *)            die "Malformed checksum for $TARBALL: $EXPECTED" ;;
esac

ACTUAL="$(sha256_of "$WORKDIR/$TARBALL")"

# Case-insensitive compare; some tools emit uppercase hex.
if [ "$(printf '%s' "$EXPECTED" | tr 'A-F' 'a-f')" != "$(printf '%s' "$ACTUAL" | tr 'A-F' 'a-f')" ]; then
  echo "❌ CHECKSUM MISMATCH — aborting install." >&2
  echo "   expected: $EXPECTED" >&2
  echo "   actual:   $ACTUAL" >&2
  echo "   The download was corrupted or tampered with. Do not run this binary." >&2
  exit 1
fi
echo "🔒 SHA-256 verified: $ACTUAL"

# ------------------------------------------------------------------------------
# Extract into an isolated dir and use the exact expected path, so a tarball
# containing extra or oddly-named files cannot influence what gets installed.
# ------------------------------------------------------------------------------
EXTRACT_DIR="$WORKDIR/extract"
mkdir -p "$EXTRACT_DIR"
tar -xzf "$WORKDIR/$TARBALL" -C "$EXTRACT_DIR" \
  || die "Failed to extract $TARBALL."

BIN_PATH="$EXTRACT_DIR/avdslim_${VERSION}_${OS_NAME}_${ARCH_NAME}/avdslim"
if [ ! -f "$BIN_PATH" ]; then
  die "Archive layout unexpected: $BIN_PATH not found. Refusing to guess which file to install."
fi
[ ! -L "$BIN_PATH" ] || die "Refusing to install a symlink."

# ------------------------------------------------------------------------------
# Choose install dir. Prefer an explicit override, then a user-owned dir.
# ------------------------------------------------------------------------------
if [ -n "${AVDSLIM_INSTALL_DIR:-}" ]; then
  DEST="$AVDSLIM_INSTALL_DIR"
  mkdir -p "$DEST"
elif [ -d "/opt/homebrew/bin" ] && [ -w "/opt/homebrew/bin" ]; then
  DEST="/opt/homebrew/bin"
elif [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
  DEST="/usr/local/bin"
else
  DEST="$HOME/.local/bin"
  mkdir -p "$DEST"
fi
[ -w "$DEST" ] || die "Install directory $DEST is not writable."

install -m 0755 "$BIN_PATH" "$DEST/avdslim" \
  || die "Failed to install to $DEST/avdslim"

echo "✅ AVD-SLIM installed to $DEST/avdslim"
"$DEST/avdslim" version

case ":$PATH:" in
  *":$DEST:"*) ;;
  *) echo "⚠️  $DEST is not on your PATH. Add it to your shell profile." ;;
esac
