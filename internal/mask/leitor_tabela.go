package mask

import (
	"regexp"
	"strings"
)

// Leitor de tabelas (ver docs/estruturas.md, seções Saídas de clientes de banco e Dados
// tabulares). A tabela é reconhecida pela forma, não pela ferramenta: cabeçalho seguido de uma
// linha de traços (psql, sqlcmd, sqlplus, mysql, snowsql, bq, db2, markdown), linhas com o
// mesmo número de separadores fora de aspas (CSV conforme a RFC 4180, TSV, ";", "|"; as bordas
// de caixa chegam como "|" pela normalização), colunas alinhadas por espaços sem linha de traços
// (print de DataFrame, R, listagens de linha de comando), linhas que são tuplas ou listas,
// tabela HTML e registro vertical ("-[ RECORD 1 ]-" + "chave | valor"). Onde o cabeçalho diz que
// a coluna guarda nome de objeto (os nomes padronizados do information_schema e dos catálogos,
// ou uma palavra de tipo em qualquer grafia: ver entChave), os valores são objetos. As outras
// células do cabeçalho são nomes de coluna.

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
	reCelulaIdent = regexp.MustCompile(`^[\p{L}_][\p{L}0-9_$#.\-]*$`)
	// célula de cabeçalho: identificador, até 3 palavras ("primary key", "APP VERSION"), "null?"
	reCelulaCab = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#.\-]*(?: [A-Za-z_][A-Za-z0-9_$#.\-]*){0,2}\??$`)
	reTablesIn  = regexp.MustCompile(`(?i)^tables_in_([A-Za-z_][\w$]*)$`)
)

type celula struct{ a, b int } // posição no texto

// celulaCabecalho: a célula tem forma de cabeçalho de catálogo: um identificador ("null?"
// aceito) ou, em tabela de "|", TAB ou traços (não em CSV, onde a vírgula também separa listas
// de SQL), até 3 palavras todas em minúsculas ou todas em maiúsculas ("primary key", "APP
// VERSION"), que é como as ferramentas escrevem. Título de documento ("Data Type") não. Sob uma
// linha de traços, a faixa já delimita a célula: vale qualquer texto curto ("Non-Null Count").
func celulaCabecalho(v string, sep byte, tracos bool) bool {
	if !reCelulaCab.MatchString(v) {
		return false
	}
	if !strings.Contains(v, " ") || tracos { // sob a linha de traços, a faixa já delimita a célula
		return true
	}
	return (sep == '|' || sep == '\t' || sep == 0) && (strings.ToLower(v) == v || strings.ToUpper(v) == v)
}

// entCabecalho: a entidade dos valores de uma coluna pelo nome do cabeçalho.
func entCabecalho(v string) (string, bool) {
	if e, ok := cabCatalogo[strings.ToLower(v)]; ok {
		return e, true
	}
	if e, forte := entChave(v); forte && e != "" {
		return e, true
	}
	return "", false
}

// contaFora: quantos c há em l fora de aspas duplas (CSV: "a,b" é uma célula só).
func contaFora(l string, c byte) int {
	n, aspas := 0, false
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case '"':
			aspas = !aspas
		case c:
			if !aspas {
				n++
			}
		}
	}
	return n
}

