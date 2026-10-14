// Package version tells which code is running: the version (written here) and the commit (recorded
// at build time by the Makefile). "llm-dlp version" and /__llm-dlp/saude show both.
package version

// Version: the llm-dlp version, in the MAJOR.MINOR.PATCH format (see docs/versioning.md). It is the
// only source: the git tag (v0.2.0) and the first section of CHANGELOG.md must say the same, and
// TestVersionMatchesChangelog checks it.
const Version = "0.9.2"

// Commit: filled by -ldflags "-X .../internal/version.Commit=abc1234" (the Makefile does it).
// "-mod" at the end: built with uncommitted changes.
var Commit = "desconhecido"

// Complete: "0.2.0 (abc1234)".
func Complete() string { return Version + " (" + Commit + ")" }
