package mask

import (
	"regexp"
	"strings"
)

// Reader de tabelas (ver docs/pt-BR/estruturas.md, seções Saídas de clientes de banco e Dados
// tabulares). A tabela é reconhecida pela forma, não pela ferramenta: cabeçalho seguido de uma
// linha de traços (psql, sqlcmd, sqlplus, mysql, snowsql, bq, db2, markdown), linhas com o
// mesmo número de separadores fora de aspas (CSV conforme a RFC 4180, TSV, ";", "|"; as bordas
// de caixa chegam como "|" pela normalização), colunas alinhadas por espaços sem linha de traços
// (print de DataFrame, R, listagens de linha de comando), linhas que são tuplas ou listas,
// tabela HTML e registro vertical ("-[ RECORD 1 ]-" + "chave | valor"). Onde o cabeçalho diz que
// a coluna guarda nome de objeto (os nomes padronizados do information_schema e dos catálogos,
// ou uma palavra de tipo em qualquer grafia: ver entKey), os valores são objetos. As outras
// células do cabeçalho são nomes de coluna.

// cabeçalho de catálogo -> entidade dos valores daquela coluna
var catalogHdr = map[string]string{
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
var outputLabels = map[string]bool{"name": true, "type": true, "null": true, "key": true, "default": true, "extra": true,
	"rows": true, "count": true, "mean": true, "std": true, "min": true, "max": true, "dtype": true, "non-null": true,
	"labels": true, "id": true, "value": true, "description": true, "status": true, "size": true, "kind": true,
	"comment": true, "collation": true, "nullable": true, "length": true, "precision": true, "scale": true}

var (
	reSeparator = regexp.MustCompile(`^\s*[|+]?[\s|+:]*[-=─━]{3,}[-=─━+|:\s┼┬┴╋]*$`)
	reIdentCell = regexp.MustCompile(`^[\p{L}_][\p{L}0-9_$#.\-]*$`)
	// célula de cabeçalho: identificador, até 3 palavras ("primary key", "APP VERSION"), "null?"
	reHdrCell  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#.\-]*(?: [A-Za-z_][A-Za-z0-9_$#.\-]*){0,2}\??$`)
	reTablesIn = regexp.MustCompile(`(?i)^tables_in_([A-Za-z_][\w$]*)$`)
)

type cell struct{ a, b int } // posição no texto

// headerCell: a célula tem forma de cabeçalho de catálogo: um identificador ("null?"
// aceito) ou, em tabela de "|", TAB ou traços (não em CSV, onde a vírgula também separa listas
// de SQL), até 3 palavras todas em minúsculas ou todas em maiúsculas ("primary key", "APP
// VERSION"), que é como as ferramentas escrevem. Título de documento ("Data Type") não. Sob uma
// linha de traços, a faixa já delimita a célula: vale qualquer texto curto ("Non-Null Count").
func headerCell(v string, sep byte, dashes bool) bool {
	if !reHdrCell.MatchString(v) {
		return false
	}
	if !strings.Contains(v, " ") || dashes { // sob a linha de traços, a faixa já delimita a célula
		return true
	}
	return (sep == '|' || sep == '\t' || sep == 0) && (strings.ToLower(v) == v || strings.ToUpper(v) == v)
}

// entHeader: a entidade dos valores de uma coluna pelo nome do cabeçalho.
func entHeader(v string) (string, bool) {
	if e, ok := catalogHdr[strings.ToLower(v)]; ok {
		return e, true
	}
	if e, strong := entKey(v); strong && e != "" {
		return e, true
	}
	return "", false
}

// countOutside: quantos c há em l fora de aspas duplas (CSV: "a,b" é uma célula só).
func countOutside(l string, c byte) int {
	n, quotes := 0, false
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case '"':
			quotes = !quotes
		case c:
			if !quotes {
				n++
			}
		}
	}
	return n
}

