#!/bin/sh

set -eu

version_range=
max_entries=40
while getopts "v:m:" opt; do
  case "$opt" in
    v)
      version_range=$OPTARG
      ;;
    m)
      max_entries=$OPTARG
      ;;
    \?)
      echo "Invalid option: -$OPTARG" >&2
      exit 1
      ;;
  esac
done

case "$max_entries" in
  ''|*[!0-9]*)
    echo "-m must be a non-negative integer" >&2
    exit 1
    ;;
esac

if [ -z "$version_range" ]; then
  echo "Please provide the version range using -v and optionally a per-section cap with -m. Example: ./genReleaseNote.sh -v v1.14.1...v1.14.2 -m 40" >&2
  exit 1
fi

git_log() {
  git log --no-merges --pretty=format:"* %h %s by @%an" "$@"
}

# Classify by the subject prefix only. git log --grep searches the body,
# so a docs commit that mentioned "accuracy fixes" was listed under BUG & Fix.
# The parentheses around the pattern matter: without them
# "chore|docs|refactor|test|perf|ci" was an ungrouped alternation and
# matched anywhere in the line, so every fix commit that merely mentioned
# "test" or "docs" appeared a second time under Maintenance.
subject_grep() {
  pattern=$1
  git_log "$version_range" | grep -iE "^\* [0-9a-f]+ (${pattern})(\(|:)" || true
}

# Emit one section, capped at $max_entries entries. git log is
# newest-first, so the cap keeps the most recent commits and points at the
# Full Changelog for the rest. Without this the prerelease notes list
# everything since the Aster baseline, so they grow with every push to
# main and eventually trip the workflow's length guard.
# 0 disables the cap; the caller logs when entries were dropped.
emit_section() {
  heading=$1
  entries=$2
  echo "$heading"
  if [ -n "$entries" ]; then
    total=$(printf '%s\n' "$entries" | wc -l)
    if [ "$max_entries" -gt 0 ] && [ "$total" -gt "$max_entries" ]; then
      printf '%s\n' "$entries" | head -n "$max_entries"
      echo "* … and $((total - max_entries)) more (see Full Changelog)"
      echo "genReleaseNote: ${heading#\#\# } capped at ${max_entries} of ${total} entries" >&2
    else
      printf '%s\n' "$entries"
    fi
  fi
  echo
  echo
}

# GitHub's compare page takes the three-dot form. A two-dot range from
# the workflow (v1.19.29..<sha>) selects the same commits as a...b, so
# spell it the way the web UI parses it.
case "$version_range" in
  *...*)
    link_range=$version_range
    ;;
  *..*)
    link_range=$(printf '%s' "$version_range" | sed 's/\.\./.../')
    ;;
  *)
    link_range=$version_range
    ;;
esac

{
  emit_section "## What's Changed" "$(subject_grep 'feat')"
  emit_section "## BUG & Fix" "$(subject_grep 'fix')"
  emit_section "## Maintenance" "$(subject_grep 'chore|docs|refactor|test|perf|ci')"
  other="$(git_log "$version_range" | grep -ivE '^\* [0-9a-f]+ (feat|fix|chore|docs|refactor|test|perf|ci)(\(|:)' || true)"
  if [ -n "$other" ]; then
    emit_section "## Other" "$other"
  fi
  echo "**Full Changelog**: https://github.com/Miku0139oao/aster-core/compare/$link_range"
} > release.md
