#!/bin/sh

set -eu

version=${1:?Go version is required}
repository=MetaCubeX/go

case "$version" in
  1.20)
    asset_id=469676095
    sha256=8bac4d785a08c337897eaa6a41a23142c3c614716368bc27d972562a6223d7dd
    ;;
  1.21)
    asset_id=469676084
    sha256=94c98f3e3aeee7f126182307c935683d5754e8c7f87fdd224e78261a627668a3
    ;;
  1.22)
    asset_id=469676085
    sha256=1bd97bfca7689ca3b194b1fd1d71162da6f80d9aecaab7e4dfbfb49eb68a1820
    ;;
  1.23)
    asset_id=469676074
    sha256=91278d7ab671319f93cc18cc213e6141d55533becd8509378f6be6a4c592845d
    ;;
  1.24)
    asset_id=469676060
    sha256=289416ea5f9e1330a8392a7be56b3d7e07cb0ca51e526f5d71648a624314d0c1
    ;;
  1.25)
    asset_id=469676050
    sha256=3694f128bb44448869d71e56b46a3cf04d527a7494d0aa091b1761a93048823a
    ;;
  1.26)
    asset_id=469676048
    sha256=03a2db2ecd724909798e8742cfcd5973f7a6eb6bb240854c7d599743e684922e
    ;;
  loong64-abi1)
    repository=MetaCubeX/loongarch64-golang
    asset_id=382204273
    sha256=46af28946a57d4a33fe7b478728267f0fb734d893b2d857dbcd485aa7cb08974
    ;;
  *)
    echo "Unsupported patched Go version: $version" >&2
    exit 1
    ;;
esac

archive="${RUNNER_TEMP:?}/aster-go-${version}.tar.gz"
install_dir="${RUNNER_TEMP}/aster-go-${version}"
url="https://api.github.com/repos/${repository}/releases/assets/${asset_id}"

set -- \
  --fail \
  --location \
  --proto '=https' \
  --tlsv1.2 \
  --header 'Accept: application/octet-stream' \
  --header 'X-GitHub-Api-Version: 2022-11-28'
if [ -n "${GITHUB_TOKEN:-}" ]; then
  set -- "$@" --header "Authorization: Bearer ${GITHUB_TOKEN}"
fi

curl "$@" --output "$archive" "$url"
printf '%s  %s\n' "$sha256" "$archive" | sha256sum --check --status

mkdir "$install_dir"
tar -xzf "$archive" -C "$install_dir"
test -x "$install_dir/go/bin/go"
printf '%s\n' "$install_dir/go/bin" >> "${GITHUB_PATH:?}"