// celulasObj divide a linha [ini, fim) de s em células: pelo separador sep (fora de aspas), ou
// pelas faixas de traços da linha separadora (tabela alinhada por espaços).
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
		a, aspas := ini, false
		for i := ini; i <= fim; i++ {
			if i < fim && s[i] == '"' && sep != '|' {
				aspas = !aspas
			}
			if i == fim || s[i] == sep && !aspas {
				add(a, i)
				a = i + 1
			}
		}
		if sep == '|' { // "| a | b |", "|| a | b ||": bordas vazias
			for len(out) > 0 && out[0].a == out[0].b {
				out = out[1:]
			}
			for n := len(out); n > 0 && out[n-1].a == out[n-1].b; n = len(out) {
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

// tokensLinha: as palavras (separadas por espaço) de s[ini:fim).
func tokensLinha(s string, ini, fim int) []celula {
	var out []celula
	for i := ini; i < fim; {
		for i < fim && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		j := i
		for j < fim && s[j] != ' ' && s[j] != '\t' {
			j++
		}
		if j > i {
			out = append(out, celula{i, j})
		}
		i = j
	}
	return out
}

// temVao: a linha tem um vão de 2+ espaços entre palavras, um TAB, ou começa recuada.
func temVao(l string) bool {
	t := strings.TrimRight(l, " \t")
	if strings.HasPrefix(t, " ") {
		return true
	}
	return strings.Contains(strings.TrimLeft(t, " "), "  ") || strings.Contains(t, "\t")
}

var reCabFixo = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#.\-()/?]*$`)

// colunasFixas: a linha s[l] como cabeçalho de colunas alinhadas por espaços (cada palavra é
// uma coluna); nil se não tem a forma (menos de duas palavras, número, pontuação de frase).
func colunasFixas(s string, l celula) []celula {
	cs := tokensLinha(s, l.a, l.b)
	if len(cs) < 2 || len(cs) > 40 {
		return nil
	}
	// prosa não tem colunas: o cabeçalho tem um vão de 2+ espaços, um TAB ou começa recuado
	// (índice do DataFrame); as linhas de dados também (ver alinharFixo)
	if !temVao(s[l.a:l.b]) {
		return nil
	}
	for _, c := range cs {
		if !reCabFixo.MatchString(s[c.a:c.b]) {
			return nil
		}
	}
	return cs
}

// alinharFixo: as células da linha l sob as colunas cab (posições relativas ao começo da
// linha). Cada palavra vai para a coluna com que mais se sobrepõe (valor alinhado à direita ou
// à esquerda) ou, sem sobreposição, para a coluna em que começa; palavra antes da primeira
// coluna é índice. ok=false se a linha não se alinha (menos da metade das colunas preenchida).
func alinharFixo(s string, l celula, cab []celula, hIni int) ([]celula, bool) {
	if !temVao(s[l.a:l.b]) || len(tokensLinha(s, l.a, l.b)) > 2*len(cab)+4 {
		return nil, false
	}
	out := make([]celula, len(cab))
	for k := range out {
		out[k] = celula{-1, -1}
	}
	n := 0
	for _, t := range tokensLinha(s, l.a, l.b) {
		a, b := t.a-l.a, t.b-l.a
		melhor, sob := -1, 0
		for k, c := range cab {
			ca, cb := c.a-hIni, c.b-hIni
			if o := min(b, cb) - max(a, ca); o > sob {
				melhor, sob = k, o
			}
		}
		if melhor < 0 {
			for k := len(cab) - 1; k >= 0; k-- {
				if a >= cab[k].a-hIni {
					melhor = k
					break
				}
			}
		}
		if melhor < 0 {
			continue // índice à esquerda
		}
		if out[melhor].a < 0 {
			out[melhor] = t
			n++
		} else {
			out[melhor].b = t.b // valor com espaços ("2 days ago")
		}
	}
	return out, n >= 2 && n*2 >= len(cab)
}

// tituloTipo: a linha logo acima do cabeçalho (até 3 linhas, sem bordas) é só uma palavra de
// tipo ("Buckets", "TABLES")? Então a coluna "name" guarda nomes desse tipo.
func tituloTipo(s string, linhas []celula, i int) string {
	for k := i - 1; k >= 0 && k >= i-3; k-- {
		l := strings.Trim(s[linhas[k].a:linhas[k].b], " \t|+-=")
		if l == "" {
			continue
		}
		if strings.ContainsAny(l, " \t") {
			return ""
		}
		e, _ := entCabecalho(strings.ToLower(l))
		return e
	}
	return ""
}

func acharTabelasObj(s string, add func(ObjAchado)) { acharTabelasD(s, nil, add) }

// acharTabelasD: com d != nil (a dica do comando, ver comando.go), só as colunas que o cabeçalho
// não tipa e a dica tipa (o resto já saiu sem a dica).
func acharTabelasD(s string, d *dicaSaida, add func(ObjAchado)) {
	addSemDica := add
	if d != nil {
		addSemDica = func(ObjAchado) {}
	}
	acharTuplas(s, d, add)
	acharTabelaHTML(s, d, add)
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
	rotulo := make([]bool, len(linhas))
	for i, l := range linhas {
		rotulo[i] = strings.IndexByte(s[l.a:l.b], '\t') > 0 && rotuloEValores(s, l, addSemDica)
	}
	for i := 0; i+1 < len(linhas); i++ {
		if rotulo[i] {
			continue
		}
		h, prox := linhas[i], linhas[i+1]
		cab := s[h.a:h.b]
		if strings.TrimSpace(cab) == "" || reSeparador.MatchString(cab) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(cab), "-[ RECORD ") {
			i = registroVertical(s, linhas, i+1, addSemDica) - 1
			continue
		}
		var sep byte
		var faixas, fixas []celula
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
				n := contaFora(cab, c)
				if n >= 1 && contaFora(s[prox.a:prox.b], c) == n {
					sep = c
					break
				}
			}
			if sep == 0 {
				fixas = colunasFixas(s, h)
				if fixas == nil {
					continue
				}
			}
		}
		var cs []celula
		if fixas != nil {
			cs = fixas
		} else {
			cs = celulasObj(s, h.a, h.b, sep, faixas)
		}
		if len(cs) < 2 {
			continue
		}
		ok := true
		for _, c := range cs {
			// cabeçalho com forma de identificador: sem espaço, operador ou unidade ("Speed MiB/s",
			// "for i := 0" são tabela de benchmark e trecho de código, não catálogo)
			if v := s[c.a:c.b]; v == "#" && len(faixas) > 0 { // coluna de índice (df.info())
				continue
			}
			if c.a == c.b || fixas == nil && !celulaCabecalho(s[c.a:c.b], sep, faixas != nil) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		// as linhas de dados
		var rows [][]celula
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
			if fixas != nil {
				vs, ok := alinharFixo(s, l, fixas, h.a)
				if !ok {
					break
				}
				rows = append(rows, vs)
				continue
			}
			rows = append(rows, celulasObj(s, l.a, l.b, sep, faixas))
		}
		if fixas != nil && len(rows) == 0 {
			continue
		}
		classificarTabelaD(s, cs, rows, tituloTipo(s, linhas, i), fixas != nil && len(rows) < 2, sep == 0 || sep == '|', d, add)
		i = k - 1
	}
}

