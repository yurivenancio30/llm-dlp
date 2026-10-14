#!/usr/bin/env bash
# Releases the version in internal/version/version.go: checks, tests and creates the tag vX.Y.Z.
# Sends nothing: at the end, it shows the push command. See docs/versioning.md.
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { echo "release: $*" >&2; exit 1; }

V=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/version/version.go)
[[ $V =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "invalid version in internal/version/version.go: '$V'"
TAG="v$V"

[[ $(git branch --show-current) == main ]] || fail "a version is released from the main branch (you are on $(git branch --show-current))"
[[ -z $(git status --porcelain) ]] || fail "there are uncommitted changes"
git rev-parse -q --verify "refs/tags/$TAG" >/dev/null && fail "tag $TAG already exists: raise the version in version.go"
# the CHANGELOG exists in English (which becomes the release notes) and in Portuguese
for c in CHANGELOG.md docs/pt-BR/CHANGELOG.md; do
  grep -q "^## \[$V\] - [0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}$" "$c" || fail "$c without the section '## [$V] - YYYY-MM-DD'"
done
[[ -n $(./scripts/release-notes.sh "$V") ]] || fail "the $V section of CHANGELOG.md is empty"

echo "== tests"
make test
# GitHub builds and publishes the archives; here only the configuration is checked, if possible
if command -v goreleaser >/dev/null; then
  echo "== goreleaser check"
  goreleaser check
fi

git tag -s "$TAG" -m "llm-dlp $V"
echo
echo "Tag $TAG created. To publish (GitHub builds the release with the archives):"
echo "  git push origin main $TAG"
