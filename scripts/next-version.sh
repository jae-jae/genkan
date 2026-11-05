#!/usr/bin/env bash
# Print the next release version.
#
#   next-version.sh <highest-tag|""> <patch|minor|major>
#
# The first argument is the highest existing release tag (vX.Y.Z), or empty when
# there is none, in which case the result is v1.0.0. The caller is responsible
# for picking the highest semver tag; anything that is not a plain vX.Y.Z is an
# error here.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <highest-tag|\"\"> <patch|minor|major>" >&2
  exit 2
fi
tag=$1
bump=$2

case "$bump" in
  patch | minor | major) ;;
  *)
    echo "next-version: bump must be patch, minor or major, got '$bump'" >&2
    exit 2
    ;;
esac

if [ -z "$tag" ]; then
  echo "v1.0.0"
  exit 0
fi

if ! [[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "next-version: '$tag' is not a vX.Y.Z tag" >&2
  exit 2
fi
major=${BASH_REMATCH[1]}
minor=${BASH_REMATCH[2]}
patch=${BASH_REMATCH[3]}

case "$bump" in
  major) echo "v$((major + 1)).0.0" ;;
  minor) echo "v${major}.$((minor + 1)).0" ;;
  patch) echo "v${major}.${minor}.$((patch + 1))" ;;
esac
