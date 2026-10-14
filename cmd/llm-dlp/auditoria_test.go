package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// O resumo do "llm-dlp audit" a partir de linhas de log como o proxy escreve.
func TestResumoDoLog(t *testing.T) {
	agora := time.Now()
	h := func(d time.Duration) string { return agora.Add(-d).Format("2006/01/02 15:04:05") + " " }
	linhas := []string{
		h(48*time.Hour) + "POST /v1/messages -> 200 | 9 substituições | 0 colisões | mascarar 1ms | total 2s | tipos: cpf 9",
		h(2*time.Hour) + "POST /v1/messages -> 200 | 5 substituições | 0 colisões | mascarar 1ms | total 2s | tipos: tabela 3, cpf 1, ip 1",
		h(time.Hour) + "POST /v1/messages -> 200 | 2 substituições | 1 colisões | mascarar 1ms | total 2s | tipos: cpf 2 | colididos: 241.7.9.x | NÃO DESFEITOS 2: CPF-abcdefgh, t_xyzwvuts",
		h(time.Hour) + "POST /v1/messages -> 529 | 0 substituições | 0 colisões | mascarar 1ms | total 2s | API: overloaded_error: Overloaded",
		h(30*time.Minute) + "AVISO POST /v1/messages: enviado sem máscara (falhou)",
		h(20*time.Minute) + `RECUSADO POST /v1/messages: não veio de um programa local (Host "x", Origin "")`,
		h(10*time.Minute) + "ERRO POST /v1/messages: upstream: timeout",
		"linha sem data",
	}
	r := novoResumo(agora.Add(-24 * time.Hour))
	r.ler(strings.NewReader(strings.Join(linhas, "\n")))
	if r.req != 3 || r.subst != 7 || r.colisoes != 1 || r.tipos["cpf"] != 3 || r.tipos["tabela"] != 3 || r.respostasND != 1 ||
		len(r.pseudosND) != 2 || len(r.colididos) != 1 || r.semMascara != 1 || r.recus != 1 || r.erros != 1 || r.status["529"] != 1 || r.errosAPI["overloaded_error"] != 1 {
		t.Fatalf("resumo: %+v", r)
	}
	txt := r.texto("24h")
	for _, quer := range []string{"requisições: 3", "cpf 3", "CPF-abcdefgh, t_xyzwvuts", "colididos: 241.7.9.x", "SEM máscara", "recusadas: 1"} {
		if !strings.Contains(txt, quer) {
			t.Errorf("falta %q em:\n%s", quer, txt)
		}
	}
	for _, p := range []struct {
		s    string
		quer time.Duration
	}{{"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}, {"90m", 90 * time.Minute}, {"3", 3 * time.Hour}} {
		if d, err := periodo(p.s); err != nil || d != p.quer {
			t.Errorf("período %q: %v %v", p.s, d, err)
		}
	}
	if _, err := periodo("ontem"); err == nil {
		t.Error("período inválido aceito")
	}
}

// O log gira ao passar do tamanho máximo, também com o processo no ar, e quem escreve depois
// que outro processo girou continua no arquivo novo.
func TestLogGira(t *testing.T) {
	antes := maxLog
	maxLog = 200
	defer func() { maxLog = antes }()
	p := filepath.Join(t.TempDir(), "llm-dlp.log")
	a, b := &logGirando{p: p}, &logGirando{p: p} // dois processos
	a.reabrir()
	b.reabrir()
	la, lb := log.New(a, "", 0), log.New(b, "", 0)
	for i := 0; i < 20; i++ {
		la.Printf("linha %02d do processo a, com algum texto", i)
		lb.Printf("linha %02d do processo b, com algum texto", i)
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() > maxLog {
		t.Fatalf("log atual: %v %v", st, err)
	}
	if st1, err := os.Stat(p + ".1"); err != nil || st1.Size() > maxLog+100 {
		t.Fatalf("log antigo: %v %v", st1, err)
	}
	antigo, _ := os.ReadFile(p + ".1")
	atual, _ := os.ReadFile(p)
	if !strings.HasSuffix(string(atual), "linha 19 do processo b, com algum texto\n") ||
		!strings.Contains(string(antigo)+string(atual), "linha 19 do processo a") {
		t.Errorf("as últimas linhas se perderam:\n%s\n---\n%s", antigo, atual)
	}
	if arquivoDoLog(la) == nil {
		t.Error("arquivoDoLog: nil")
	}
}
