#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ "${SKIP_WEB:-}" != "1" ]; then
  echo "==> frontend"
  ( cd "$root/web"
    [ -d node_modules ] || pnpm install --frozen-lockfile
    pnpm exec tsc -b --force
    pnpm exec vite build )
fi

echo "==> binary"
goos="${GOOS:-$(go env GOHOSTOS)}"
goarch="${GOARCH:-$(go env GOHOSTARCH)}"
name="yoru"
if [ "$goos" != "$(go env GOHOSTOS)" ] || [ "$goarch" != "$(go env GOHOSTARCH)" ]; then
  name="yoru-$goos-$goarch"
fi
[ "$goos" = "windows" ] && name="$name.exe"
out="${OUT:-$root/$name}"
case $out in /*) ;; *) out="$PWD/$out" ;; esac

ldflags="-s -w"
[ "$goos" = "windows" ] && ldflags="$ldflags -H windowsgui"
[ -n "${VERSION:-}" ] && ldflags="$ldflags -X yoru/internal/update.Version=$VERSION"
CGO_ENABLED=0 go -C "$root" build -trimpath -ldflags "$ldflags" -o "$out" ./cmd/yoru
echo "    $out"