// objCells divide a linha [ini, fim) de s em células: pelo separador sep (fora de aspas), ou
// pelas faixas de traços da linha separadora (tabela alinhada por espaços).
func objCells(s string, ini, end int, sep byte, ranges []cell) []cell {
	var out []cell
	add := func(a, b int) {
		for a < b && (s[a] == ' ' || s[a] == '\t' || s[a] == '"') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t' || s[b-1] == '\r' || s[b-1] == '"') {
			b--
		}
		out = append(out, cell{a, b})
	}
	if sep != 0 {
		a, quotes := ini, false
		for i := ini; i <= end; i++ {
			if i < end && s[i] == '"' && sep != '|' {
				quotes = !quotes
			}
			if i == end || s[i] == sep && !quotes {
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
	for _, f := range ranges {
		a, b := ini+f.a, ini+f.b
		if a >= end {
			out = append(out, cell{end, end})
			continue
		}
		add(a, min(b, end))
	}
	return out
}

// lineTokens: as palavras (separadas por espaço) de s[ini:fim).
func lineTokens(s string, ini, end int) []cell {
	var out []cell
	for i := ini; i < end; {
		for i < end && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		j := i
		for j < end && s[j] != ' ' && s[j] != '\t' {
			j++
		}
		if j > i {
			out = append(out, cell{i, j})
		}
		i = j
	}
	return out
}

// hasGap: a linha tem um vão de 2+ espaços entre palavras, um TAB, ou começa recuada.
func hasGap(l string) bool {
	t := strings.TrimRight(l, " \t")
	if strings.HasPrefix(t, " ") {
		return true
	}
	return strings.Contains(strings.TrimLeft(t, " "), "  ") || strings.Contains(t, "\t")
}

// looksLikeCode: a linha tem pontuação de código (bloco, atribuição, fim de instrução,
// comentário, operador, chamada em minúsculas): código recuado com espaços alinhados não é
// tabela.
func looksLikeCode(l string) bool {
	if strings.ContainsAny(l, "{};") || strings.Contains(l, ":=") || strings.Contains(l, "//") || strings.Contains(l, "==") ||
		strings.Contains(l, "!=") || strings.Contains(l, "&&") || strings.Contains(l, "||") || strings.Contains(l, " = ") {
		return true
	}
	for i := 1; i < len(l); i++ {
		if l[i] == '(' && (l[i-1] >= 'a' && l[i-1] <= 'z' || l[i-1] >= '0' && l[i-1] <= '9') {
			return true // f(x), int64(v)
		}
	}
	return false
}

var reFixedHdr = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$#.\-()/?]*$`)

// fixedColumns: a linha s[l] como cabeçalho de colunas alinhadas por espaços (cada palavra é
// uma coluna); nil se não tem a forma (menos de duas palavras, número, pontuação de frase).
func fixedColumns(s string, l cell) []cell {
	cs := lineTokens(s, l.a, l.b)
	if len(cs) < 2 || len(cs) > 40 {
		return nil
	}
	// prosa não tem colunas: o cabeçalho tem um vão de 2+ espaços ou um TAB entre as palavras
	// (recuo sozinho é parágrafo recuado de RST/Markdown; o DataFrame separa as colunas por 2+
	// espaços); as linhas de dados podem só começar recuadas (ver alignFixed)
	if h := strings.TrimSpace(s[l.a:l.b]); !strings.Contains(h, "  ") && !strings.Contains(h, "\t") || looksLikeCode(s[l.a:l.b]) {
		return nil
	}
	for _, c := range cs {
		if !reFixedHdr.MatchString(s[c.a:c.b]) {
			return nil
		}
	}
	return cs
}

// alignFixed: as células da linha l sob as colunas cab (posições relativas ao começo da
// linha). Cada palavra vai para a coluna com que mais se sobrepõe (valor alinhado à direita ou
// à esquerda) ou, sem sobreposição, para a coluna em que começa; palavra antes da primeira
// coluna é índice. ok=false se a linha não se alinha (menos da metade das colunas preenchida).
func alignFixed(s string, l cell, hdr []cell, hIni int) ([]cell, bool) {
	if !hasGap(s[l.a:l.b]) || looksLikeCode(s[l.a:l.b]) || len(lineTokens(s, l.a, l.b)) > 2*len(hdr)+4 {
		return nil, false
	}
	out := make([]cell, len(hdr))
	for k := range out {
		out[k] = cell{-1, -1}
	}
	n := 0
	for _, t := range lineTokens(s, l.a, l.b) {
		a, b := t.a-l.a, t.b-l.a
		best, under := -1, 0
		for k, c := range hdr {
			ca, cb := c.a-hIni, c.b-hIni
			if o := min(b, cb) - max(a, ca); o > under {
				best, under = k, o
			}
		}
		if best < 0 {
			for k := len(hdr) - 1; k >= 0; k-- {
				if a >= hdr[k].a-hIni {
					best = k
					break
				}
			}
		}
		if best < 0 {
			continue // índice à esquerda
		}
		if out[best].a < 0 {
			out[best] = t
			n++
		} else {
			out[best].b = t.b // valor com espaços ("2 days ago")
		}
	}
	return out, n >= 2 && n*2 >= len(hdr)
}

// typeTitle: a linha logo acima do cabeçalho (até 3 linhas, sem bordas) é só uma palavra de
// tipo ("Buckets", "TABLES")? Então a coluna "name" guarda nomes desse tipo.
func typeTitle(s string, lines []cell, i int) string {
	for k := i - 1; k >= 0 && k >= i-3; k-- {
		l := strings.Trim(s[lines[k].a:lines[k].b], " \t|+-=")
		if l == "" {
			continue
		}
		if strings.ContainsAny(l, " \t") {
			return ""
		}
		e, _ := entHeader(strings.ToLower(l))
		return e
	}
	return ""
}

func findTablesObj(s string, add func(ObjFinding)) { findTablesD(s, nil, add) }

// findTablesD: com d != nil (a dica do comando, ver chamada_comando.go), só as colunas que o cabeçalho
// não tipa e a dica tipa (o resto já saiu sem a dica).
func findTablesD(s string, d *outputHint, add func(ObjFinding)) {
	addWithoutHint := add
	if d != nil {
		addWithoutHint = func(ObjFinding) {}
	}
	findTuples(s, d, add)
	findHTMLTable(s, d, add)
	if strings.Count(s, "\n") < 2 {
		return
	}
	var lines []cell // [ini, fim) de cada linha
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		if j < 0 {
			lines = append(lines, cell{i, len(s)})
			break
		}
		lines = append(lines, cell{i, i + j})
		i += j + 1
	}
	label := make([]bool, len(lines))
	for i, l := range lines {
		label[i] = strings.IndexByte(s[l.a:l.b], '\t') > 0 && labelAndValues(s, l, addWithoutHint)
	}
	for i := 0; i+1 < len(lines); i++ {
		if label[i] {
			continue
		}
		if k := looseColumn(s, lines, i, add); k > i {
			i = k - 1
			continue
		}
		h, next := lines[i], lines[i+1]
		hdr := s[h.a:h.b]
		if strings.TrimSpace(hdr) == "" || reSeparator.MatchString(hdr) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(hdr), "-[ RECORD ") {
			i = verticalRecord(s, lines, i+1, addWithoutHint) - 1
			continue
		}
		var sep byte
		var ranges, fixedOnes []cell
		data := i + 1
		if reSeparator.MatchString(s[next.a:next.b]) {
			data = i + 2
			if strings.Contains(hdr, "|") {
				sep = '|'
			} else { // faixas de traços da linha separadora
				l := s[next.a:next.b]
				for k := 0; k < len(l); {
					if l[k] == '-' || l[k] == '=' {
						a := k
						for k < len(l) && (l[k] == '-' || l[k] == '=') {
							k++
						}
						ranges = append(ranges, cell{a, k})
					} else {
						k++
					}
				}
				if len(ranges) < 1 {
					continue
				}
				if len(ranges) > 1 { // cada faixa vai até a próxima (valores podem passar dos traços)
					for k := 0; k+1 < len(ranges); k++ {
						ranges[k].b = ranges[k+1].a
					}
				}
				ranges[len(ranges)-1].b = 1 << 20
			}
		} else {
			for _, c := range []byte{'\t', ',', ';', '|'} {
				n := countOutside(hdr, c)
				if n >= 1 && countOutside(s[next.a:next.b], c) == n {
					sep = c
					break
				}
			}
			if sep == 0 {
				fixedOnes = fixedColumns(s, h)
				if fixedOnes == nil {
					continue
				}
			}
		}
		var cs []cell
		if fixedOnes != nil {
			cs = fixedOnes
		} else {
			cs = objCells(s, h.a, h.b, sep, ranges)
		}
		if len(cs) < 2 {
			continue
		}
		ok := true
		for _, c := range cs {
			// cabeçalho com forma de identificador: sem espaço, operador ou unidade ("Speed MiB/s",
			// "for i := 0" são tabela de benchmark e trecho de código, não catálogo)
			if v := s[c.a:c.b]; v == "#" && len(ranges) > 0 { // coluna de índice (df.info())
				continue
			}
			if c.a == c.b || fixedOnes == nil && !headerCell(s[c.a:c.b], sep, ranges != nil) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		// as linhas de dados
		var rows [][]cell
		k := data
		for ; k < len(lines); k++ {
			l := lines[k]
			line := s[l.a:l.b]
			if strings.TrimSpace(line) == "" {
				break
			}
			if reSeparator.MatchString(line) {
				continue
			}
			if fixedOnes != nil {
				vs, ok := alignFixed(s, l, fixedOnes, h.a)
				if !ok {
					break
				}
				rows = append(rows, vs)
				continue
			}
			rows = append(rows, objCells(s, l.a, l.b, sep, ranges))
		}
		if fixedOnes != nil && len(rows) == 0 {
			continue
		}
		noHdr := fixedOnes != nil && len(rows) < 2
		var dv *outputHint
		if d == nil {
			// a listagem à vista logo acima (terminal colado, README: "$ airflow dags list"): o
			// cabeçalho é da ferramenta, não coluna do dono, e a listagem tipa a coluna de
			// identidade como no tool_use. Só listagem: debaixo de "$ cat x.csv" ou de
			// "$ python relatorio.py" o cabeçalho é do dono e continua sendo coluna
			if cmd, ok := commandAbove(s, lines, i); ok {
				if dv = listingHint(cmd); dv != nil {
					noHdr = true
				}
			}
		}
		classifyTableD(s, cs, rows, typeTitle(s, lines, i), noHdr, sep == 0 || sep == '|', d, add)
		if dv != nil {
			classifyTableD(s, cs, rows, "", true, sep == 0 || sep == '|', dv, add)
		}
		i = k - 1
	}
}

// linha de prompt de shell: "$ cmd", "% cmd", "user@host:~/x$ cmd", "(venv) $ cmd", "PS C:\x> cmd"
var rePromptShell = regexp.MustCompile(`^(?:\([\w.-]+\)\s*)?(?:[\w.-]+@[\w.-]+(?::[^\s$%]*)?\s*)?[$%]\s+(\S.*)$|^PS [^>\n]{0,80}>\s+(\S.*)$`)

// linha que é ela mesma um comando: programa em minúsculas e argumentos, sem pontuação de frase
var reCommandLine = regexp.MustCompile(`^[a-z][\w.-]*(?:\s+[\w./:=@,-]+){1,12}$`)

// SQL no comando (psql -c "select ...", mysql -e "show tables"): o cabeçalho são as colunas
// da consulta, não campos da ferramenta
var reSQLInCommand = regexp.MustCompile(`(?i)\b(?:select|insert|update|delete|merge|show|describe|desc|with)\b`)

// commandAbove: o comando de shell (não SQL) até 3 linhas acima do cabeçalho linhas[i]: uma
// linha de prompt ou, logo acima, uma linha que é um comando de listagem ("airflow dags list").
func commandAbove(s string, lines []cell, i int) (string, bool) {
	for k := i - 1; k >= 0 && k >= i-3; k-- {
		l := strings.TrimSpace(s[lines[k].a:lines[k].b])
		cmd := ""
		if m := rePromptShell.FindStringSubmatch(l); m != nil {
			cmd = m[1] + m[2]
		} else if k == i-1 && reCommandLine.MatchString(l) && listingHint(l) != nil {
			cmd = l
		}
		if cmd != "" {
			return cmd, !reSQLInCommand.MatchString(cmd)
		}
	}
	return "", false
}

// classifyTable: o tipo de cada coluna pelo cabeçalho e os valores das colunas de objeto.
// titulo: o tipo que a linha acima do cabeçalho diz (para a coluna "name"). noHdr: não marcar
// as células do cabeçalho como coluna (forma ainda pouco confirmada).
//
// Fora de CSV/TSV, o cabeçalho só vira coluna com "_" ou dígito (cd_cliente, col1): rótulos de
// ferramenta em PascalCase ("CreationDate", "LastModified") não são colunas do usuário.
func classifyTable(s string, cs []cell, rows [][]cell, title string, noHdr, strict bool, add func(ObjFinding)) {
	classifyTableD(s, cs, rows, title, noHdr, strict, nil, add)
}

// classifyTableD: com a dica do comando (d != nil), só as colunas sem tipo pelo cabeçalho
// que a dica tipa (SELECT a lista, SHOW e listagens a coluna NAME); o resto já saiu sem a dica.
func classifyTableD(s string, cs []cell, rows [][]cell, title string, noHdr, strict bool, d *outputHint, add func(ObjFinding)) {
	emitIt := add
	if d != nil {
		add = func(ObjFinding) {}
	}
	ent := map[int]string{}
	hasSchema, name, typ := false, -1, -1
	for k, c := range cs {
		h := s[c.a:c.b]
		v := strings.ToLower(h)
		switch v {
		case "name", "nome":
			name = k
			continue
		case "type", "data_type", "datatype", "tipo", "dtype":
			typ = k
			continue
		}
		if e, ok := entHeader(h); ok {
			ent[k] = e
			hasSchema = hasSchema || e == "schema"
			continue
		}
		if m := reTablesIn.FindStringSubmatchIndex(h); m != nil { // MySQL: Tables_in_<banco>
			ent[k] = "tabela"
			add(ObjFinding{c.a + m[2], c.a + m[3], "database", "tabela-catálogo", true})
			continue
		}
		if !noHdr && !outputLabels[v] && !publicSQL(v) && !strings.ContainsAny(h, " ().") && identifierShape(h) && (!strict || strings.ContainsAny(h, "_0123456789")) {
			add(ObjFinding{c.a, c.b, "coluna", "cabeçalho", false})
		}
	}
	if name >= 0 {
		switch {
		case hasSchema: // psql \dt: Schema | Name | Type | Owner
			ent[name] = "tabela"
		case title != "": // aws --output table: o título da seção diz o tipo
			ent[name] = title
		case typ >= 0: // DESCRIBE: name | type, com tipos de dado na coluna type
			n, t := 0, 0
			for _, r := range rows {
				if typ < len(r) && r[typ].a >= 0 {
					n++
					if isDataType(s[r[typ].a:r[typ].b]) {
						t++
					}
				}
			}
			if n > 0 && t*2 >= n {
				ent[name] = "coluna"
			}
		}
	}
	if d != nil {
		byHint(s, cs, rows, ent, d, emitIt)
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
			if len(v) < 2 || !reIdentCell.MatchString(v) || strings.EqualFold(v, "null") || strings.EqualFold(v, "none") ||
				isDataType(v) || strings.Trim(v, ".-") == "" {
				continue
			}
			addCellParts(s, c.a, c.b, e, add)
		}
	}
}

