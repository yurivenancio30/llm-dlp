package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Os nomes em português de antes da 0.9.0 levam a um comando que existe (na ajuda, ou entre os
// internos), e a ajuda não mostra nenhum deles.
func TestNomesAntigosDosComandos(t *testing.T) {
	ajuda := textoAjuda()
	for velho, novo := range nomesAntigos {
		if !strings.Contains(ajuda, "llm-dlp "+novo) && !strings.Contains(ajuda, novo+",") && !strings.HasSuffix(strings.TrimSpace(ajuda), novo) {
			t.Errorf("%s -> %s: o nome novo não está na ajuda", velho, novo)
		}
		if strings.Contains(ajuda, "llm-dlp "+velho+" ") || strings.Contains(ajuda, "llm-dlp "+velho+"\n") {
			t.Errorf("a ajuda ainda mostra o nome antigo %q", velho)
		}
	}
}

func lerHooks(t *testing.T, alvo string) string {
	t.Helper()
	b, err := os.ReadFile(alvo)
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	json.Unmarshal(b, &s)
	h, _ := json.Marshal(s["hooks"])
	return string(h)
}

// install troca os hooks antigos pelos novos (sem duplicar e sem mexer nos de outros
// programas); update para uma versão anterior põe os antigos de volta; uninstall tira os dois.
func TestHooksNomesNovosEAntigos(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // uninstall usa a home do usuário: nunca o ~/.claude de verdade
	t.Setenv("LLM_DLP_HOME", filepath.Join(home, "cfg"))
	bin := filepath.Join(home, ".local", "bin", "llm-dlp")
	alvo := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(alvo), 0o700)
	antes := `{"hooks": {
	  "SessionStart": [{"hooks": [{"type": "command", "command": "` + bin + ` garantir", "timeout": 20}]},
	                   {"hooks": [{"type": "command", "command": "outro-programa iniciar"}]}],
	  "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "` + bin + ` verificar", "timeout": 20}]}]}}`
	os.WriteFile(alvo, []byte(antes), 0o600)
	cfg := config.Padrao()

	if _, err := aplicarClaude(home, bin, cfg); err != nil {
		t.Fatal(err)
	}
	h := lerHooks(t, alvo)
	if strings.Contains(h, " garantir") || strings.Contains(h, " verificar") {
		t.Errorf("install deixou hooks antigos: %s", h)
	}
	if strings.Count(h, bin+" ensure") != 1 || strings.Count(h, bin+" check") != 1 || !strings.Contains(h, "outro-programa") {
		t.Errorf("install: esperava ensure e check uma vez cada, e o hook do outro programa: %s", h)
	}
	if _, err := aplicarClaude(home, bin, cfg); err != nil {
		t.Fatal(err)
	}
	if h2 := lerHooks(t, alvo); strings.Count(h2, bin+" ensure") != 1 {
		t.Errorf("install de novo duplicou: %s", h2)
	}

	if mudou, err := hooksParaVersao(home, bin, "0.8.0"); err != nil || !mudou {
		t.Fatalf("voltar para a 0.8.0: mudou=%v err=%v", mudou, err)
	}
	h = lerHooks(t, alvo)
	if !strings.Contains(h, bin+" garantir") || !strings.Contains(h, bin+" verificar") || strings.Contains(h, " ensure") {
		t.Errorf("voltar para a 0.8.0 devia pôr os nomes antigos: %s", h)
	}
	if mudou, _ := hooksParaVersao(home, bin, "0.8.0"); mudou {
		t.Errorf("já estava com os nomes da 0.8.0: não devia mexer")
	}

	if err := desinstalar(nil); err != nil {
		t.Fatal(err)
	}
	h = lerHooks(t, alvo)
	if strings.Contains(h, "llm-dlp") || !strings.Contains(h, "outro-programa") {
		t.Errorf("uninstall: devia tirar só os do llm-dlp: %s", h)
	}
}
