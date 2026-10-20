#!/bin/sh
# Installs Yoru __VERSION__ as Yoru.app and opens it; fetched by curl it has no quarantine flag, so it opens.
#   curl -fsSL https://github.com/Tons-7/yoru/releases/latest/download/install-mac.sh | sh
set -eu

version="__VERSION__"
base="https://github.com/Tons-7/yoru/releases/download/v$version"
case "$(uname -m)" in
  arm64) arch=arm64 ;;
  x86_64) arch=amd64 ;;
  *) echo "Yoru: there is no build for this Mac ($(uname -m))." >&2; exit 1 ;;
esac
zip="Yoru-$version-mac-$arch.zip"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading Yoru $version for $arch..."
curl -fsSL -o "$tmp/$zip" "$base/$zip"
curl -fsSL -o "$tmp/SHA256SUMS.txt" "$base/SHA256SUMS.txt"
want="$(awk -v f="$zip" '$2 == f { print $1 }' "$tmp/SHA256SUMS.txt")"
got="$(shasum -a 256 "$tmp/$zip" | awk '{ print $1 }')"
if [ -z "$want" ] || [ "$want" != "$got" ]; then
  echo "Yoru: the download does not match the release's checksum; nothing was installed." >&2
  exit 1
fi
ditto -x -k "$tmp/$zip" "$tmp/app"

# /Applications when this account may write there (admins can), else the account's own Applications.
apps=/Applications
if [ ! -w "$apps" ]; then
  apps="$HOME/Applications"
  mkdir -p "$apps"
fi

# A running Yoru is stopped first, or the new one would hand its window to the old and quit.
if pgrep -x yoru >/dev/null 2>&1; then
  echo "Stopping the running Yoru..."
  pkill -INT -x yoru || true
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    pgrep -x yoru >/dev/null 2>&1 || break
    sleep 1
  done
fi

rm -rf "$apps/Yoru.app"
mv "$tmp/app/Yoru.app" "$apps/Yoru.app"
echo "Installed $apps/Yoru.app. Its files go in ~/Library/Application Support/Yoru."
open "$apps/Yoru.app"
