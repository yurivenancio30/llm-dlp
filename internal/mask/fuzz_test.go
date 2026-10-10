package mask

import (
	"strings"
	"testing"
)

// Fuzz: a normalização mantém o mapa coerente e a ida e volta pelos leitores
// devolve o texto byte a byte, com qualquer transporte em volta.
// go test ./internal/mask -run '^$' -fuzz FuzzNormalizar -fuzztime 60s
// go test ./internal/mask -run '^$' -fuzz FuzzIdaVolta -fuzztime 60s

func sementesFuzz(f *testing.F) {
	for _, c := range conteudosMatriz[:3] {
		for _, d := range desenhosMatriz[:12] {
			for _, tr := range transportes {
				f.Add(tr.f(d.f(c.cols, c.rows)))
			}
		}
	}
	f.Add("\x1b[1m│ a ┆ b │\r\n\\n\\\"x\\u003c\n")
	f.Add("  1→a.py:2:x\n+@@ -1 +1 @@\n> > ```\n")
}

func FuzzNormalizar(f *testing.F) {
	sementesFuzz(f)
	f.Fuzz(func(t *testing.T, s string) {
		n, ok := normalizar(s)
		if !ok {
			return
		}
		if len(n.mp) != len(n.t)+1 {
			t.Fatalf("mapa com %d posições para %d bytes", len(n.mp), len(n.t))
		}
		for i := 0; i < len(n.t); i++ {
			if p := n.mp[i]; p < 0 || p >= len(s) || i > 0 && p < n.mp[i-1] {
				t.Fatalf("posição %d -> %d fora de ordem", i, p)
			}
		}
		for a := 0; a+1 < len(n.t) && a < 200; a += 7 {
			b := min(len(n.t), a+5)
			if oa, ob, ok := n.original(s, a, b); ok && s[oa:ob] != n.t[a:b] {
				t.Fatalf("trecho [%d,%d) não corresponde ao original", a, b)
			}
		}
	})
}

func FuzzIdaVolta(f *testing.F) {
	sementesFuzz(f)
	m := novoTeste(&testing.T{})
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 || !strings.ContainsAny(s, "\n,=:|") {
			return
		}
		out, ents := m.Mascarar(s)
		if volta := NovaTabela(ents).Desmascarar(out, false); volta != s {
			t.Fatalf("a volta não reproduz o original:\n%q\n%q", s, volta)
		}
	})
}

// a varredura manual do leitor de SQL faz o mesmo que as regex que substituiu
func FuzzVarreduraSQL(f *testing.F) {
	for _, s := range []string{"SELECT a.b, [x y].\"z\" FROM `db.t` GROUP  BY c ORDER\nBY d", "café.ação_1 $x #y 1abc", "[]\"\"``[a", "group by groupby ORDERBY"} {
		f.Add(s)
	}
	sementesFuzz(f)
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 2000 {
			return
		}
		eq := func(nome string, a [][]int, b [][2]int) {
			if len(a) != len(b) {
				t.Fatalf("%s: %d contra %d em %q", nome, len(a), len(b), s)
			}
			for i := range a {
				if a[i][0] != b[i][0] || a[i][1] != b[i][1] {
					t.Fatalf("%s: %v contra %v em %q", nome, a[i], b[i], s)
				}
			}
		}
		eq("partes", reParteSQL.FindAllStringIndex(s, -1), partesSQLEm(s))
		eq("qualificados", reQualTok.FindAllStringIndex(s, -1), qualsSQLEm(s))
		if a, b := len(reClausulasSQL.FindAllStringIndex(s, 4)), contarClausulas(s, 4); a != b {
			t.Fatalf("cláusulas: %d contra %d em %q", a, b, s)
		}
	})
}
