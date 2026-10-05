package mask

import (
	"regexp"
	"strings"
)

// Leitor de tabelas (ver docs/estruturas.md, seções Saídas de clientes de banco e Dados
// tabulares). A tabela é reconhecida pela forma, não pela ferramenta: cabeçalho seguido de uma
// linha de traços (psql, sqlcmd, sqlplus, mysql, snowsql, bq, db2, markdown), ou linhas com o
// mesmo número de separadores (CSV, TSV, "|"). Onde o cabeçalho diz que a coluna guarda nome de
// objeto (os nomes padronizados do information_schema e dos catálogos), os valores são objetos.
// As outras células do cabeçalho são nomes de coluna.

// cabeçalho de catálogo -> entidade dos valores daquela coluna
var cabCatalogo = map[string]string{
	"table_name": "tabela", "tablename": "tabela", "tabname": "tabela", "table": "tabela", "tabela": "tabela",
	"view_name": "tabela", "object_name": "tabela", "objeto": "tabela", "object": "tabela", "tableid": "tabela",
	"table_schema": "schema", "schema_name": "schema", "schemaname": "schema", "tabschema": "schema", "schema": "schema",
	"routine_schema": "schema", "specific_schema": "schema", "constraint_schema": "schema", "owner": "usuario",
	"table_catalog": "database", "catalog_name": "database", "database_name": "database", "database": "database",
	"datname": "database", "banco": "database", "routine_catalog": "database",
	"column_name": "coluna", "colname": "coluna", "coluna": "coluna", "column": "coluna", "field": "coluna",
	"routine_name": "procedure", "specific_name": "procedure", "procedure_name": "procedure", "function_name": "procedure",
	"index_name": "indice", "indname": "indice", "key_name": "indice", "constraint_name": "indice",
	"server": "servidor", "servidor": "servidor", "server_name": "servidor", "host_name": "servidor", "hostname": "servidor",
	"usename": "usuario", "rolname": "usuario", "grantee": "usuario", "user_name": "usuario", "username": "usuario",
}

// rótulos das próprias ferramentas: nunca são nome de coluna do usuário
var rotulosSaida = map[string]bool{"name": true, "type": true, "null": true, "key": true, "default": true, "extra": true,
	"rows": true, "count": true, "mean": true, "std": true, "min": true, "max": true, "dtype": true, "non-null": true,
	"labels": true, "id": true, "value": true, "description": true, "status": true, "size": true, "kind": true,
	"comment": true, "collation": true, "nullable": true, "length": true, "precision": true, "scale": true}

var (
	reSeparador   = regexp.MustCompile(`^\s*[|+]?[\s|+:]*[-=─━]{3,}[-=─━+|:\s┼┬┴╋]*$`)
	reCelulaIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#.\-]*$`)
	reTablesIn    = regexp.MustCompile(`(?i)^tables_in_([A-Za-z_][\w$]*)$`)
)

type celula struct{ a, b int } // posição no texto

// celulasObj divide a linha [ini, fim) de s em células: pelo separador sep, ou pelas faixas de
// traços da linha separadora (tabela alinhada por espaços).
func celulasObj(s string, ini, fim int, sep byte, faixas []celula) []celula {
	var out []celula
	add := func(a, b int) {
		for a < b && (s[a] == ' ' || s[a] == '\t' || s[a] == '"') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t' || s[b-1] == '\r' || s[b-1] == '"') {
			b--
		}
		out = append(out, celula{a, b})
	}
	if sep != 0 {
		a := ini
		for i := ini; i <= fim; i++ {
			if i == fim || s[i] == sep {
				add(a, i)
				a = i + 1
			}
		}
		if sep == '|' && len(out) > 0 { // "| a | b |": bordas vazias
			if out[0].a == out[0].b {
				out = out[1:]
			}
			if n := len(out); n > 0 && out[n-1].a == out[n-1].b {
				out = out[:n-1]
			}
		}
		return out
	}
	for _, f := range faixas {
		a, b := ini+f.a, ini+f.b
		if a >= fim {
			out = append(out, celula{fim, fim})
			continue
		}
		add(a, min(b, fim))
	}
	return out
}

