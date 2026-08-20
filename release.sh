#!/bin/sh
set -eu

tag=${1:-}
repository=${GIT_REVIEW_REPOSITORY:-OrKoN/git-review}
target=${GIT_REVIEW_RELEASE_TARGET:-main}

case "$tag" in
  v[0-9]*) ;;
  *) echo "usage: $0 v<version>" >&2; exit 2 ;;
esac

if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI (gh) is required: https://cli.github.com/" >&2
  exit 1
fi

if ! terminal_settings=$(stty -g </dev/tty 2>/dev/null); then
  echo "an interactive terminal is required to enter the GitHub token" >&2
  exit 1
fi

restore_terminal() {
  stty "$terminal_settings" </dev/tty
}

trap 'restore_terminal' EXIT
trap 'restore_terminal; exit 130' HUP INT TERM
printf "GitHub personal access token: " >/dev/tty
stty -echo </dev/tty
IFS= read -r personal_access_token </dev/tty
restore_terminal
printf "\n" >/dev/tty
trap - EXIT HUP INT TERM

if [ -z "$personal_access_token" ]; then
  echo "a GitHub personal access token is required" >&2
  exit 1
fi
GH_TOKEN=$personal_access_token
export GH_TOKEN
unset personal_access_token

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir"

gh auth status
./b release

set -- \
  bin/git-review-linux-amd64 \
  bin/git-review-linux-arm64 \
  bin/git-repo-server-linux-amd64 \
  bin/git-repo-server-linux-arm64 \
  bin/git-review-hub-darwin-amd64 \
  bin/git-review-hub-linux-amd64 \
  bin/git-review-hub-linux-arm64 \
  LICENSE \
  internal/licenses/THIRD_PARTY_LICENSES.txt

for asset in "$@"; do
  if [ ! -f "$asset" ]; then
    echo "missing release asset: $asset" >&2
    exit 1
  fi
  case "$asset" in
    bin/*)
      if [ ! -x "$asset" ]; then
        echo "release binary is not executable: $asset" >&2
        exit 1
      fi
      ;;
  esac
done

gh release create "$tag" "$@" \
  --repo "$repository" \
  --target "$target" \
  --title "$tag" \
  --generate-notes \
  --fail-on-no-commits
