package mask

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Transportes (seção A): o mesmo conteúdo embrulhado como chega na conversa. Usados também
// pela matriz CONTEÚDO × TRANSPORTE × DESENHO.

type transporte struct {
	nome string
	f    func(string) string
}

func cadaLinha(s string, f func(i int, l string) string) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range ls {
		ls[i] = f(i, l)
	}
	return strings.Join(ls, "\n") + "\n"
}

var transportes = []transporte{
	{"cru", func(s string) string { return s }},
	{"Read", func(s string) string {
		return cadaLinha(s, func(i int, l string) string { return fmt.Sprintf("%6d→%s", i+1, l) })
	}},
	{"cat -n", func(s string) string {
		return cadaLinha(s, func(i int, l string) string { return fmt.Sprintf("%6d\t%s", i+1, l) })
	}},
	{"grep -n", func(s string) string {
		return cadaLinha(s, func(i int, l string) string { return fmt.Sprintf("dados/saida.txt:%d:%s", i+1, l) })
	}},
	{"diff", func(s string) string {
		return "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -0,0 +1,9 @@\n" + cadaLinha(s, func(i int, l string) string { return "+" + l })
	}},
	{"markdown", func(s string) string { return "Saída:\n\n```text\n" + s + "```\n" }},
	{"JSON escapado", func(s string) string {
		b, _ := json.Marshal(map[string]string{"stdout": s})
		return string(b)
	}},
	{"ANSI", func(s string) string {
		return cadaLinha(s, func(i int, l string) string { return "\x1b[32m" + l + "\x1b[0m" })
	}},
	{"CRLF", func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }},
	{"citação", func(s string) string { return cadaLinha(s, func(i int, l string) string { return "> " + l }) }},
	{"log", func(s string) string {
		return cadaLinha(s, func(i int, l string) string { return fmt.Sprintf("2026-10-05 10:11:%02d INFO %s", i, l) })
	}},
}

// conferirMascarado: nenhum dos nomes sobra na saída, e a volta reproduz o texto exato.
func conferirMascarado(t *testing.T, m *Masker, rotulo, texto string, nomes []string) (ok, tot int) {
	t.Helper()
	out, ents := m.Mascarar(texto)
	for _, n := range nomes {
		a, b := strings.Count(texto, n), strings.Count(out, n)
		tot += a
		ok += a - b
	}
	if volta := NovaTabela(ents).Desmascarar(out, false); volta != texto {
		t.Errorf("%s: a volta não reproduz o original", rotulo)
	}
	return ok, tot
}

func TestTransporteMesmoCSV(t *testing.T) {
	csv := "table_schema,table_name,column_name\nfin_contab,lanc_diario,vl_lancto\nfin_contab,plano_contas,cd_conta\n"
	nomes := []string{"fin_contab", "lanc_diario", "plano_contas", "vl_lancto", "cd_conta"}
	for _, tr := range transportes {
		m := novoTeste(t)
		ok, tot := conferirMascarado(t, m, tr.nome, tr.f(csv), nomes)
		if ok != tot {
			out, _ := m.Mascarar(tr.f(csv))
			t.Errorf("%s: %d/%d mascarados\n%s", tr.nome, ok, tot, out)
		}
	}
}

func TestNormalizarMapa(t *testing.T) {
	casos := []string{
		"  1→a: b\n  2→c: d\n",
		"\x1b[1mtitulo\x1b[0m\r\nlinha  \r\n",
		"┌────┬────┐\n│ a  │ b  │\n╞════╪════╡\n│ x  ┆ y  │\n└────┴────┘\n",
		`{"out": "a\tb\nc\\\"d\ne"}`,
		"src/a.py:10:x = 1\nsrc/a.py-11-y = 2\n--\nsrc/b.py:3:z\n",
		"@@ -1,2 +1,2 @@\n-a\n+b\n c\n",
		"\ufeff> citado\n> > duplo\n",
	}
	for _, c := range casos {
		n, ok := normalizar(c)
		if !ok {
			t.Errorf("%q: nada normalizado", c)
			continue
		}
		if len(n.mp) != len(n.t)+1 {
			t.Fatalf("%q: mapa com tamanho errado", c)
		}
		for i := 0; i < len(n.t); i++ {
			if p := n.mp[i]; p < 0 || p >= len(c) || i > 0 && p < n.mp[i-1] {
				t.Fatalf("%q: posição %d fora de ordem", c, i)
			}
		}
		if strings.ContainsAny(n.t, "\x1b\r→│┆") || strings.Contains(n.t, "\ufeff") {
			t.Errorf("%q: sobrou transporte: %q", c, n.t)
		}
	}
	// texto comum não é tocado
	for _, c := range []string{"SELECT a FROM b\nWHERE c = 1\n", "nome: x\n12: y\nz: 3\n", "- name: a\n- name: b\n"} {
		if _, ok := normalizar(c); ok {
			t.Errorf("%q: normalizado sem transporte", c)
		}
	}
}

// item 33: o manifesto chega com prefixo de linha
func TestTransporteManifesto(t *testing.T) {
	y := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: svc-cobranca-lote\n  namespace: ns-financeiro\nspec:\n  replicas: 2\n"
	for _, tr := range transportes {
		m := novoTeste(t)
		if ok, tot := conferirMascarado(t, m, tr.nome, tr.f(y), []string{"svc-cobranca-lote", "ns-financeiro"}); ok != tot {
			t.Errorf("%s: %d/%d", tr.nome, ok, tot)
		}
	}
}