// byHint: os valores das colunas que o cabeçalho não tipou (ent) e a dica do comando tipa.
func byHint(s string, cs []cell, rows [][]cell, ent map[int]string, d *outputHint, add func(ObjFinding)) {
	types := map[int]string{}
	for k, c := range cs {
		if _, already := ent[k]; already {
			continue
		}
		if e := d.typ(s[c.a:c.b], k, len(cs)); e != "" {
			types[k] = e
		}
	}
	for _, r := range rows {
		for col, e := range types {
			if col < len(r) {
				hintCell(s, r[col], e, d.strong, add)
			}
		}
	}
}

// looseColumn: uma coluna só, com o cabeçalho sendo uma palavra de tipo ("bucket",
// "table_name") e 2+ linhas de um item cada com forma de nome, até a linha vazia. Devolve a
// linha em que parou (i se não é coluna solta).
func looseColumn(s string, lines []cell, i int, add func(ObjFinding)) int {
	h := strings.TrimSpace(s[lines[i].a:lines[i].b])
	if h == "" || strings.ContainsAny(h, " \t,;|") {
		return i
	}
	e, ok := entHeader(h)
	if !ok {
		return i
	}
	k := i + 1
	var vs []cell
	for ; k < len(lines); k++ {
		l := lines[k]
		a, b := l.a, l.b
		for a < b && (s[a] == ' ' || s[a] == '\t') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\t' || s[b-1] == '\r') {
			b--
		}
		if a == b {
			break
		}
		v := s[a:b]
		if !reIdentCell.MatchString(v) || !identifierShape(v) {
			return i
		}
		vs = append(vs, cell{a, b})
	}
	if len(vs) < 2 {
		return i
	}
	for _, c := range vs {
		addCellParts(s, c.a, c.b, e, add)
	}
	return k
}

