#!/usr/bin/env bash
# Wrap MyGo's Linux build of the desktop app in an AppImage.
#
#   scripts/appimage.sh <mygo linux build dir> <version> <output.AppImage>
#
# MyGo has no AppImage target, so this packs what `mygo build` wrote into
# build/linux-<arch>/ (the executable, desktop entry, icon) as an AppDir and
# runs appimagetool over it. The app is pure Go and opens WebKitGTK at run
# time, so the AppImage does not bundle it: like the .deb and the archive,
# it needs libwebkit2gtk-4.1 on the system (Debian/Ubuntu:
# `sudo apt install libwebkit2gtk-4.1-0`).
#
# APPIMAGETOOL may point at an appimagetool binary; otherwise the pinned
# release below is downloaded and checked against its SHA-256.
set -euo pipefail

src=${1:?usage: appimage.sh <build dir> <version> <output.AppImage>}
version=${2:?missing version}
out=${3:?missing output path}
slug=nota

case "$(uname -m)" in
  x86_64) arch=x86_64 ;;
  aarch64) arch=aarch64 ;;
  *) echo "appimage.sh: unsupported machine $(uname -m)" >&2; exit 1 ;;
esac

for f in "$src/$slug" "$src/$slug.desktop" "$src/$slug.png"; do
  [ -e "$f" ] || { echo "appimage.sh: $f is missing (run mygo build first)" >&2; exit 1; }
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
appdir="$work/Nota.AppDir"
mkdir -p "$appdir/usr/lib/$slug" "$appdir/usr/share/applications" \
  "$appdir/usr/share/icons/hicolor/512x512/apps"

# The app's files, as the archive has them, minus the packages and archives.
for f in "$src"/*; do
  case "$f" in
    *.deb | *.rpm | *.tar.gz | *.AppImage | */PKGBUILD | */SHA256SUMS | */install.sh) continue ;;
  esac
  cp -a "$f" "$appdir/usr/lib/$slug/"
done

# The desktop entry runs the executable through AppRun; keep the rest
# (name, icon, MimeType for nota://, StartupWMClass) as MyGo wrote it.
sed -E -e "s|^Exec=[^ ]*|Exec=$slug|" -e "s|^TryExec=.*||" "$src/$slug.desktop" > "$appdir/$slug.desktop"
grep -q '^X-AppImage-Version=' "$appdir/$slug.desktop" ||
  sed -i "/^\[Desktop Entry\]/a X-AppImage-Version=$version" "$appdir/$slug.desktop"
cp "$appdir/$slug.desktop" "$appdir/usr/share/applications/$slug.desktop"
cp "$src/$slug.png" "$appdir/$slug.png"
cp "$src/$slug.png" "$appdir/usr/share/icons/hicolor/512x512/apps/$slug.png"
ln -s "$slug.png" "$appdir/.DirIcon"

cat > "$appdir/AppRun" <<'EOF'
#!/bin/sh
here="$(dirname "$(readlink -f "$0")")"
exec "$here/usr/lib/nota/nota" "$@"
EOF
chmod +x "$appdir/AppRun"

tool=${APPIMAGETOOL:-}
if [ -z "$tool" ]; then
  # appimagetool 1.9.1 (github.com/AppImage/appimagetool), pinned by digest.
  case "$arch" in
    x86_64) sum=ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0 ;;
    aarch64) sum=f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158 ;;
  esac
  tool="$work/appimagetool"
  curl -fsSL -o "$tool" "https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-$arch.AppImage"
  echo "$sum  $tool" | sha256sum -c --quiet -
  chmod +x "$tool"
fi

# No FUSE on CI runners or in containers: run the tool from its extracted copy.
mkdir -p "$(dirname "$out")"
APPIMAGE_EXTRACT_AND_RUN=1 ARCH=$arch VERSION=$version "$tool" --no-appstream "$appdir" "$out"
chmod +x "$out"
echo "wrote $out ($(du -h "$out" | cut -f1))"
