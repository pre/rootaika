#!/usr/bin/env bash
# Creates a DRAFT GitHub release pinned to the current HEAD, with release notes
# generated from conventional commits since the previous v* tag (grouped into
# Features / Fixes / Tests / Chores / Other). Review the draft in the browser and
# press "Publish release": publishing creates the tag, which triggers
# .github/workflows/release.yml to build and attach the artifacts.
#
# Usage: scripts/github-release.sh <version-tag> [--dry-run]
#        --dry-run prints the generated notes without touching GitHub.
set -euo pipefail

REPO="pre/rootaika"

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <version-tag> [--dry-run]   e.g. $0 v1.2.3" >&2
  exit 2
fi
VERSION="$1"
DRY_RUN="${2:-}"

if [[ ! "$VERSION" =~ ^v[0-9] ]]; then
  echo "version must look like v1.2.3, got: $VERSION" >&2
  exit 2
fi

HEAD_SHA="$(git rev-parse HEAD)"
PREV_TAG="$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || true)"
RANGE="${PREV_TAG:+$PREV_TAG..}HEAD"
SUBJECTS="$(git log --no-merges --pretty='%s' "$RANGE")"

NOTES=""
section() { # append a "## title" section with subjects matching the prefix regex
  local title="$1" re="$2" body
  body="$(grep -E "$re" <<<"$SUBJECTS" | sed -E 's/^[a-z]+(\([^)]*\))?!?: */- /' || true)"
  [[ -n "$body" ]] && NOTES+="## $title"$'\n'"$body"$'\n\n'
  return 0
}
section "Features" '^feat(\(|!|:)'
section "Fixes"    '^fix(\(|!|:)'
section "Tests"    '^test(\(|!|:)'
section "Chores"   '^chore(\(|!|:)'
OTHER="$(grep -Ev '^(feat|fix|test|chore)(\(|!|:)' <<<"$SUBJECTS" | sed 's/^/- /' || true)"
[[ -n "$OTHER" ]] && NOTES+="## Other"$'\n'"$OTHER"$'\n\n'
NOTES+="Commits: ${PREV_TAG:-start}..$VERSION ($HEAD_SHA)"

if [[ "$DRY_RUN" == "--dry-run" ]]; then
  printf -- '--- draft notes for %s (target %s) ---\n%s\n' "$VERSION" "$HEAD_SHA" "$NOTES"
  exit 0
fi

command -v gh >/dev/null || { echo "gh CLI is required" >&2; exit 1; }

# The draft is pinned to HEAD, so HEAD must exist on origin and the tag be free.
git fetch -q origin
if ! git merge-base --is-ancestor "$HEAD_SHA" origin/main; then
  echo "HEAD ($HEAD_SHA) is not on origin/main; push first." >&2
  exit 1
fi
if [[ -n "$(git ls-remote --tags origin "refs/tags/$VERSION")" ]]; then
  echo "tag $VERSION already exists on origin" >&2
  exit 1
fi

URL="$(gh release create "$VERSION" \
  --repo "$REPO" \
  --draft \
  --target "$HEAD_SHA" \
  --title "$VERSION" \
  --notes "$NOTES")"

cat >&2 <<EOF

Draft release created (invisible to others until published):

  $URL

Review the notes, then press "Publish release" in the browser. Publishing
creates tag $VERSION at $HEAD_SHA and triggers the build workflow, which
attaches rootaika.exe (+ .sha256, install.ps1) and appends the admin triple.
EOF
