#!/bin/sh
set -eu

version=${VERSION:-dev}
out_dir=${OUT_DIR:-dist}
ldflags=${LDFLAGS:--s -w -X main.version=$version}

case $version in
  ''|*[!A-Za-z0-9._+-]*)
    printf 'invalid VERSION for artifact names: %s\n' "$version" >&2
    exit 2
    ;;
esac

rm -rf "$out_dir"
mkdir -p "$out_dir"
out_dir=$(cd "$out_dir" && pwd)
build_root=$(mktemp -d "${TMPDIR:-/tmp}/viagh-release.XXXXXX")
trap 'rm -rf "$build_root"' EXIT HUP INT TERM

for target in \
  linux/amd64 \
  linux/arm64 \
  darwin/amd64 \
  darwin/arm64 \
  windows/amd64 \
  windows/arm64
do
  goos=${target%/*}
  goarch=${target#*/}
  artifact="viagh_${version}_${goos}_${goarch}"
  package_dir="$build_root/$artifact"
  mkdir -p "$package_dir"

  binary=viagh
  if [ "$goos" = windows ]; then
    binary=viagh.exe
  fi

  printf 'building %s/%s\n' "$goos" "$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$ldflags" -o "$package_dir/$binary" ./cmd/viagh

  if [ "$goos" = windows ]; then
    (
      cd "$package_dir"
      zip -q -X "$out_dir/$artifact.zip" "$binary"
    )
  else
    chmod 0755 "$package_dir/$binary"
    tar -C "$package_dir" -czf "$out_dir/$artifact.tar.gz" "$binary"
  fi
done

(
  cd "$out_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum viagh_*.tar.gz viagh_*.zip > SHA256SUMS
  else
    shasum -a 256 viagh_*.tar.gz viagh_*.zip > SHA256SUMS
  fi
)
