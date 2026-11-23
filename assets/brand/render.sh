#!/bin/sh
# Renders the raster files that are generated from the SVGs in this
# directory. Needs rsvg-convert (librsvg2-bin); ImageMagick's own SVG
# renderer drops gradients and filters. Run from the repo root:
#   sh assets/brand/render.sh [review-dir]
# With a review dir it also writes the icon at 1024/512/256/128/64/32/16
# and the favicon at 16/32 (and 16 upscaled 16x) there, for looking at.
set -eu
cd "$(dirname "$0")/../.."
b=assets/brand

# Desktop app icon: MyGo takes resources/icon.png (1024 square) and makes
# the .icns, the Linux desktop entry icon and the Windows icon from it.
cp $b/icon.svg apps/desktop/resources/icon.svg
rsvg-convert -w 1024 -h 1024 $b/icon.svg -o apps/desktop/resources/icon.png

# Favicons: the simplified mark on a flat tile, legible at 16 px.
cp $b/favicon.svg apps/web/public/favicon.svg
cp $b/favicon.svg apps/landing/public/icon.svg
rsvg-convert -w 180 -h 180 $b/touch-icon.svg -o apps/landing/public/apple-touch-icon.png

if [ $# -gt 0 ]; then
  out=$1
  mkdir -p "$out"
  for s in 1024 512 256 128 64 32 16; do
    rsvg-convert -w $s -h $s $b/icon.svg -o "$out/icon-$s.png"
  done
  for s in 16 32 64; do
    rsvg-convert -w $s -h $s $b/favicon.svg -o "$out/favicon-$s.png"
  done
  for f in lockup-light lockup-dark; do
    rsvg-convert -h 128 $b/$f.svg -o "$out/$f.png"
  done
fi
