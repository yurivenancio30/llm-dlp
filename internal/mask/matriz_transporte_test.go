package mask

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Matriz CONTEÚDO × TRANSPORTE × DESENHO (seção H, itens 34 e 35): cada conteúdo (registros com
// campos de tipo conhecido) é escrito em cada desenho e embrulhado em cada transporte. Regra:
// os mesmos nomes mascarados em todas as células, sem nada no config. Meta: 95%. Os negativos
// (conteúdo sem nome interno) não podem ter nada mascarado em célula nenhuma.

type conteudoM struct {
	nome  string
	cols  []string
	rows  [][]string
	nomes []int // as colunas que guardam nome
}

var conteudosMatriz = []conteudoM{
	{"catálogo", []string{"table_schema", "table_name", "column_name"},
		[][]string{{"mt_fin_contab", "mt_lanc_diario", "mt_vl_lancto"}, {"mt_fin_contab", "mt_plano_contas", "mt_cd_conta"}}, []int{0, 1, 2}},
	{"conexão", []string{"host", "port", "database", "user"},
		[][]string{{"mt-srv-erp01", "5432", "mt_erp_fin", "mt_svc_carga"}, {"mt-srv-erp02", "5433", "mt_erp_rh", "mt_svc_folha"}}, []int{0, 2, 3}},
	{"config Snowflake", []string{"account", "role", "warehouse", "database", "schema"},
		[][]string{{"mt-acme-erpfin", "mt_papel_fin", "mt_wh_carga", "mt_db_fin", "mt_sch_contab"}, {"mt-acme-erprh", "mt_papel_rh", "mt_wh_folha", "mt_db_rh", "mt_sch_folha"}}, []int{0, 1, 2, 3, 4}},
	{"manifesto Kubernetes", []string{"namespace", "service", "replicas"},
		[][]string{{"mt-ns-financeiro", "mt-svc-cobranca", "2"}, {"mt-ns-financeiro", "mt-svc-conciliacao", "3"}}, []int{0, 1}},
	{"listagem de buckets", []string{"bucket", "region", "created"},
		[][]string{{"mt-bkt-relat-fin", "us-east-1", "2024-01-01"}, {"mt-bkt-notas-fisc", "sa-east-1", "2024-02-01"}}, []int{0}},
	{"modelo ORM", []string{"table", "schema", "column"},
		[][]string{{"mt_lancamento", "mt_contab", "mt_vl_total"}, {"mt_conta_razao", "mt_contab", "mt_cd_razao"}}, []int{0, 1, 2}},
}

var negativosMatriz = []conteudoM{
	{"pip list", []string{"package", "version"}, [][]string{{"requests", "2.31.0"}, {"typing_extensions", "4.12.2"}}, nil},
	{"benchmark", []string{"benchmark", "ns/op", "allocs/op"}, [][]string{{"FrioDenso-32", "21200000", "3"}, {"FrioComum-32", "8870000", "2"}}, nil},
	{"arquivos", []string{"permissions", "size", "file"}, [][]string{{"-rw-r--r--", "220", "notas_reuniao.txt"}, {"drwxr-xr-x", "4096", "bin"}}, nil},
	{"números", []string{"a", "b", "c"}, [][]string{{"0.496714", "-0.138264", "0.647689"}, {"1.523030", "-0.234153", "-0.234137"}}, nil},
}

type desenhoM struct {
	nome      string
	f         func(cols []string, rows [][]string) string
	propagado bool // só pela propagação: o texto não tem a estrutura (vem depois do CSV)
}

func larguras(cols []string, rows [][]string) []int {
	w := make([]int, len(cols))
	for j, c := range cols {
		w[j] = len(c)
		for _, r := range rows {
			w[j] = max(w[j], len(r[j]))
		}
	}
	return w
}

func juntar(sep string) func([]string, [][]string) string {
	return func(cols []string, rows [][]string) string {
		var b strings.Builder
		b.WriteString(strings.Join(cols, sep) + "\n")
		for _, r := range rows {
			b.WriteString(strings.Join(r, sep) + "\n")
		}
		return b.String()
	}
}

func linhaFixa(vs []string, w []int, sep, borda string) string {
	ps := make([]string, len(vs))
	for j, v := range vs {
		ps[j] = v + strings.Repeat(" ", w[j]-len(v))
	}
	return borda + strings.Join(ps, sep) + strings.TrimRight(borda, " ") + "\n"
}

func tracos(w []int, ch, cruz, ini, fim string) string {
	ps := make([]string, len(w))
	for j, n := range w {
		ps[j] = strings.Repeat(ch, n+2)
	}
	return ini + strings.Join(ps, cruz) + fim + "\n"
}

var desenhosMatriz = []desenhoM{
	{"vírgula", juntar(","), false},
	{"ponto e vírgula", juntar(";"), false},
	{"tab", juntar("\t"), false},
	{"|", func(cols []string, rows [][]string) string {
		w := larguras(cols, rows)
		s := linhaFixa(cols, w, " | ", "| ") + tracos(w, "-", "|", "|", "|")
		for _, r := range rows {
			s += linhaFixa(r, w, " | ", "| ")
		}
		return s
	}, false},
	{"caixa", func(cols []string, rows [][]string) string {
		w := larguras(cols, rows)
		s := tracos(w, "─", "┬", "┌", "┐") + linhaFixa(cols, w, " │ ", "│ ") + tracos(w, "─", "┼", "├", "┤")
		for _, r := range rows {
			s += linhaFixa(r, w, " │ ", "│ ")
		}
		return s + tracos(w, "─", "┴", "└", "┘")
	}, false},
	{"largura fixa", func(cols []string, rows [][]string) string {
		w := larguras(cols, rows)
		s := linhaFixa(cols, w, "   ", "")
		for _, r := range rows {
			s += linhaFixa(r, w, "   ", "")
		}
		return s
	}, false},
	{"bordas", func(cols []string, rows [][]string) string {
		w := larguras(cols, rows)
		t := tracos(w, "-", "+", "+", "+")
		s := t + linhaFixa(cols, w, " | ", "| ") + t
		for _, r := range rows {
			s += linhaFixa(r, w, " | ", "| ")
		}
		return s + t
	}, false},
	{"tuplas", func(cols []string, rows [][]string) string {
		q := func(vs []string) string { return "('" + strings.Join(vs, "', '") + "')\n" }
		s := q(cols)
		for _, r := range rows {
			s += q(r)
		}
		return s
	}, false},
	{"HTML", func(cols []string, rows [][]string) string {
		s := "<table>\n<tr><th>" + strings.Join(cols, "</th><th>") + "</th></tr>\n"
		for _, r := range rows {
			s += "<tr><td>" + strings.Join(r, "</td><td>") + "</td></tr>\n"
		}
		return s + "</table>\n"
	}, false},
	{"vertical", func(cols []string, rows [][]string) string {
		w := larguras(cols, nil)
		s := ""
		for i, r := range rows {
			s += fmt.Sprintf("-[ RECORD %d ]+------\n", i+1)
			for j, c := range cols {
				s += c + strings.Repeat(" ", w[j]-len(c)) + " | " + r[j] + "\n"
			}
		}
		return s
	}, false},
	{"json indentado", func(cols []string, rows [][]string) string {
		var os []map[string]string
		for _, r := range rows {
			o := map[string]string{}
			for j, c := range cols {
				o[c] = r[j]
			}
			os = append(os, o)
		}
		b, _ := json.MarshalIndent(os, "", "  ")
		return string(b) + "\n"
	}, false},
	{"jsonl", func(cols []string, rows [][]string) string {
		s := ""
		for _, r := range rows {
			o := map[string]string{}
			for j, c := range cols {
				o[c] = r[j]
			}
			b, _ := json.Marshal(o)
			s += string(b) + "\n"
		}
		return s
	}, false},
	{"k=v em linha", func(cols []string, rows [][]string) string {
		s := ""
		for _, r := range rows {
			ps := make([]string, len(cols))
			for j, c := range cols {
				ps[j] = c + "=" + r[j]
			}
			s += "registro " + strings.Join(ps, " ") + "\n"
		}
		return s
	}, false},
	{"coluna solta", func(cols []string, rows [][]string) string {
		s := ""
		for j, c := range cols {
			s += c + "\n"
			for _, r := range rows {
				s += r[j] + "\n"
			}
			s += "\n"
		}
		return s
	}, false},
	{"uniq -c", func(cols []string, rows [][]string) string {
		s := ""
		for j := range cols {
			for i, r := range rows {
				s += fmt.Sprintf("%7d %s\n", i+1, r[j])
			}
		}
		return s
	}, true},
}

// celulaMatriz: quantos nomes do conteúdo foram mascarados no texto (desenho, transporte).
func celulaMatriz(t *testing.T, c conteudoM, d desenhoM, tr transporte) (ok, tot int, out string) {
	m := novoTeste(t)
	m.cfg.DominiosInternos = nil
	m.cfg.Termos = nil
	var nomes []string
	visto := map[string]bool{}
	for _, r := range c.rows {
		for _, j := range c.nomes {
			if visto[r[j]] {
				continue
			}
			// só pela propagação: coluna não é propagada (nomes como user_id existem em todo código)
			if d.propagado && strings.Contains(c.cols[j], "column") {
				continue
			}
			visto[r[j]] = true
			nomes = append(nomes, r[j])
		}
	}
	if d.propagado {
		m.Mascarar(juntar(",")(c.cols, c.rows)) // o arquivo passou antes pela conversa
	}
	txt := tr.f(d.f(c.cols, c.rows))
	ok, tot = conferirMascarado(t, m, c.nome+"/"+d.nome+"/"+tr.nome, txt, nomes)
	out, _ = m.Mascarar(txt)
	return ok, tot, out
}

func TestMatrizTransporteDesenho(t *testing.T) {
	if testing.Short() {
		t.Skip("matriz grande")
	}
	type par struct{ ok, tot int }
	porDT := map[[2]string]par{}
	porC := map[string]par{}
	var falhas []string
	okT, totT := 0, 0
	for _, c := range conteudosMatriz {
		for _, d := range desenhosMatriz {
			for _, tr := range transportes {
				ok, tot, _ := celulaMatriz(t, c, d, tr)
				k := [2]string{d.nome, tr.nome}
				p := porDT[k]
				porDT[k] = par{p.ok + ok, p.tot + tot}
				q := porC[c.nome]
				porC[c.nome] = par{q.ok + ok, q.tot + tot}
				okT += ok
				totT += tot
				if ok < tot {
					falhas = append(falhas, fmt.Sprintf("%s / %s / %s: %d/%d", c.nome, d.nome, tr.nome, ok, tot))
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s", "desenho \\ transp.")
	for _, tr := range transportes {
		fmt.Fprintf(&b, " %8.8s", tr.nome)
	}
	b.WriteString("\n")
	for _, d := range desenhosMatriz {
		fmt.Fprintf(&b, "%-16.16s", d.nome)
		for _, tr := range transportes {
			p := porDT[[2]string{d.nome, tr.nome}]
			fmt.Fprintf(&b, " %7.1f%%", 100*float64(p.ok)/float64(max(1, p.tot)))
		}
		b.WriteString("\n")
	}
	var cs []string
	for k, p := range porC {
		cs = append(cs, fmt.Sprintf("  %-22s %4d/%-4d %5.1f%%", k, p.ok, p.tot, 100*float64(p.ok)/float64(p.tot)))
	}
	sort.Strings(cs)
	pct := 100 * float64(okT) / float64(totT)
	t.Logf("matriz CONTEÚDO × TRANSPORTE × DESENHO:\n%s\npor conteúdo:\n%s\nTOTAL %d/%d = %.1f%%", b.String(), strings.Join(cs, "\n"), okT, totT, pct)
	if testing.Verbose() && len(falhas) > 0 {
		t.Logf("células incompletas:\n  %s", strings.Join(falhas, "\n  "))
	}
	if pct < 95 {
		t.Errorf("cobertura %.1f%% abaixo da meta de 95%%", pct)
	}
}

func TestMatrizNegativos(t *testing.T) {
	if testing.Short() {
		t.Skip("matriz grande")
	}
	n, ruins := 0, 0
	for _, c := range negativosMatriz {
		for _, d := range desenhosMatriz {
			if d.propagado {
				continue
			}
			for _, tr := range transportes {
				m := novoTeste(t)
				m.cfg.DominiosInternos = nil
				m.cfg.Termos = nil
				txt := tr.f(d.f(c.cols, c.rows))
				n++
				if out, _ := m.Mascarar(txt); out != txt {
					ruins++
					t.Errorf("%s / %s / %s: mascarou\n%s", c.nome, d.nome, tr.nome, out)
				}
			}
		}
	}
	t.Logf("negativos: %d células, %d com máscara", n, ruins)
}