// labelAndValues: linha separada por TAB cuja primeira célula é uma palavra de tipo em
// maiúsculas ("BUCKETS\t2024-01-02\tbkt-x", saída em texto de CLIs de nuvem): as outras
// células com forma de nome são desse tipo.
func labelAndValues(s string, l cell, add func(ObjFinding)) bool {
	line := s[l.a:l.b]
	t := strings.IndexByte(line, '\t')
	if t < 3 || strings.ToUpper(line[:t]) != line[:t] {
		return false
	}
	e, ok := entHeader(line[:t])
	if !ok {
		return false
	}
	for _, c := range objCells(s, l.a+t+1, l.b, '\t', nil) {
		if v := s[c.a:c.b]; v != "" && reSimpleIdent.MatchString(v) && resourceValue(v, e) && !isDataType(v) {
			add(ObjFinding{c.a, c.b, e, "tabela-rótulo", false})
		}
	}
	return true
}

// verticalRecord: "-[ RECORD 1 ]-" seguido de "chave | valor" (psql \x), até a linha vazia.
// Devolve a linha em que parou.
func verticalRecord(s string, lines []cell, i int, add func(ObjFinding)) int {
	for ; i < len(lines); i++ {
		l := lines[i]
		line := s[l.a:l.b]
		if strings.TrimSpace(line) == "" {
			return i
		}
		if strings.HasPrefix(strings.TrimSpace(line), "-[ RECORD ") {
			continue
		}
		cs := objCells(s, l.a, l.b, '|', nil)
		if len(cs) != 2 || cs[0].a == cs[0].b {
			continue
		}
		if e, ok := entHeader(s[cs[0].a:cs[0].b]); ok {
			if v := s[cs[1].a:cs[1].b]; v != "" && reIdentCell.MatchString(v) && !isDataType(v) {
				addCellParts(s, cs[1].a, cs[1].b, e, add)
			}
		}
	}
	return i
}

