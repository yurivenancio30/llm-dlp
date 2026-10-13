#!/bin/sh
# llm-dlp installer.
#
# Downloads the release binary for this machine, checks its SHA-256 against the checksums
# published with the release and runs "llm-dlp install" (the 5-step setup, which asks before
# changing anything). If llm-dlp is already installed in ~/.local/bin, it runs
# "llm-dlp update" instead: it swaps the binary and the proxy that is running without taking
# llm-dlp offline (no setup questions, no need to close Claude Code).
#
#   curl -fsSL https://raw.githubusercontent.com/yurivenancio30/llm-dlp/main/install.sh | sh
#
# Options (pass them after "sh -s --") or the matching environment variable:
#
#   --version X.Y.Z   install that version instead of the latest      LLM_DLP_VERSION
#   --no-setup        only place the binary, do not run the setup     LLM_DLP_NO_SETUP=1
#   --bin-dir DIR     where --no-setup places the binary              LLM_DLP_BIN_DIR
#                     (default: ~/.local/bin)
#
# Releases up to 0.2.0 published bare binaries; this script installs 0.2.1 and later.
set -eu

REPO="yurivenancio30/llm-dlp"
VERSION="${LLM_DLP_VERSION:-}"
NO_SETUP="${LLM_DLP_NO_SETUP:-}"
BIN_DIR="${LLM_DLP_BIN_DIR:-$HOME/.local/bin}"

fail() {
	echo "llm-dlp install: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "'$1' is required but was not found"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || fail "--version needs a value"
		VERSION="$2"
		shift 2
		;;
	--bin-dir)
		[ $# -ge 2 ] || fail "--bin-dir needs a value"
		BIN_DIR="$2"
		shift 2
		;;
	--no-setup)
		NO_SETUP=1
		shift
		;;
	-h | --help)
		sed -n '2,16s/^# \{0,1\}//p' "$0" 2>/dev/null || true
		exit 0
		;;
	*) fail "unknown option: $1" ;;
	esac
done

need curl
need tar
need uname

[ "$(uname -s)" = Linux ] || fail "only Linux (including WSL) has release binaries; on other systems build from source: https://github.com/$REPO#installation"

case "$(uname -m)" in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) fail "no release binary for $(uname -m); build from source: https://github.com/$REPO#installation" ;;
esac

if [ -z "$NO_SETUP" ] && [ "$(id -u)" -eq 0 ]; then
	fail "run it without sudo: the installation belongs to your user (the setup asks for sudo only where it needs it)"
fi

if [ -z "$VERSION" ]; then
	# the "latest" page redirects to .../releases/tag/vX.Y.Z (no API call, no rate limit)
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		fail "could not reach github.com to find the latest version"
	VERSION="${latest##*/}"
fi
VERSION="${VERSION#v}"
case "$VERSION" in
[0-9]*.[0-9]*.[0-9]*) ;;
*) fail "could not find the version to install (got '$VERSION')" ;;
esac

ARCHIVE="llm-dlp_${VERSION}_linux_${ARCH}.tar.gz"
BASE="https://github.com/$REPO/releases/download/v$VERSION"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "Downloading llm-dlp $VERSION (linux/$ARCH)..."
curl -fsSL -o "$TMP/$ARCHIVE" "$BASE/$ARCHIVE" || fail "could not download $BASE/$ARCHIVE"
curl -fsSL -o "$TMP/checksums.txt" "$BASE/checksums.txt" || fail "could not download $BASE/checksums.txt"

expected=$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "$TMP/checksums.txt")
[ -n "$expected" ] || fail "$ARCHIVE is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$TMP/$ARCHIVE" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{ print $1 }')
else
	fail "'sha256sum' or 'shasum' is required to check the download"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $ARCHIVE (expected $expected, got $actual)"
echo "Checksum OK."

tar -xzf "$TMP/$ARCHIVE" -C "$TMP" llm-dlp
chmod 0755 "$TMP/llm-dlp"

# Commands are in English since 0.9.0. Releases before it only know the Portuguese names
# ("ajuda", "instalar", "atualizar"...), which 0.9.0 and later still accept.
english() { "$1" ajuda 2>/dev/null | grep -q "llm-dlp update"; }
# newer A B: version A is newer than version B
newer() {
	awk -v a="$1" -v b="$2" 'BEGIN { split(a, x, "."); split(b, y, ".")
		for (i = 1; i <= 3; i++) { if (x[i] + 0 > y[i] + 0) exit 0; if (x[i] + 0 < y[i] + 0) exit 1 }
		exit 1 }'
}

# Already installed: update in place (versions without "atualizar" fall back to the setup).
# Going back to an older version, the installed one runs the update when it knows how: it
# also converts the configuration to the field names the older version reads.
INSTALLED="$HOME/.local/bin/llm-dlp"
if [ -z "$NO_SETUP" ] && [ -x "$INSTALLED" ]; then
	current=$("$INSTALLED" versao 2>/dev/null | awk '{ print $2 }') || current=""
	if [ -n "$current" ] && newer "$current" "$VERSION" && english "$INSTALLED"; then
		echo
		"$INSTALLED" update --file "$TMP/$ARCHIVE"
		exit $?
	elif english "$TMP/llm-dlp"; then
		echo
		"$TMP/llm-dlp" update --file "$TMP/$ARCHIVE"
		exit $?
	elif "$TMP/llm-dlp" ajuda 2>/dev/null | grep -q "llm-dlp atualizar"; then
		echo
		"$TMP/llm-dlp" atualizar --arquivo "$TMP/$ARCHIVE"
		exit $?
	fi
fi
SETUP=instalar
if english "$TMP/llm-dlp"; then
	SETUP=install
fi

# The setup asks questions, and with "curl | sh" the standard input is the script itself:
# answer from the terminal. Without a terminal (CI, a provisioning script), only place the
# binary.
if [ -z "$NO_SETUP" ] && (exec </dev/tty) 2>/dev/null; then
	echo
	"$TMP/llm-dlp" "$SETUP" </dev/tty
	exit $?
fi

mkdir -p "$BIN_DIR"
# write next to the target and rename: works even while the old binary is running
cp "$TMP/llm-dlp" "$BIN_DIR/llm-dlp.new"
mv -f "$BIN_DIR/llm-dlp.new" "$BIN_DIR/llm-dlp"
echo "Installed $("$BIN_DIR/llm-dlp" versao) at $BIN_DIR/llm-dlp"
case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) echo "Note: $BIN_DIR is not in your PATH." ;;
esac
echo "To finish (configuration, OCR, Claude Code and the lock), run:  $BIN_DIR/llm-dlp $SETUP"
