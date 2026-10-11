package mask

import (
	"regexp"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// A string de conexão "esquema://usuario:senha@servidor/banco": cada parte com o seu tipo, a
// senha inteira (até o "@") e o mesmo servidor com UM pseudônimo, dentro e fora da URL.
func TestURLDeConexaoCadaParteComSeuTipo(t *testing.T) {
	cfg := config.Padrao()
	cfg.DominiosInternos = []string{"acme"}
	m, err := NovoMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	const host = "pg-prd.acme.intra"
	for _, senha := range []string{"Pr0dS3nha2024", "Pr0d-S3nha!", "s3nha.final.", "abc;def?"} {
		s := "DATABASE_URL=postgresql://app_crm:" + senha + "@" + host + ":5432/crm_clientes\n" +
			"o servidor " + host + " caiu; mysql://ro:" + senha + "@" + host + "/crm_clientes"
		out, ents := m.Mascarar(s)
		for _, real := range []string{senha, host, "app_crm", "crm_clientes"} {
			if strings.Contains(out, real) {
				t.Errorf("senha %q: %q ficou em claro:\n%s", senha, real, out)
			}
		}
		pseudos := map[string]map[string]bool{}
		for _, e := range ents {
			if pseudos[e.Real] == nil {
				pseudos[e.Real] = map[string]bool{}
			}
			pseudos[e.Real][e.Pseudo] = true
			if e.Tipo == "email" {
				t.Errorf("senha %q: %q virou e-mail (é a senha e o servidor de uma URL)", senha, e.Real)
			}
		}
		if len(pseudos[host]) != 1 {
			t.Errorf("senha %q: o servidor tem %d pseudônimos, devia ter 1: %v\n%s", senha, len(pseudos[host]), pseudos[host], out)
		}
		if len(pseudos[senha]) != 1 {
			t.Errorf("senha %q: a senha não foi mascarada inteira: %v\n%s", senha, pseudos, out)
		}
		if !regexp.MustCompile(`:SEGREDO-[a-z2-7]+@`).MatchString(out) {
			t.Errorf("senha %q: sobrou parte da senha entre o pseudônimo e o @:\n%s", senha, out)
		}
	}
}

// Um e-mail de verdade continua sendo e-mail: solto, no caminho ou nos parâmetros de uma URL e
// num mailto.
func TestEmailForaDaAutoridadeDaURL(t *testing.T) {
	m := novoTeste(t)
	const email = "joao.silva@empresa-ficticia.com.br"
	for _, s := range []string{
		"fale com " + email,
		"https://crm.example.org/clientes?email=" + email,
		"https://crm.example.org/u/" + email + "/perfil",
		"<a href=\"mailto:" + email + "\">",
	} {
		_, ents := m.Mascarar(s)
		achou := false
		for _, e := range ents {
			achou = achou || e.Tipo == "email" && e.Real == email
		}
		if !achou {
			t.Errorf("o e-mail devia ser mascarado como e-mail em %q: %v", s, ents)
		}
	}
	for _, c := range []struct {
		s    string
		i    int
		quer bool
	}{
		{"postgresql://u:s@h/db", strings.Index("postgresql://u:s@h/db", "s@"), true},
		{"https://user@host.example.org/x", strings.Index("https://user@host.example.org/x", "user@"), true},
		{"https://h.example.org/?e=a@b.com", strings.Index("https://h.example.org/?e=a@b.com", "a@"), false},
		{"x a@b.com", 2, false},
	} {
		if veio := naAutoridadeDeURL(c.s, c.i); veio != c.quer {
			t.Errorf("naAutoridadeDeURL(%q, %d) = %v, esperado %v", c.s, c.i, veio, c.quer)
		}
	}
}