// addCellParts: valor de coluna de catálogo, que pode ser qualificado (banco.schema.tabela).
func addCellParts(s string, a, b int, lastPos string, add func(ObjFinding)) {
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), lastPos)
	for k, p := range ps {
		if p != "" && !publicSQL(p) && reSimpleIdent.MatchString(p) {
			// valor curto (até 4 caracteres: códigos, siglas) mascara no lugar, mas não ensina sozinho
			add(ObjFinding{a, a + len(p), ents[k], "tabela-catálogo", ents[k] != "coluna" && len(p) >= 5})
		}
		a += len(p) + 1
	}
}

// ---------------------------------------------------------------------------------------
// Linhas que são tuplas ou listas: ('a', 'b') ou ['a', 'b'] por linha, lista de tuplas, lista
// de listas (cursor.fetchall(), csv.reader, planilhas lidas linha a linha). A primeira tupla é o
// cabeçalho quando todas as suas células são identificadores e alguma diz o tipo.

// tuplesIn: as tuplas/listas de literais de s, com as posições dos itens.
func tuplesIn(s string) [][]cell {
	var out [][]cell
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '(' && c != '[' {
			continue
		}
		closes := byte(')')
		if c == '[' {
			closes = ']'
		}
		var items []cell
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
				items = append(items, cell{j + 1, k})
				j = k + 1
			} else {
				k := j
				for k < len(s) && (isAlnum(s[k]) || s[k] == '.' || s[k] == '-' || s[k] == '_') {
					k++
				}
				if v := s[j:k]; k == j || !(allDigits(strings.Trim(v, ".-")) || v == "None" || v == "NULL" || v == "null" || v == "True" || v == "False" || v == "NA") {
					break
				}
				items = append(items, cell{-1, -1}) // número, None: não é nome
				j = k
			}
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < len(s) && s[j] == ',' {
				j++
				continue
			}
			if j < len(s) && s[j] == closes {
				ok = true
			}
			break
		}
		if ok && len(items) >= 2 {
			out = append(out, items)
			i = j
		}
	}
	return out
}

