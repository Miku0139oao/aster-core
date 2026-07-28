#!/bin/sh

set -eu

for filename in *; do
  [ -e "$filename" ] || continue
  case "$filename" in
    *darwin-10.16-arm64*) target=aster-core-darwin-arm64-cgo ;;
    *darwin-10.16-amd64*) target=aster-core-darwin-amd64-cgo ;;
    *windows-4.0-386*) target=aster-core-windows-386-cgo.exe ;;
    *windows-4.0-amd64*) target=aster-core-windows-amd64-cgo.exe ;;
    *aster-core-linux-arm-5*) target=aster-core-linux-armv5-cgo ;;
    *aster-core-linux-arm-6*) target=aster-core-linux-armv6-cgo ;;
    *aster-core-linux-arm-7*) target=aster-core-linux-armv7-cgo ;;
    *linux*|*android*) target=$filename-cgo ;;
    *)
      echo "skip $filename"
      continue
      ;;
  esac
  echo "rename $filename to $target"
  mv -- "$filename" "$target"
done
