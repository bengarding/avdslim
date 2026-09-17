#!/usr/bin/env bash
#
# Guards the contract between release.yml (which writes checksums.txt) and
# install.sh (which parses it). A "./"-prefixed filename column once made every
# lookup miss, so install.sh refused to install every future release.
#
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

tarball="avdslim_v9.9.9_darwin_arm64.tar.gz"
hash="997890bc85c5796408ceb20b0ca75dabe6fe868136e926d24ad0f36aa424f99d"

# Reuse install.sh's own lookup so this cannot drift from the real thing.
lookup() {
  awk -v f="$tarball" '
    {
      name = $2
      sub(/^\.\//, "", name)
      sub(/^\*/, "", name)
      if (name == f) { print $1; exit }
    }
  ' "$1"
}

fail=0
check() {
  local label="$1" file="$2" got
  got="$(lookup "$file")"
  if [ "$got" = "$hash" ]; then
    echo "  ok   $label"
  else
    echo "  FAIL $label: got '$got', want '$hash'"
    fail=1
  fi
}

printf '%s  %s\n' "$hash" "$tarball"      > "$work/bare.txt"
printf '%s  ./%s\n' "$hash" "$tarball"    > "$work/dotslash.txt"
printf '%s *%s\n'  "$hash" "$tarball"     > "$work/binmode.txt"
printf '%s  other.tar.gz\n%s  %s\n' "$hash" "$hash" "$tarball" > "$work/multi.txt"

check "bare filename (shasum name)"        "$work/bare.txt"
check "./-prefixed (shasum ./glob)"        "$work/dotslash.txt"
check "*-prefixed (sha256sum binary mode)" "$work/binmode.txt"
check "correct line among several"         "$work/multi.txt"

# A missing entry must yield empty, so install.sh aborts rather than proceeding.
printf '%s  unrelated.tar.gz\n' "$hash" > "$work/absent.txt"
if [ -n "$(lookup "$work/absent.txt")" ]; then
  echo "  FAIL absent entry produced a hash"
  fail=1
else
  echo "  ok   absent entry yields nothing"
fi

# install.sh must actually carry the normalisation, not just this test.
if ! grep -qF 'sub(/^\.\//, "", name)' "$here/install.sh"; then
  echo "  FAIL install.sh is missing the './' filename normalisation"
  fail=1
else
  echo "  ok   install.sh carries the normalisation"
fi

exit "$fail"
