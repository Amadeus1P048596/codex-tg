#!/usr/bin/env bash
set -euo pipefail
export COPYFILE_DISABLE=1

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-"$ROOT_DIR/dist"}"
VERSION="${VERSION:-"$(git -C "$ROOT_DIR" describe --tags --always --dirty)"}"
PKG_VERSION="${VERSION#v}"
ARCHES="${MACOS_PKG_ARCHES:-"$(go env GOARCH)"}"

if ! command -v pkgbuild >/dev/null 2>&1; then
  echo "pkgbuild is required to build macOS installer packages" >&2
  exit 1
fi

mkdir -p "$DIST_DIR"

for arch in $ARCHES; do
  workdir="$(mktemp -d)"
  trap 'rm -rf "$workdir"' EXIT

  payload="$workdir/payload"
  app="$payload/Applications/codex-tg.app"
  mkdir -p "$payload/usr/local/bin" "$app/Contents/MacOS" "$app/Contents/Resources"

  env CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w" -buildvcs=false \
    -o "$app/Contents/MacOS/ctr-go" "$ROOT_DIR/cmd/ctr-go"

  env CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w" -buildvcs=false \
    -o "$app/Contents/MacOS/ctr-go-tray" "$ROOT_DIR/cmd/ctr-go-tray"

  # Keep the actual daemon inside the app for macOS privacy attribution.
  cat >"$payload/usr/local/bin/ctr-go" <<'SHIM'
#!/bin/sh
exec /Applications/codex-tg.app/Contents/MacOS/ctr-go "$@"
SHIM
  chmod 0755 "$payload/usr/local/bin/ctr-go" "$app/Contents/MacOS/ctr-go" "$app/Contents/MacOS/ctr-go-tray"
  cat >"$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>ctr-go-tray</string>
  <key>CFBundleIdentifier</key>
  <string>tech.mideco.codex-tg.tray</string>
  <key>CFBundleName</key>
  <string>codex-tg</string>
  <key>CFBundleDisplayName</key>
  <string>codex-tg</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>${PKG_VERSION}</string>
  <key>CFBundleVersion</key>
  <string>${PKG_VERSION}</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
PLIST

  find "$payload" -name '._*' -delete
  xattr -cr "$payload" 2>/dev/null || true

  # Sign nested executable first, then the app. No private signing material is
  # part of this repository; use the same distribution identity across upgrades.
  identity="${MACOS_CODESIGN_IDENTITY:--}"
  signing_args=(--force --sign "$identity")
  if [[ "$identity" != "-" ]]; then
    signing_args+=(--timestamp --options runtime)
  else
    echo "Development build: ad-hoc signing does not guarantee retained Full Disk Access." >&2
  fi
  codesign "${signing_args[@]}" --identifier tech.mideco.codex-tg.daemon "$app/Contents/MacOS/ctr-go"
  codesign "${signing_args[@]}" --identifier tech.mideco.codex-tg.tray "$app"
  xattr -cr "$payload"
  codesign --verify --deep --strict "$app"
  mkdir -p "$DIST_DIR/apps/$arch"
  ditto "$app" "$DIST_DIR/apps/$arch/codex-tg.app"

  installer_args=(pkgbuild)
  if [[ -n "${MACOS_INSTALLER_IDENTITY:-}" ]]; then
    installer_args+=(--sign "$MACOS_INSTALLER_IDENTITY")
  fi
  pkgbuild --analyze --root "$payload" "$workdir/components.plist"
  /usr/libexec/PlistBuddy -c 'Set :0:BundleIsRelocatable false' "$workdir/components.plist"
  "${installer_args[@]}" \
    --root "$payload" \
    --component-plist "$workdir/components.plist" \
    --identifier "tech.mideco.codex-tg" \
    --version "$PKG_VERSION" \
    --install-location "/" \
    "$DIST_DIR/codex-tg_${VERSION}_darwin_${arch}.pkg"

  rm -rf "$workdir"
  trap - EXIT
done
