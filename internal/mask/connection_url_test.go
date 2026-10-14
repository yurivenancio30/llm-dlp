package mask

import (
	"regexp"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// The connection string "scheme://user:password@server/database": each part with its type, the
// whole password (up to the "@") and the same server with ONE pseudonym, inside and outside the
// URL.
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
				t.Errorf("password %q: %q stayed in plain text:\n%s", password, real, out)
			}
		}
		pseudos := map[string]map[string]bool{}
		for _, e := range ents {
			if pseudos[e.Real] == nil {
				pseudos[e.Real] = map[string]bool{}
			}
			pseudos[e.Real][e.Pseudo] = true
			if e.Type == "email" {
				t.Errorf("password %q: %q became an e-mail (it is the password and the server of a URL)", password, e.Real)
			}
		}
		if len(pseudos[host]) != 1 {
			t.Errorf("password %q: the server has %d pseudonyms, should have 1: %v\n%s", password, len(pseudos[host]), pseudos[host], out)
		}
		if len(pseudos[password]) != 1 {
			t.Errorf("password %q: the password was not masked whole: %v\n%s", password, pseudos, out)
		}
		if !regexp.MustCompile(`:SECRET-[a-z2-7]+@`).MatchString(out) {
			t.Errorf("password %q: part of the password left between the pseudonym and the @:\n%s", password, out)
		}
	}
}

// A real e-mail is still an e-mail: loose, in the path or in the parameters of a URL and in a
// mailto.
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
			t.Errorf("the e-mail should be masked as an e-mail in %q: %v", s, ents)
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
			t.Errorf("inURLAuthority(%q, %d) = %v, expected %v", c.s, c.i, came, c.wants)
		}
	}
}
