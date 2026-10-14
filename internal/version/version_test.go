package version

import (
	"os"
	"regexp"
	"testing"
)

// The version is written in two places a person reads (version.go and CHANGELOG.md): they must say
// the same, and the version must have a section in the CHANGELOG before it is released.
func TestVersionMatchesChangelog(t *testing.T) {
	if !regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`).MatchString(Version) {
		t.Fatalf("Version = %q: the format is MAJOR.MINOR.PATCH", Version)
	}
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindSubmatch(b)
	if m == nil {
		t.Fatal("CHANGELOG.md without any version section (## [X.Y.Z] - YYYY-MM-DD)")
	}
	if string(m[1]) != Version {
		t.Fatalf("version.go says %s and the first version in CHANGELOG.md is %s", Version, m[1])
	}
}
