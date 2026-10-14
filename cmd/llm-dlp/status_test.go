package main

import (
	"os"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// status warns when the running process is not the installed binary.
func TestStatusWarnsNewBinary(t *testing.T) {
	if newBinary("outro", os.Getpid()) == "" {
		t.Error("commit diferente deveria avisar")
	}
	if newBinary("", 0) == "" {
		t.Error("processo sem commit (versão antiga) deveria avisar")
	}
	if a := newBinary(version.Commit, os.Getpid()); a != "" {
		t.Errorf("mesmo commit e executável intacto não deveria avisar: %s", a)
	}
}
