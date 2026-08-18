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

git_log() {
  git log --no-merges --pretty=format:"* %h %s by @%an" "$@"
}

{
  echo "## What's Changed"
  git_log --grep="^feat" -i "$version_range"
  echo
  echo
  echo "## BUG & Fix"
  git_log --grep="^fix" -i "$version_range"
  echo
  echo
  echo "## Maintenance"
  git_log --grep="^chore\|^docs\|^refactor\|^test\|^perf\|^ci" -i "$version_range"
  echo
  echo
  other="$(git_log "$version_range" | grep -ivE '^\* [0-9a-f]+ (feat|fix|chore|docs|refactor|test|perf|ci)(\(|:)' || true)"
  if [ -n "$other" ]; then
    echo "## Other"
    printf '%s\n' "$other"
    echo
    echo
  fi
  echo "**Full Changelog**: https://github.com/Miku0139oao/aster-core/compare/$version_range"
} > release.md