func acharTabelasObj(s string, add func(ObjAchado)) {
	if strings.Count(s, "\n") < 2 {
		return
	}
	var linhas []celula // [ini, fim) de cada linha
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		if j < 0 {
			linhas = append(linhas, celula{i, len(s)})
			break
		}
		linhas = append(linhas, celula{i, i + j})
		i += j + 1
	}
	for i := 0; i+1 < len(linhas); i++ {
		h, prox := linhas[i], linhas[i+1]
		cab := s[h.a:h.b]
		if strings.TrimSpace(cab) == "" || reSeparador.MatchString(cab) {
			continue
		}
		var sep byte
		var faixas []celula
		dados := i + 1
		if reSeparador.MatchString(s[prox.a:prox.b]) {
			dados = i + 2
			if strings.Contains(cab, "|") {
				sep = '|'
			} else { // faixas de traços da linha separadora
				l := s[prox.a:prox.b]
				for k := 0; k < len(l); {
					if l[k] == '-' || l[k] == '=' {
						a := k
						for k < len(l) && (l[k] == '-' || l[k] == '=') {
							k++
						}
						faixas = append(faixas, celula{a, k})
					} else {
						k++
					}
				}
				if len(faixas) < 1 {
					continue
				}
				if len(faixas) > 1 { // cada faixa vai até a próxima (valores podem passar dos traços)
					for k := 0; k+1 < len(faixas); k++ {
						faixas[k].b = faixas[k+1].a
					}
				}
				faixas[len(faixas)-1].b = 1 << 20
			}
		} else {
			for _, c := range []byte{'\t', ',', ';', '|'} {
				n := strings.Count(cab, string(c))
				if n >= 1 && strings.Count(s[prox.a:prox.b], string(c)) == n {
					sep = c
					break
				}
			}
			if sep == 0 {
				continue
			}
		}
		cs := celulasObj(s, h.a, h.b, sep, faixas)
		if len(cs) < 2 {
			continue
		}
		ok := true
		for _, c := range cs {
			if c.a == c.b || !reCelulaIdent.MatchString(s[c.a:c.b]) && !(sep != 0 && sep != ',' && strings.Contains(s[c.a:c.b], " ")) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		// colunas de objeto e o próprio cabeçalho
		ent := map[int]string{}
		temSchema := false
		for k, c := range cs {
			v := strings.ToLower(s[c.a:c.b])
			if e, ok := cabCatalogo[v]; ok {
				ent[k] = e
				temSchema = temSchema || e == "schema"
				continue
			}
			if m := reTablesIn.FindStringSubmatchIndex(s[c.a:c.b]); m != nil { // MySQL: Tables_in_<banco>
				ent[k] = "tabela"
				add(ObjAchado{c.a + m[2], c.a + m[3], "database", "tabela-catálogo", true})
				continue
			}
			if !rotulosSaida[v] && !publicoSQL(v) && caraDeIdentificador(s[c.a:c.b]) {
				add(ObjAchado{c.a, c.b, "coluna", "cabeçalho", false})
			}
		}
		if temSchema { // psql \dt: Schema | Name | Type | Owner
			for k, c := range cs {
				if strings.EqualFold(s[c.a:c.b], "name") {
					ent[k] = "tabela"
				}
			}
		}
		k := dados
		for ; k < len(linhas); k++ {
			l := linhas[k]
			linha := s[l.a:l.b]
			if strings.TrimSpace(linha) == "" {
				break
			}
			if reSeparador.MatchString(linha) {
				continue
			}
			if len(ent) == 0 {
				continue
			}
			vs := celulasObj(s, l.a, l.b, sep, faixas)
			for col, e := range ent {
				if col >= len(vs) {
					continue
				}
				c := vs[col]
				v := s[c.a:c.b]
				if v == "" || !reCelulaIdent.MatchString(v) || strings.EqualFold(v, "null") {
					continue
				}
				addPartesCelula(s, c.a, c.b, e, add)
			}
		}
		i = k - 1
	}
}

// addPartesCelula: valor de coluna de catálogo, que pode ser qualificado (banco.schema.tabela).
func addPartesCelula(s string, a, b int, ult string, add func(ObjAchado)) {
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), ult)
	for k, p := range ps {
		if p != "" && !publicoSQL(p) && reIdentSimples.MatchString(p) {
			add(ObjAchado{a, a + len(p), ents[k], "tabela-catálogo", ents[k] != "coluna"})
		}
		a += len(p) + 1
	}
}