// classificarTabela: o tipo de cada coluna pelo cabeçalho e os valores das colunas de objeto.
// titulo: o tipo que a linha acima do cabeçalho diz (para a coluna "name"). semCab: não marcar
// as células do cabeçalho como coluna (forma ainda pouco confirmada).
//
// Fora de CSV/TSV, o cabeçalho só vira coluna com "_" ou dígito (cd_cliente, col1): rótulos de
// ferramenta em PascalCase ("CreationDate", "LastModified") não são colunas do usuário.
func classificarTabela(s string, cs []celula, rows [][]celula, titulo string, semCab, estrito bool, add func(ObjAchado)) {
	classificarTabelaD(s, cs, rows, titulo, semCab, estrito, nil, add)
}

// classificarTabelaD: com a dica do comando (d != nil), só as colunas sem tipo pelo cabeçalho
// que a dica tipa (SELECT a lista, SHOW e listagens a coluna NAME); o resto já saiu sem a dica.
func classificarTabelaD(s string, cs []celula, rows [][]celula, titulo string, semCab, estrito bool, d *dicaSaida, add func(ObjAchado)) {
	emitir := add
	if d != nil {
		add = func(ObjAchado) {}
	}
	ent := map[int]string{}
	temSchema, nome, tipo := false, -1, -1
	for k, c := range cs {
		h := s[c.a:c.b]
		v := strings.ToLower(h)
		switch v {
		case "name", "nome":
			nome = k
			continue
		case "type", "data_type", "datatype", "tipo", "dtype":
			tipo = k
			continue
		}
		if e, ok := entCabecalho(h); ok {
			ent[k] = e
			temSchema = temSchema || e == "schema"
			continue
		}
		if m := reTablesIn.FindStringSubmatchIndex(h); m != nil { // MySQL: Tables_in_<banco>
			ent[k] = "tabela"
			add(ObjAchado{c.a + m[2], c.a + m[3], "database", "tabela-catálogo", true})
			continue
		}
		if !semCab && !rotulosSaida[v] && !publicoSQL(v) && !strings.Contains(h, " ") && caraDeIdentificador(h) && (!estrito || strings.ContainsAny(h, "_0123456789")) {
			add(ObjAchado{c.a, c.b, "coluna", "cabeçalho", false})
		}
	}
	if nome >= 0 {
		switch {
		case temSchema: // psql \dt: Schema | Name | Type | Owner
			ent[nome] = "tabela"
		case titulo != "": // aws --output table: o título da seção diz o tipo
			ent[nome] = titulo
		case tipo >= 0: // DESCRIBE: name | type, com tipos de dado na coluna type
			n, t := 0, 0
			for _, r := range rows {
				if tipo < len(r) && r[tipo].a >= 0 {
					n++
					if ehTipoDado(s[r[tipo].a:r[tipo].b]) {
						t++
					}
				}
			}
			if n > 0 && t*2 >= n {
				ent[nome] = "coluna"
			}
		}
	}
	if d != nil {
		porDica(s, cs, rows, ent, d, emitir)
		return
	}
	if len(ent) == 0 {
		return
	}
	for _, r := range rows {
		for col, e := range ent {
			if col >= len(r) || r[col].a < 0 {
				continue
			}
			c := r[col]
			v := s[c.a:c.b]
			if v == "" || !reCelulaIdent.MatchString(v) || strings.EqualFold(v, "null") || strings.EqualFold(v, "none") ||
				ehTipoDado(v) || strings.Trim(v, ".-") == "" {
				continue
			}
			addPartesCelula(s, c.a, c.b, e, add)
		}
	}
}

