package main

import (
	"os"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/versao"
)

// O status avisa quando o processo no ar não é o binário instalado.
func TestStatusAvisaBinarioNovo(t *testing.T) {
	if binarioNovo("outro", os.Getpid()) == "" {
		t.Error("commit diferente deveria avisar")
	}
	if binarioNovo("", 0) == "" {
		t.Error("processo sem commit (versão antiga) deveria avisar")
	}
	if a := binarioNovo(versao.Commit, os.Getpid()); a != "" {
		t.Errorf("mesmo commit e executável intacto não deveria avisar: %s", a)
	}
}