func findTuples(s string, d *outputHint, add func(ObjFinding)) {
	if !strings.Contains(s, "', '") && !strings.Contains(s, `", "`) && !strings.Contains(s, "','") && !strings.Contains(s, `","`) {
		return
	}
	ts := tuplesIn(s)
	for i := 0; i < len(ts); {
		j := i + 1
		for j < len(ts) && len(ts[j]) == len(ts[i]) {
			j++
		}
		group := ts[i:j]
		i = j
		hdr := group[0]
		hasType, everyone := false, true
		for _, c := range hdr {
			if c.a < 0 || !reIdentCell.MatchString(s[c.a:c.b]) {
				everyone = false
				break
			}
			if _, ok := entHeader(s[c.a:c.b]); ok || d != nil && d.names[strings.ToLower(s[c.a:c.b])] != "" {
				hasType = true
			}
		}
		if d != nil && !hasType && len(d.cols) == len(hdr) { // cursor.fetchall(): sem cabeçalho
			for _, t := range group {
				for k, c := range t {
					if e := d.cols[k]; e != "" && c.a >= 0 {
						hintCell(s, c, e, d.strong, add)
					}
				}
			}
			continue
		}
		if !everyone || !hasType || len(group) < 2 {
			continue
		}
		classifyTableD(s, hdr, group[1:], "", false, false, d, add)
	}
}

