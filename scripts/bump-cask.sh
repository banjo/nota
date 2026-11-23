#!/usr/bin/env bash
# Point the Homebrew cask (banjo/homebrew-tap, Casks/nota.rb) at a release.
#
#   scripts/bump-cask.sh 0.1.0                 clone the tap, write, commit, push
#   scripts/bump-cask.sh 0.1.0 --tap ~/code/homebrew-tap   use a checkout
#   scripts/bump-cask.sh 0.1.0 --dry-run [--out FILE]      only write FILE
#
# It downloads Nota-<version>-arm64.zip from the GitHub release v<version>,
# checks it against the release's checksums.txt, and renders
# packaging/homebrew/nota.rb with the version and SHA-256. Pushing needs a
# maintainer's own git/gh credentials; the release workflow never runs this.
set -euo pipefail

usage() { sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-2}"; }

version=""
tap=""
dry_run=false
out=""
allow_prerelease=false
while [ $# -gt 0 ]; do
  case "$1" in
    --tap) tap=${2:?--tap needs a directory}; shift 2 ;;
    --dry-run) dry_run=true; shift ;;
    --out) out=${2:?--out needs a file}; shift 2 ;;
    --allow-prerelease) allow_prerelease=true; shift ;;
    -h | --help) usage 0 ;;
    -*) echo "bump-cask: unknown flag $1" >&2; usage ;;
    *) [ -z "$version" ] || usage; version=${1#v}; shift ;;
  esac
done
[ -n "$version" ] || usage
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "bump-cask: '$version' is not a version like 0.1.0" >&2
  exit 2
fi
if [[ "$version" == *-* ]] && ! $dry_run && ! $allow_prerelease; then
  echo "bump-cask: $version is a prerelease; the cask tracks releases (use --dry-run, or --allow-prerelease)" >&2
  exit 2
fi

root=$(cd "$(dirname "$0")/.." && pwd)
template="$root/packaging/homebrew/nota.rb"
repo=banjo/nota
asset="Nota-${version}-arm64.zip"
base="https://github.com/$repo/releases/download/v$version"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "downloading $base/$asset"
curl -fsSL -o "$work/$asset" "$base/$asset"
curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt"
sha=$( (sha256sum "$work/$asset" 2>/dev/null || shasum -a 256 "$work/$asset") | cut -d' ' -f1)
want=$(awk -v f="$asset" '$2 == f || $2 == "*"f { print $1 }' "$work/checksums.txt")
if [ "$sha" != "$want" ]; then
  echo "bump-cask: $asset is $sha, but checksums.txt says '${want:-nothing}'" >&2
  exit 1
fi
echo "sha256 $sha (matches checksums.txt)"

render() {
  sed -e "s/@VERSION@/$version/" -e "s/@SHA256@/$sha/" \
    -e '/^# Template for Casks/d' -e '/^# scripts\/bump-cask.sh fills in/d' "$template" > "$1"
  ! grep -n '@[A-Z0-9]*@' "$1" || { echo "bump-cask: unfilled placeholder in $1" >&2; exit 1; }
}

check() {
  if command -v ruby > /dev/null; then ruby -c "$1"; fi
  if command -v brew > /dev/null; then brew style "$1"; fi
}

if $dry_run; then
  out=${out:-${TMPDIR:-/tmp}/nota.rb}
  mkdir -p "$(dirname "$out")"
  render "$out"
  check "$out"
  echo "wrote $out (dry run, nothing committed)"
  cat "$out"
  exit 0
fi

if [ -z "$tap" ]; then
  tap="$work/homebrew-tap"
  gh repo clone banjo/homebrew-tap "$tap" -- --depth 1
fi
git -C "$tap" pull --ff-only
mkdir -p "$tap/Casks"
render "$tap/Casks/nota.rb"
check "$tap/Casks/nota.rb"
git -C "$tap" add Casks/nota.rb
if git -C "$tap" diff --cached --quiet; then
  echo "Casks/nota.rb already points at $version"
  exit 0
fi
git -C "$tap" commit -m "nota $version"
git -C "$tap" push
echo "pushed Casks/nota.rb for $version"