// porDica: os valores das colunas que o cabeçalho não tipou (ent) e a dica do comando tipa.
func porDica(s string, cs []celula, rows [][]celula, ent map[int]string, d *dicaSaida, add func(ObjAchado)) {
	tipos := map[int]string{}
	for k, c := range cs {
		if _, ja := ent[k]; ja {
			continue
		}
		if e := d.tipo(s[c.a:c.b], k, len(cs)); e != "" {
			tipos[k] = e
		}
	}
	for _, r := range rows {
		for col, e := range tipos {
			if col < len(r) {
				celulaDica(s, r[col], e, d.forte, add)
			}
		}
	}
}

// rotuloEValores: linha separada por TAB cuja primeira célula é uma palavra de tipo em
// maiúsculas ("BUCKETS\t2024-01-02\tbkt-x", saída em texto de CLIs de nuvem): as outras
// células com forma de nome são desse tipo.
func rotuloEValores(s string, l celula, add func(ObjAchado)) bool {
	linha := s[l.a:l.b]
	t := strings.IndexByte(linha, '\t')
	if t < 3 || strings.ToUpper(linha[:t]) != linha[:t] {
		return false
	}
	e, ok := entCabecalho(linha[:t])
	if !ok {
		return false
	}
	for _, c := range celulasObj(s, l.a+t+1, l.b, '\t', nil) {
		if v := s[c.a:c.b]; v != "" && reIdentSimples.MatchString(v) && valorRecurso(v, e) && !ehTipoDado(v) {
			add(ObjAchado{c.a, c.b, e, "tabela-rótulo", false})
		}
	}
	return true
}

// registroVertical: "-[ RECORD 1 ]-" seguido de "chave | valor" (psql \x), até a linha vazia.
// Devolve a linha em que parou.
func registroVertical(s string, linhas []celula, i int, add func(ObjAchado)) int {
	for ; i < len(linhas); i++ {
		l := linhas[i]
		linha := s[l.a:l.b]
		if strings.TrimSpace(linha) == "" {
			return i
		}
		if strings.HasPrefix(strings.TrimSpace(linha), "-[ RECORD ") {
			continue
		}
		cs := celulasObj(s, l.a, l.b, '|', nil)
		if len(cs) != 2 || cs[0].a == cs[0].b {
			continue
		}
		if e, ok := entCabecalho(s[cs[0].a:cs[0].b]); ok {
			if v := s[cs[1].a:cs[1].b]; v != "" && reCelulaIdent.MatchString(v) && !ehTipoDado(v) {
				addPartesCelula(s, cs[1].a, cs[1].b, e, add)
			}
		}
	}
	return i
}

// addPartesCelula: valor de coluna de catálogo, que pode ser qualificado (banco.schema.tabela).
func addPartesCelula(s string, a, b int, ult string, add func(ObjAchado)) {
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), ult)
	for k, p := range ps {
		if p != "" && !publicoSQL(p) && reIdentSimples.MatchString(p) {
			// valor curto (até 4 caracteres: códigos, siglas) mascara no lugar, mas não ensina sozinho
			add(ObjAchado{a, a + len(p), ents[k], "tabela-catálogo", ents[k] != "coluna" && len(p) >= 5})
		}
		a += len(p) + 1
	}
}

// ---------------------------------------------------------------------------------------
// Linhas que são tuplas ou listas: ('a', 'b') ou ['a', 'b'] por linha, lista de tuplas, lista
// de listas (cursor.fetchall(), csv.reader, planilhas lidas linha a linha). A primeira tupla é o
// cabeçalho quando todas as suas células são identificadores e alguma diz o tipo.

