#!/bin/sh
# pal installer — downloads the release binary for this machine.
#
#   curl -fsSL https://raw.githubusercontent.com/binRick/pal/main/install.sh | sh
#
# Environment:
#   PAL_VERSION       release tag to install, e.g. v0.2.1   (default: latest)
#   PAL_INSTALL_DIR   where to put the binary               (default: ~/.local/bin)
#   PAL_BASE_URL      download location override, for mirrors and testing
set -eu

REPO="binRick/pal"

say()  { printf '%s\n' "$*"; }
fail() { printf 'pal install: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Linux*)               os=linux ;;
  Darwin*)              os=darwin ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) fail "unsupported system: $(uname -s). Build from source: https://github.com/$REPO" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported CPU: $(uname -m). Build from source: https://github.com/$REPO" ;;
esac

asset="pal-$os-$arch"
name="pal"
if [ "$os" = windows ]; then
  [ "$arch" = amd64 ] || fail "no Windows build for $arch. Build from source: https://github.com/$REPO"
  asset="$asset.exe"
  name="pal.exe"
fi

version="${PAL_VERSION:-latest}"
if [ -n "${PAL_BASE_URL:-}" ]; then
  base="$PAL_BASE_URL"
elif [ "$version" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$version"
fi

dir="${PAL_INSTALL_DIR:-$HOME/.local/bin}"

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q "$1" -O "$2"; }
else
  fail "need curl or wget"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading $asset ($version)..."
fetch "$base/$asset" "$tmp/$asset" || fail "download failed: $base/$asset"

# Verify against the release's checksums when it publishes them.
if fetch "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  want="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")"
  if command -v sha256sum >/dev/null 2>&1; then
    got="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    got="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
  else
    got=""
  fi
  if [ -z "$want" ] || [ -z "$got" ]; then
    say "Checksum not verified (no entry for $asset, or no sha256 tool)."
  elif [ "$want" != "$got" ]; then
    fail "checksum mismatch for $asset: expected $want, got $got"
  else
    say "Checksum OK."
  fi
else
  say "Checksum not verified (this release publishes none)."
fi

mkdir -p "$dir" || fail "cannot create $dir"
chmod 755 "$tmp/$asset"
mv "$tmp/$asset" "$dir/$name" || fail "cannot write $dir/$name (set PAL_INSTALL_DIR to a directory you can write)"

say "Installed $dir/$name"
case ":$PATH:" in
  *":$dir:"*) say "Run: pal" ;;
  *) say "$dir is not on your PATH. Add it:"
     say "  export PATH=\"$dir:\$PATH\"" ;;
esac
