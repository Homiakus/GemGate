#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:?VERSION is required}"
OUT_DIR="${OUT_DIR:-dist}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-946684800}"
LDFLAGS="-s -w -buildid= -X main.version=${VERSION}"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

targets=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "windows arm64"
)

for target in "${targets[@]}"; do
  read -r goos goarch <<<"$target"
  name="gemgate_${VERSION}_${goos}_${goarch}"
  tmp="$(mktemp -d)"
  binary="gemgate"
  if [[ "$goos" == "windows" ]]; then
    binary="gemgate.exe"
  fi

  echo "Building ${goos}/${goarch}..."
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$tmp/$binary" ./cmd/gemgate

  # Normalize file timestamps so archive metadata does not depend on runner time.
  touch -d "@${SOURCE_DATE_EPOCH}" "$tmp/$binary"

  if [[ "$goos" == "windows" ]]; then
    (cd "$tmp" && zip -X -q "$OLDPWD/$OUT_DIR/${name}.zip" "$binary")
  else
    tar --sort=name \
      --mtime="@${SOURCE_DATE_EPOCH}" \
      --owner=0 --group=0 --numeric-owner \
      -C "$tmp" -cf - "$binary" | gzip -n >"$OUT_DIR/${name}.tar.gz"
  fi
  rm -rf "$tmp"
done

printf 'Built release archives in %s\n' "$OUT_DIR"
ls -lh "$OUT_DIR"
