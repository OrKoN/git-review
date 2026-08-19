#!/bin/sh
set -eu

version=1.26.5
archive=go${version}.linux-amd64.tar.gz
checksum=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_dir=$(dirname -- "$script_dir")
target=$repository_dir/.tools/go

if [ "$(uname -s)" != Linux ] || [ "$(uname -m)" != x86_64 ]; then
  echo "This pinned installer supports Linux amd64 only." >&2
  exit 1
fi
if [ -e "$target" ]; then
  echo "$target already exists; refusing to overwrite it." >&2
  exit 1
fi

mkdir -p "$repository_dir/.tools"
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
curl --fail --location --proto '=https' --tlsv1.2 \
  --output "$temporary/$archive" "https://go.dev/dl/$archive"
printf '%s  %s\n' "$checksum" "$temporary/$archive" | sha256sum --check
tar -C "$repository_dir/.tools" -xzf "$temporary/$archive"
"$target/bin/go" version
