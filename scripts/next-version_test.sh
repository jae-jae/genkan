#!/usr/bin/env bash
# Table test for next-version.sh. Run: bash scripts/next-version_test.sh
set -u
cd "$(dirname "$0")"
fail=0
check() { # expected tag bump
  local got
  got=$(./next-version.sh "$2" "$3" 2>/dev/null) || got="<error>"
  if [ "$got" != "$1" ]; then echo "FAIL: next-version.sh '$2' $3 -> $got, want $1"; fail=1; fi
}
check v1.0.0 "" patch
check v1.0.0 "" major
check v1.0.1 v1.0.0 patch
check v1.1.0 v1.0.0 minor
check v2.0.0 v1.0.0 major
check v1.3.0 v1.2.9 minor
check v2.0.0 v1.9.9 major
check v1.0.10 v1.0.9 patch
check "<error>" v1.0 patch
check "<error>" v1.0.0-rc.1 patch
check "<error>" latest patch
check "<error>" v1.0.0 huge
check "<error>" v01.0.0 patch
[ "$fail" = 0 ] && echo "next-version: all cases pass"
exit "$fail"
