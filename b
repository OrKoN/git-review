#!/bin/sh
set -eu

case "${1:-}" in
  build)
    exec bazel build //:build
    ;;
  test)
    bazel test //cmd/... //internal/...
    exec bazel build //:frontend_checks
    ;;
  release)
    bazel build //:release
    mkdir -p bin
    bazel cquery //:release --output=files | while IFS= read -r output; do
      case "$output" in
        bazel-out/*/bin/*)
          artifact=${output##*/}
          destination=${artifact%_*}-${artifact##*_}
          temporary="bin/$destination.tmp.$$"
          cp "$output" "$temporary"
          mv "$temporary" "bin/$destination"
          ;;
      esac
    done
    release_os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$(uname -m)" in
      x86_64) release_arch=amd64 ;;
      aarch64 | arm64) release_arch=arm64 ;;
      *) release_arch= ;;
    esac
    if [ -n "$release_arch" ]; then
      for program in git-review git-repo-server git-review-hub; do
        native="bin/$program-$release_os-$release_arch"
        if [ -f "$native" ]; then
          temporary="bin/$program.tmp.$$"
          cp "$native" "$temporary"
          mv "$temporary" "bin/$program"
        fi
      done
    fi
    ;;
  *)
    echo "usage: ./b {build|test|release}" >&2
    exit 2
    ;;
esac
