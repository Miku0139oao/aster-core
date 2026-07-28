#!/bin/sh

set -eu

version_range=
while getopts "v:" opt; do
  case "$opt" in
    v)
      version_range=$OPTARG
      ;;
    \?)
      echo "Invalid option: -$OPTARG" >&2
      exit 1
      ;;
  esac
done

if [ -z "$version_range" ]; then
  echo "Please provide the version range using -v. Example: ./genReleaseNote.sh -v v1.14.1...v1.14.2" >&2
  exit 1
fi

{
  echo "## What's Changed"
  git log --pretty=format:"* %h %s by @%an" --grep="^feat" -i "$version_range" | sort -f | uniq
  echo
  echo "## BUG & Fix"
  git log --pretty=format:"* %h %s by @%an" --grep="^fix" -i "$version_range" | sort -f | uniq
  echo
  echo "## Maintenance"
  git log --pretty=format:"* %h %s by @%an" --grep="^chore\|^docs\|^refactor" -i "$version_range" | sort -f | uniq
  echo
  echo "**Full Changelog**: https://github.com/Miku0139oao/aster-core/compare/$version_range"
} > release.md
