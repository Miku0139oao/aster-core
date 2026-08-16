#!/bin/sh

set -eu

version=${1:?Go version is required}

# CI always runs on linux/amd64 hosts. Download the patched host toolchain
# by release name so we do not depend on stale GitHub asset IDs.
case "$version" in
  1.20|1.21|1.22|1.23|1.24|1.25|1.26)
    url="https://github.com/MetaCubeX/go/releases/download/build/go${version}.linux-amd64.tar.gz"
    ;;
  loong64-abi1)
    url="https://github.com/MetaCubeX/loongarch64-golang/releases/download/go1.24.4-abi1/go1.24.4.linux-amd64.tar.gz"
    ;;
  *)
    echo "Unsupported patched Go version: $version" >&2
    exit 1
    ;;
esac

archive="${RUNNER_TEMP:?}/aster-go-${version}.tar.gz"
install_dir="${RUNNER_TEMP}/aster-go-${version}"

set -- \
  --fail \
  --location \
  --proto '=https' \
  --tlsv1.2
if [ -n "${GITHUB_TOKEN:-}" ]; then
  set -- "$@" --header "Authorization: Bearer ${GITHUB_TOKEN}"
fi

curl "$@" --output "$archive" "$url"

mkdir "$install_dir"
tar -xzf "$archive" -C "$install_dir"
test -x "$install_dir/go/bin/go"
printf '%s\n' "$install_dir/go/bin" >> "${GITHUB_PATH:?}"
