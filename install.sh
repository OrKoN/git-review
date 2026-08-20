#!/bin/sh
set -eu

repository=${GIT_REVIEW_REPOSITORY:-OrKoN/git-review}
version=${GIT_REVIEW_VERSION:-latest}
install_dir=${GIT_REVIEW_INSTALL_DIR:-"${HOME}/.local/bin"}
role=${1:-}

case "$role" in
  hub) programs="git-review-hub" ;;
  vm) programs="git-review git-repo-server" ;;
  *)
    echo "usage: $0 {hub|vm}" >&2
    exit 2
    ;;
esac

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$os" = darwin ] && { [ "$arch" != amd64 ] || [ "$role" != hub ]; }; then
  echo "only the Intel Mac hub is currently released for macOS" >&2
  exit 1
fi

case "$version" in
  latest) download_base="https://github.com/$repository/releases/latest/download" ;;
  *) download_base="https://github.com/$repository/releases/download/$version" ;;
esac

mkdir -p "$install_dir"
temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

for program in $programs; do
  asset="$program-$os-$arch"
  echo "Installing $asset"
  curl -fL "$download_base/$asset" -o "$temporary_dir/$program"
  chmod 0755 "$temporary_dir/$program"
  mv "$temporary_dir/$program" "$install_dir/$program"
  echo "Installed $install_dir/$program"
done

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to PATH before running git-review." ;;
esac
