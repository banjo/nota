#!/usr/bin/env bash
# Print the next release tag. Nota releases are patch bumps for now:
# no release yet -> v0.1.0, then v0.1.1, v0.1.2, ... (minor and major bumps
# only when decided). Prereleases (v0.1.1-rc.1) are ignored as a base.
#
#   scripts/next-version.sh          v0.1.1
#   scripts/next-version.sh --rc     v0.1.1-rc.1 (or the next free rc number)
#
# Reads the tags of origin (git fetch --tags first) so a stale clone does
# not offer a tag that already exists.
set -euo pipefail

rc=false
case "${1:-}" in
  --rc) rc=true ;;
  "") ;;
  *) echo "usage: next-version.sh [--rc]" >&2; exit 2 ;;
esac

git fetch --quiet --tags origin 2>/dev/null || echo "next-version: could not fetch tags, using local ones" >&2

latest=$(git tag -l 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)
if [ -z "$latest" ]; then
  next=v0.1.0
else
  IFS=. read -r major minor patch <<< "${latest#v}"
  next="v$major.$minor.$((patch + 1))"
fi

if $rc; then
  n=$(git tag -l "$next-rc.*" | sed -E "s/^.*-rc\.//" | grep -E '^[0-9]+$' | sort -n | tail -1 || true)
  next="$next-rc.$(( ${n:-0} + 1 ))"
fi
echo "$next"