// tuplasEm: as tuplas/listas de literais de s, com as posições dos itens.
func tuplasEm(s string) [][]celula {
	var out [][]celula
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '(' && c != '[' {
			continue
		}
		fecha := byte(')')
		if c == '[' {
			fecha = ']'
		}
		var itens []celula
		j := i + 1
		ok := false
		for j < len(s) {
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j >= len(s) {
				break
			}
			if s[j] == '\'' || s[j] == '"' {
				q := s[j]
				k := j + 1
				for k < len(s) && s[k] != q && s[k] != '\n' {
					if s[k] == '\\' {
						k++
					}
					k++
				}
				if k >= len(s) || s[k] != q {
					break
				}
				itens = append(itens, celula{j + 1, k})
				j = k + 1
			} else {
				k := j
				for k < len(s) && (ehAlnum(s[k]) || s[k] == '.' || s[k] == '-' || s[k] == '_') {
					k++
				}
				if v := s[j:k]; k == j || !(todoDigitos(strings.Trim(v, ".-")) || v == "None" || v == "NULL" || v == "null" || v == "True" || v == "False" || v == "NA") {
					break
				}
				itens = append(itens, celula{-1, -1}) // número, None: não é nome
				j = k
			}
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < len(s) && s[j] == ',' {
				j++
				continue
			}
			if j < len(s) && s[j] == fecha {
				ok = true
			}
			break
		}
		if ok && len(itens) >= 2 {
			out = append(out, itens)
			i = j
		}
	}
	return out
}

func acharTuplas(s string, d *dicaSaida, add func(ObjAchado)) {
	if !strings.Contains(s, "', '") && !strings.Contains(s, `", "`) && !strings.Contains(s, "','") && !strings.Contains(s, `","`) {
		return
	}
	ts := tuplasEm(s)
	for i := 0; i < len(ts); {
		j := i + 1
		for j < len(ts) && len(ts[j]) == len(ts[i]) {
			j++
		}
		grupo := ts[i:j]
		i = j
		cab := grupo[0]
		temTipo, todos := false, true
		for _, c := range cab {
			if c.a < 0 || !reCelulaIdent.MatchString(s[c.a:c.b]) {
				todos = false
				break
			}
			if _, ok := entCabecalho(s[c.a:c.b]); ok || d != nil && d.nomes[strings.ToLower(s[c.a:c.b])] != "" {
				temTipo = true
			}
		}
		if d != nil && !temTipo && len(d.cols) == len(cab) { // cursor.fetchall(): sem cabeçalho
			for _, t := range grupo {
				for k, c := range t {
					if e := d.cols[k]; e != "" && c.a >= 0 {
						celulaDica(s, c, e, d.forte, add)
					}
				}
			}
			continue
		}
		if !todos || !temTipo || len(grupo) < 2 {
			continue
		}
		classificarTabelaD(s, cab, grupo[1:], "", false, false, d, add)
	}
}

// ---------------------------------------------------------------------------------------
// Tabela HTML: <tr><th>...</th></tr> e <tr><td>...</td></tr>.

var (
	reLinhaHTML  = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	reCelulaHTML = regexp.MustCompile(`(?is)<t([hd])\b[^>]*>\s*(.*?)\s*</t[hd]>`)
)

func acharTabelaHTML(s string, d *dicaSaida, add func(ObjAchado)) {
	if !strings.Contains(s, "<tr") && !strings.Contains(s, "<TR") {
		return
	}
	var cab []celula
	var rows [][]celula
	fechar := func() {
		if len(cab) >= 1 && len(rows) > 0 {
			classificarTabelaD(s, cab, rows, "", false, true, d, add)
		}
		cab, rows = nil, nil
	}
	ultimo := -1
	for _, m := range reLinhaHTML.FindAllStringSubmatchIndex(s, -1) {
		if ultimo >= 0 && strings.Contains(strings.ToLower(s[ultimo:m[0]]), "<table") {
			fechar()
		}
		ultimo = m[1]
		var cs []celula
		th := false
		for _, c := range reCelulaHTML.FindAllStringSubmatchIndex(s[m[2]:m[3]], -1) {
			th = th || s[m[2]+c[2]] == 'h' || s[m[2]+c[2]] == 'H'
			cs = append(cs, celula{m[2] + c[4], m[2] + c[5]})
		}
		if th || cab == nil {
			fechar()
			cab = cs
			continue
		}
		rows = append(rows, cs)
	}
	fechar()
}
