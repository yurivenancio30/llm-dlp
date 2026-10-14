package mask

import (
	"regexp"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// A string de conexão "esquema://usuario:senha@servidor/banco": cada parte com o seu tipo, a
// senha inteira (até o "@") e o mesmo servidor com UM pseudônimo, dentro e fora da URL.
func TestConnectionURLEachPartItsType(t *testing.T) {
	cfg := config.Default()
	cfg.InternalDomains = []string{"acme"}
	m, err := NewMasker(cfg, []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	const host = "pg-prd.acme.intra"
	for _, password := range []string{"Pr0dS3nha2024", "Pr0d-S3nha!", "s3nha.final.", "abc;def?"} {
		s := "DATABASE_URL=postgresql://app_crm:" + password + "@" + host + ":5432/crm_clientes\n" +
			"o servidor " + host + " caiu; mysql://ro:" + password + "@" + host + "/crm_clientes"
		out, ents := m.Mask(s)
		for _, real := range []string{password, host, "app_crm", "crm_clientes"} {
			if strings.Contains(out, real) {
				t.Errorf("senha %q: %q ficou em claro:\n%s", password, real, out)
			}
		}
		pseudos := map[string]map[string]bool{}
		for _, e := range ents {
			if pseudos[e.Real] == nil {
				pseudos[e.Real] = map[string]bool{}
			}
			pseudos[e.Real][e.Pseudo] = true
			if e.Type == "email" {
				t.Errorf("senha %q: %q virou e-mail (é a senha e o servidor de uma URL)", password, e.Real)
			}
		}
		if len(pseudos[host]) != 1 {
			t.Errorf("senha %q: o servidor tem %d pseudônimos, devia ter 1: %v\n%s", password, len(pseudos[host]), pseudos[host], out)
		}
		if len(pseudos[password]) != 1 {
			t.Errorf("senha %q: a senha não foi mascarada inteira: %v\n%s", password, pseudos, out)
		}
		if !regexp.MustCompile(`:SEGREDO-[a-z2-7]+@`).MatchString(out) {
			t.Errorf("senha %q: sobrou parte da senha entre o pseudônimo e o @:\n%s", password, out)
		}
	}
}

// Um e-mail de verdade continua sendo e-mail: solto, no caminho ou nos parâmetros de uma URL e
// num mailto.
func TestEmailOutsideURLAuthority(t *testing.T) {
	m := newTest(t)
	const email = "joao.silva@empresa-ficticia.com.br"
	for _, s := range []string{
		"fale com " + email,
		"https://crm.example.org/clientes?email=" + email,
		"https://crm.example.org/u/" + email + "/perfil",
		"<a href=\"mailto:" + email + "\">",
	} {
		_, ents := m.Mask(s)
		foundIt := false
		for _, e := range ents {
			foundIt = foundIt || e.Type == "email" && e.Real == email
		}
		if !foundIt {
			t.Errorf("o e-mail devia ser mascarado como e-mail em %q: %v", s, ents)
		}
	}
	for _, c := range []struct {
		s     string
		i     int
		wants bool
	}{
		{"postgresql://u:s@h/db", strings.Index("postgresql://u:s@h/db", "s@"), true},
		{"https://user@host.example.org/x", strings.Index("https://user@host.example.org/x", "user@"), true},
		{"https://h.example.org/?e=a@b.com", strings.Index("https://h.example.org/?e=a@b.com", "a@"), false},
		{"x a@b.com", 2, false},
	} {
		if came := inURLAuthority(c.s, c.i); came != c.wants {
			t.Errorf("naAutoridadeDeURL(%q, %d) = %v, esperado %v", c.s, c.i, came, c.wants)
		}
	}
}
