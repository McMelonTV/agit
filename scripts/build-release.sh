#!/bin/sh
set -eu

version=${VERSION:-dev}
out_dir=${OUT_DIR:-dist}
ldflags=${LDFLAGS:--s -w -X main.version=$version}

rm -rf "$out_dir"
mkdir -p "$out_dir"

for target in \
  linux/amd64 \
  linux/arm64 \
  darwin/amd64 \
  darwin/arm64 \
  windows/amd64
do
  goos=${target%/*}
  goarch=${target#*/}
  extension=
  if [ "$goos" = windows ]; then
    extension=.exe
  fi
  output="$out_dir/ghapp_${version}_${goos}_${goarch}${extension}"
  printf 'building %s/%s\n' "$goos" "$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$ldflags" -o "$output" ./cmd/ghapp
done

(
  cd "$out_dir"
  sha256sum ghapp_* > SHA256SUMS
)
