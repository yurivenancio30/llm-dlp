package main

import (
	"os"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// status warns when the running process is not the installed binary.
func TestStatusWarnsNewBinary(t *testing.T) {
	if newBinary("outro", os.Getpid()) == "" {
		t.Error("a different commit should warn")
	}
	if newBinary("", 0) == "" {
		t.Error("process without a commit (old version) should warn")
	}
	if a := newBinary(version.Commit, os.Getpid()); a != "" {
		t.Errorf("same commit and intact executable should not warn: %s", a)
	}
}