// ---------------------------------------------------------------------------------------
// Table HTML: <tr><th>...</th></tr> e <tr><td>...</td></tr>.

var (
	reHTMLLine = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	reHTMLCell = regexp.MustCompile(`(?is)<t([hd])\b[^>]*>\s*(.*?)\s*</t[hd]>`)
)

func findHTMLTable(s string, d *outputHint, add func(ObjFinding)) {
	if !strings.Contains(s, "<tr") && !strings.Contains(s, "<TR") {
		return
	}
	var hdr []cell
	var rows [][]cell
	closeIt := func() {
		if len(hdr) >= 1 && len(rows) > 0 {
			classifyTableD(s, hdr, rows, "", false, true, d, add)
		}
		hdr, rows = nil, nil
	}
	lastOne := -1
	for _, m := range reHTMLLine.FindAllStringSubmatchIndex(s, -1) {
		if lastOne >= 0 && strings.Contains(strings.ToLower(s[lastOne:m[0]]), "<table") {
			closeIt()
		}
		lastOne = m[1]
		var cs []cell
		th := false
		for _, c := range reHTMLCell.FindAllStringSubmatchIndex(s[m[2]:m[3]], -1) {
			th = th || s[m[2]+c[2]] == 'h' || s[m[2]+c[2]] == 'H'
			cs = append(cs, cell{m[2] + c[4], m[2] + c[5]})
		}
		if th || hdr == nil {
			closeIt()
			hdr = cs
			continue
		}
		rows = append(rows, cs)
	}
	closeIt()
}
