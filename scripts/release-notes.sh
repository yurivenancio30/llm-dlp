#!/usr/bin/env bash
# Prints the section of a version of CHANGELOG.md as the release notes on GitHub.
# Usage: release-notes.sh 0.2.0
#
# The release page shows every line break of the text as a line break, and does not resolve
# relative links. So each paragraph and each list item goes out on a single line (the CHANGELOG
# is wrapped at 95 columns), and links to repository files become links to the tag.
set -euo pipefail
cd "$(dirname "$0")/.."
V=${1:?usage: release-notes.sh X.Y.Z}
REPO=https://github.com/yurivenancio30/llm-dlp
awk -v v="$V" '
  function blanks()  { for (; pending > 0; pending--) print "" }
  function flush()   { if (buf != "") { print buf; buf = "" } }
  /^## \[/           { flush(); inside = (index($0, "## [" v "]") == 1); next }
  !inside            { next }
  /^```/             { flush(); blanks(); print; fence = !fence; started = 1; next }
  fence              { print; next }
  /^[[:space:]]*$/   { flush(); if (started) pending++; next }
  /^(#|\|)/          { flush(); blanks(); print; started = 1; next }   # heading and table row
  /^[[:space:]]*([-*+]|[0-9]+\.)[[:space:]]/ { flush(); blanks(); buf = $0; started = 1; next }
  buf != ""          { sub(/^[[:space:]]+/, ""); buf = buf " " $0; next }   # continuation
                     { blanks(); buf = $0; started = 1 }
  END                { flush() }
' CHANGELOG.md |
  sed -E "s#\]\((docs/|\.github/|README\.md|CHANGELOG\.md|install\.sh|LICENSE)#](${REPO}/blob/v${V}/\1#g"
cat <<END

---

**Install or update:** \`curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh\` (if llm-dlp is already installed, it is updated without going offline and without closing Claude Code). The archives below are checked against \`checksums.txt\`.

[Changelog in Portuguese](${REPO}/blob/v${V}/docs/pt-BR/CHANGELOG.md) · [All versions](${REPO}/blob/main/CHANGELOG.md)
END
