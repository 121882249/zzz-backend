#!/bin/sh
set -eu

# Resolve the TokenPro server version.
#
# Usage: resolve-version.sh [candidate]
#
# Accepted forms, with one optional leading v on a semver core:
#   X.Y.Z
#   X.Y.Z-tp.N
#   TokenPro-R<N>-vX.Y.Z
#
# Precedence: a valid candidate, then an exact valid git tag, then
# cmd/server/VERSION. A build label such as PR7-a34e450 is rejected so Docker
# falls through to the VERSION file instead of embedding a non-semver stamp.
#
# Future builds: bump tp.N in cmd/server/VERSION for a TokenPro release that
# stays on the same upstream. When merging a newer upstream tag, set the file
# to X.Y.Z-tp.1 and update tokenProUpstreamVersion. Pass the same string as
# the Docker VERSION build-arg. See TOKENPRO_CUSTOMIZATIONS.md.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BACKEND_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(CDPATH= cd -- "$BACKEND_DIR/.." && pwd)"
VERSION_FILE="$BACKEND_DIR/cmd/server/VERSION"

is_tokenpro_version() {
  printf '%s\n' "$1" | grep -Eq '^(v?[0-9]+\.[0-9]+\.[0-9]+(-tp\.[0-9]+)?|TokenPro-R[0-9]+-v[0-9]+\.[0-9]+\.[0-9]+)$'
}

normalize_version() {
  case "$1" in
    v[0-9]*) printf '%s' "${1#v}" ;;
    *) printf '%s' "$1" ;;
  esac
}

candidate="${1:-}"
if [ -n "$candidate" ] && is_tokenpro_version "$candidate"; then
  printf '%s\n' "$(normalize_version "$candidate")"
  exit 0
fi

# Prefer the exact release tag when building from a tagged checkout so
# source builds from vX.Y.Z don't inherit the previous VERSION file value.
if command -v git >/dev/null 2>&1; then
  TAG="$(
    git -C "$REPO_DIR" describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || \
    git -C "$REPO_DIR" describe --tags --exact-match --match '[0-9]*' 2>/dev/null || \
    true
  )"
  if [ -n "$TAG" ] && is_tokenpro_version "$TAG"; then
    printf '%s\n' "$(normalize_version "$TAG")"
    exit 0
  fi
fi

file_version="$(tr -d '\r\n' < "$VERSION_FILE")"
if is_tokenpro_version "$file_version"; then
  printf '%s\n' "$(normalize_version "$file_version")"
  exit 0
fi

echo "invalid TokenPro version: candidate=${candidate:-<empty>} file=${file_version:-<empty>}" >&2
exit 1
