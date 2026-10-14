package main

import (
	"os"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// O status avisa quando o processo no ar não é o binário instalado.
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
