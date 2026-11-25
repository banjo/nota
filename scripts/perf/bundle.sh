#!/bin/sh
# Builds apps/web and prints raw + gzip sizes of the JS/CSS it ships
# (lazy language chunks of @codemirror/language-data excluded: they load
# only when a code block uses that language).
set -e
cd "$(dirname "$0")/../.."
rm -rf dist/app
bunx vp run @nota/web#build >/dev/null
cd dist/app/assets
total=0; gz=0
printf '%-40s %10s %10s\n' file bytes gzip
for f in $(ls -S *.js *.css); do
  b=$(wc -c < "$f"); g=$(gzip -9 -c "$f" | wc -c)
  total=$((total + b)); gz=$((gz + g))
  [ "$b" -gt 20000 ] && printf '%-40s %10d %10d\n' "$f" "$b" "$g"
done
printf '%-40s %10d %10d\n' "ALL js+css ($(ls *.js *.css | wc -l) files)" "$total" "$gz"
# What the first page load fetches: index.html's script/link tags.
init=0; initgz=0
for f in $(grep -oE 'assets/[^"]+\.(js|css)' ../index.html | sed 's#assets/##' | sort -u); do
  init=$((init + $(wc -c < "$f"))); initgz=$((initgz + $(gzip -9 -c "$f" | wc -c)))
done
printf '%-40s %10d %10d\n' "INITIAL (index.html)" "$init" "$initgz"
