package mask

import (
	_ "embed"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Leitor de SQL e DDL (ver docs/estruturas.md, seção SQL e DDL). Uma instrução só vale se tiver
// a forma mínima da gramática (SELECT ... FROM x, INSERT INTO x, CREATE TABLE x...). Dentro
// dela, o nome depois de FROM, JOIN, INTO, UPDATE, TABLE, VIEW, PROCEDURE, DATABASE, SCHEMA,
// INDEX... é objeto (tabela, procedure, banco, schema, índice), com as partes de um nome
// qualificado servidor.banco.schema.objeto; os outros identificadores são colunas.

//go:embed vocab_sql.txt
var vocabSQLTxt string

var vocabSQL = func() map[string]bool {
	m := map[string]bool{}
	for _, l := range strings.Split(vocabSQLTxt, "\n") {
		if strings.HasPrefix(l, "#") {
			continue
		}
		for _, w := range strings.Fields(l) {
			m[strings.ToLower(w)] = true
		}
	}
	return m
}()

func publicoSQL(v string) bool { return vocabSQL[strings.ToLower(v)] }

// objetos de conta (papel, usuário, warehouse, integração) e de schema (políticas, stage,
// stream, tarefa, pipe, sequência, formato de arquivo, tag, alerta) nas instruções DDL
const (
	reTiposConta  = `(?:DATABASE\s+)?ROLE|USER|WAREHOUSE|(?:STORAGE\s+|API\s+|NOTIFICATION\s+|SECURITY\s+)?INTEGRATION|RESOURCE\s+MONITOR`
	reTiposSchema = `(?:MASKING|ROW\s+ACCESS|NETWORK|PASSWORD|SESSION|AGGREGATION|PROJECTION|AUTHENTICATION)\s+POLICY|TAG|FILE\s+FORMAT|SECRET|ALERT|DYNAMIC\s+TABLE|NOTEBOOK`
)

const (
	reIdSQL   = "(?:\\[[^\\]\\n]{1,128}\\]|\"[^\"\\n]{1,128}\"|`[^`\\n]{1,128}`|[\\p{L}_][\\p{L}0-9_$#]*)"
	reQualSQL = reIdSQL + "(?:\\." + reIdSQL + ")*"
)

var (
	// forma mínima de cada instrução: a gramática exige essas peças, nessa ordem
	reFormaSQL = regexp.MustCompile(`(?is)^(?:SELECT\b[^;]*?\bFROM\s+` + reQualSQL + `|WITH\s+(?:RECURSIVE\s+)?` + reIdSQL + `\s*(?:\([^)]{0,500}\)\s*)?AS\s*\(` +
		`|INSERT\s+(?:INTO\s+|OVERWRITE\s+(?:TABLE\s+)?)` + reQualSQL + `|UPDATE\s+` + reQualSQL + `(?:\s+(?:AS\s+)?\w+)?\s+SET\s+(?:\(|` + reQualSQL + `\s*=)|DELETE\s+FROM\s+` + reQualSQL +
		`|MERGE\s+INTO\s+` + reQualSQL + `|(?:CREATE|ALTER|DROP)\s+(?:OR\s+(?:REPLACE|ALTER)\s+)?(?:(?:GLOBAL|LOCAL|SECURE|EXTERNAL|MATERIALIZED|TRANSIENT|TEMP(?:ORARY)?|UNIQUE|CLUSTERED|NONCLUSTERED)\s+)*` +
		`(?:TABLE|VIEW|PROCEDURE|PROC|FUNCTION|TRIGGER|SCHEMA|DATABASE|SEQUENCE|INDEX|STAGE|TASK|PIPE|STREAM|SYNONYM|PACKAGE(?:\s+BODY)?|` + reTiposConta + `|` + reTiposSchema + `)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?` + reQualSQL +
		`|TRUNCATE\s+TABLE\s+` + reQualSQL + `|COPY\s+INTO\s+'?@?` + reQualSQL + `|(?:EXEC|EXECUTE|CALL)\s+` + reQualSQL + `|USE\s+(?:ROLE\s+|WAREHOUSE\s+|DATABASE\s+|SCHEMA\s+|SECONDARY\s+ROLES\s+)?` + reQualSQL + `\s*(?:;|$|\n)` +
		`|(?:GRANT|REVOKE)\s+(?:DATABASE\s+)?ROLE\s+` + reQualSQL + `\s+(?:TO|FROM)\s+` +
		`|(?:GRANT|REVOKE)\s[^;]{0,300}?\bON\s+(?:TABLE\s+|SCHEMA\s+|DATABASE\s+|VIEW\s+)?` + reQualSQL +
		`|(?:DESCRIBE|DESC)\s+(?:TABLE\s+)?` + reQualSQL + `\s*(?:;|$|\n)|SHOW\s+\w+(?:\s+\w+)?\s+(?:IN|FROM)\s+` + reQualSQL + `)`)
	reClausulasSQL = regexp.MustCompile(`(?i)\b(FROM|WHERE|JOIN|GROUP\s+BY|ORDER\s+BY|SET|VALUES|INTO|HAVING|UNION|AS|ON|TABLE)\b`)
	// linha que já não é SQL (código em volta): para a instrução ali
	reNaoSQL   = regexp.MustCompile(`^\s*(?:def |class |func |return\b|if\s*\(|for\s*\(|import |from \S+ import|package |\}|\)\s*$|#|//|print\(|echo |cd |\$ )`)
	reFimSQL   = regexp.MustCompile("(?m);|\\n[ \\t]*\\n|```")
	reComSQL   = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	reLitSQL   = regexp.MustCompile(`(?s)'(?:[^']|'')*'|--[^\n]*|/\*.*?\*/`)
	reQualTok  = regexp.MustCompile(reQualSQL)
	reParteSQL = regexp.MustCompile(reIdSQL)
	// posições de objeto: palavra-chave -> entidade do último pedaço
	// tem: palavras (em maiúsculas) sem as quais a regex não casa; evita rodá-la à toa
	rePosObjSQL = []struct {
		re  *regexp.Regexp
		ent string
		tem []string
	}{
		{regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE|TABLE|VIEW|REFERENCES|USING|OVERWRITE)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+|ONLY\s+)?(` + reQualSQL + `)`), "tabela", nil},
		{regexp.MustCompile(`(?i)\b(?:PROCEDURE|PROC|FUNCTION|TRIGGER|PACKAGE(?:\s+BODY)?|EXEC|EXECUTE|CALL)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reQualSQL + `)`), "procedure", []string{"PROC", "FUNCTION", "TRIGGER", "PACKAGE", "EXEC", "CALL"}},
		{regexp.MustCompile(`(?i)\b(?:USE|DATABASE)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reQualSQL + `)`), "database", []string{"USE", "DATABASE"}},
		{regexp.MustCompile(`(?i)\bSCHEMA\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reQualSQL + `)`), "schema", []string{"SCHEMA"}},
		{regexp.MustCompile(`(?i)\bINDEX\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reIdSQL + `)`), "indice", []string{"INDEX"}},
		{regexp.MustCompile(`(?i)\bCONSTRAINT\s+(` + reIdSQL + `)`), "indice", []string{"CONSTRAINT"}},
		{regexp.MustCompile(`(?i)\b(?:ROLE|USER)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reIdSQL + `)`), "usuario", []string{"ROLE", "USER"}},
		{regexp.MustCompile(`(?i)\bWAREHOUSE\s*(?:=\s*)?(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reIdSQL + `)`), "servico", []string{"WAREHOUSE"}},
		{regexp.MustCompile(`(?i)\b(?:` + reTiposSchema + `|STAGE|STREAM|TASK|PIPE|SEQUENCE)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(` + reQualSQL + `)`), "tabela",
			[]string{"POLICY", "TAG", "FORMAT", "SECRET", "ALERT", "DYNAMIC", "NOTEBOOK", "STAGE", "STREAM", "TASK", "PIPE", "SEQUENCE"}},
		{regexp.MustCompile(`(?i)(?:\bFROM|\bINTO|\bLIST|\bLS|\bREMOVE|\bRM|=)\s*'?@(` + reQualSQL + `)`), "tabela", []string{"@"}},
	}
	reOnObjSQL = regexp.MustCompile(`(?i)\bON\s+(` + reQualSQL + `)\s*(?:\(|TO\b|FROM\b|;|$)`)
	reListaSQL = regexp.MustCompile(`^\s*(?:(?:AS\s+)?[A-Za-z_]\w*\s*)?,\s*(` + reQualSQL + `)`)
)

func temAlguma(s string, ps []string) bool {
	for _, p := range ps {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// entQual: as entidades de cada parte de um nome qualificado a.b.c.d (ult = a do último).
func entQual(n int, ult string) []string {
	acima := []string{"schema", "database", "servidor"}
	if ult == "coluna" {
		acima = []string{"tabela", "schema", "database"}
	} else if ult == "schema" {
		acima = []string{"database", "servidor"}
	} else if ult == "database" {
		acima = []string{"servidor"}
	}
	out := make([]string, n)
	out[n-1] = ult
	for i := n - 2; i >= 0; i-- {
		k := n - 2 - i
		if k >= len(acima) {
			k = len(acima) - 1
		}
		out[i] = acima[k]
	}
	return out
}

// partesSQL: as partes de um nome qualificado em lit[a:b]. No BigQuery o nome inteiro pode
// estar entre crases com os pontos dentro (`projeto.dataset.tabela`): cada parte vale.
func partesSQL(lit string, a, b int) [][2]int {
	var out [][2]int
	for _, p := range partesSQLEm(lit[a:b]) {
		x, y := a+p[0], a+p[1]
		if lit[x] == '`' && strings.Contains(lit[x:y], ".") {
			k := x + 1
			for _, q := range strings.Split(lit[x+1:y-1], ".") {
				if q != "" {
					out = append(out, [2]int{k, k + len(q)})
				}
				k += len(q) + 1
			}
			continue
		}
		out = append(out, [2]int{x, y})
	}
	return out
}

// tirarCitacao: início e fim do nome sem os colchetes/aspas/crases em volta.
func tirarCitacao(s string, a, b int) (int, int) {
	if b-a >= 2 {
		switch s[a] {
		case '[', '"', '`':
			return a + 1, b - 1
		}
	}
	return a, b
}

// fimInstrucao: onde termina a instrução que começa em i.
func fimInstrucao(s string, i, j int) int {
	lim := min(len(s), i+8000)
	// dentro de uma string de código: termina na aspa que fecha a string
	antes := strings.TrimRight(s[max(0, i-6):i], " \t(")
	antes = strings.TrimRight(antes, "fFrRbBuU")
	for _, q := range []string{`"""`, `'''`, `"`, `'`, "`"} {
		if strings.HasSuffix(antes, q) {
			if k := strings.Index(s[j:lim], q); k >= 0 {
				return j + k
			}
			return lim
		}
	}
	fim := lim
	if loc := reFimSQL.FindStringIndex(s[j:lim]); loc != nil {
		fim = j + loc[0]
	}
	for o := strings.IndexByte(s[j:fim], '\n'); o >= 0; {
		p := j + o + 1
		if reNaoSQL.MatchString(s[p:min(fim, p+120)]) {
			return p - 1
		}
		k := strings.IndexByte(s[p:fim], '\n')
		if k < 0 {
			break
		}
		o = p - j + k
	}
	return fim
}

// iniciosSQL: as palavras que começam uma instrução (em maiúsculas).
var iniciosSQL = map[string]bool{"SELECT": true, "WITH": true, "INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "EXEC": true, "EXECUTE": true, "CALL": true, "USE": true,
	"GRANT": true, "REVOKE": true, "DESCRIBE": true, "SHOW": true, "COPY": true}

// palavrasInicio chama fn(i, j) para cada palavra de s que começa uma instrução SQL. Anda
// palavra a palavra (bem mais rápido que uma regex sem diferença de caixa no texto inteiro).
func palavrasInicio(s string, fn func(i, j int)) {
	var buf [8]byte
	for i := 0; i < len(s); {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') || i > 0 && (ehIdent(s[i-1]) || s[i-1] == '.') {
			i++
			continue
		}
		j := i
		for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
			j++
		}
		if n := j - i; n >= 3 && n <= 8 && (j == len(s) || !ehIdent(s[j])) {
			for k := 0; k < n; k++ {
				buf[k] = s[i+k] &^ 0x20
			}
			if iniciosSQL[string(buf[:n])] {
				fn(i, j)
			}
		}
		i = j
	}
}

func acharSQL(s string, add func(ObjAchado)) {
	ate := 0
	palavrasInicio(s, func(i, j int) {
		if i < ate {
			return
		}
		maiusc := strings.ToUpper(s[i:j]) == s[i:j]
		kw := strings.ToUpper(s[i:j])
		// palavras comuns em prosa (use, copy, show, call, exec...): fora de maiúsculas, só com
		// forma inequívoca
		prosa := !maiusc && (kw == "USE" || kw == "COPY" || kw == "SHOW" || kw == "DESCRIBE" || kw == "CALL" || kw == "EXEC" || kw == "EXECUTE" || kw == "GRANT" || kw == "REVOKE")
		fim := fimInstrucao(s, i, j)
		corpo := s[i:fim]
		// a forma é lida sem os comentários ("UPDATE t /* x */ SET", "FROM t -- x\nWHERE")
		forma := corpo
		if strings.Contains(corpo, "--") || strings.Contains(corpo, "/*") {
			forma = reComSQL.ReplaceAllStringFunc(corpo, func(x string) string { return strings.Repeat(" ", len(x)) })
		}
		// a frase continua em prosa depois das palavras-chave ("o SELECT pega os dados FROM da
		// tabela certa"): a instrução acaba onde começa a prosa
		if k := inicioProsaSQL(forma); k >= 0 {
			fim, corpo, forma = i+k, corpo[:k], forma[:k]
		}
		if !reFormaSQL.MatchString(forma) {
			return
		}
		if !maiusc && !instrucaoForaDeProsa(s, i, forma, kw, prosa) {
			return
		}
		if maiusc && !inicioDeInstrucao(s, i) {
			return
		}
		claus := contarClausulas(forma, 4)
		// uma cláusula só: em minúsculas, só vale com forma inequívoca; em maiúsculas vale
		// sempre, e ensina quando a forma também é inequívoca
		inequivoca := false
		if claus < 2 || prosa {
			if inequivoca = formaInequivoca(forma, kw); !inequivoca && (!maiusc || prosa) {
				return
			}
		}
		ate = fim
		forte := claus >= 2 || inequivoca
		instrucaoSQL(s, i, corpo, kw, forte, add)
	})
}

// inicioProsaSQL: onde, no corpo de uma instrução, começa uma sequência de 3 ou mais palavras
// soltas lado a lado (só espaço entre elas): palavras em minúsculas, sem "_" nem dígito, que
// não são do vocabulário do SQL, não são qualificadas (a.b) nem chamada de função. Na gramática
// do SQL, no máximo duas ficam assim lado a lado (nome e apelido: "pedidos p", "a AS b"); três
// são prosa. Devolve -1 se não houver.
func inicioProsaSQL(corpo string) int {
	lit := reLitSQL.ReplaceAllStringFunc(corpo, func(x string) string { return strings.Repeat(" ", len(x)) })
	run, ini := 0, -1
	for _, p := range partesSQLEm(lit) {
		a, b := p[0], p[1]
		v := lit[a:b]
		solta := strings.ToLower(v) == v && !publicoSQL(v) && !caraDeIdentificador(v) && strings.IndexByte(v, '_') < 0 &&
			!strings.ContainsAny(v, "[\"`$#") && !(a > 1 && lit[a-1] == '.' && ehIdent(lit[a-2])) &&
			(b == len(lit) || lit[b] != '(' && !(lit[b] == '.' && b+1 < len(lit) && (ehIdent(lit[b+1]) || strings.IndexByte("[\"`*", lit[b+1]) >= 0)))
		if !solta {
			run = 0
			continue
		}
		// só espaço (sem vírgula, operador ou quebra de linha) desde a palavra anterior
		if run > 0 && !soEspacoEntrePalavras(lit[ini:a]) {
			run = 0
		}
		if run == 0 {
			ini = a
		}
		run++
		if run >= 3 {
			return ini
		}
	}
	return -1
}

// soEspacoEntrePalavras: o trecho tem só letras, dígitos, "_" e espaço/tab (as palavras da
// sequência e os espaços entre elas).
func soEspacoEntrePalavras(t string) bool {
	for _, r := range t {
		if !(r == ' ' || r == '\t' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// Instrução em minúsculas com uma cláusula só ("select * from tb_pedido", "delete from
// tb_log where ..."): vale quando a forma não deixa dúvida, para não pegar prosa ("select from
// the list", "update the readme"). A lista do SELECT é "*" ou nomes separados por vírgula (sem
// palavras soltas lado a lado), e depois do nome da tabela vem o fim, ";" ou outra cláusula;
// ou o nome da tabela tem cara de identificador.
var (
	reSelectMin = regexp.MustCompile(`(?is)^select\s+(?:distinct\s+|top\s+\d+\s+)?(\*|` + reQualSQL + `(?:\s*\([^)]{0,80}\))?(?:\s+as\s+\w+)?(?:\s*,\s*(?:\*|` + reQualSQL + `(?:\s*\([^)]{0,80}\))?(?:\s+as\s+\w+)?))*)\s+from\s+(` + reQualSQL + `)(.{0,12})`)
	reAlvoMin   = regexp.MustCompile(`(?is)^(?:insert\s+into|delete\s+from|update)\s+(` + reQualSQL + `)(.{0,12})`)
	// DDL, TRUNCATE, EXEC/CALL e USE fora de maiúsculas ("create table t_x (", "exec sp_x @a = 1")
	reDDLMin = regexp.MustCompile(`(?is)^(?:(?:create|alter|drop)\s+(?:or\s+(?:replace|alter)\s+)?(?:(?:global|local|temp|temporary|unique|external|materialized|transient|secure|clustered|nonclustered)\s+)*` +
		`(?:table|view|procedure|proc|function|schema|database|index|sequence)|truncate\s+table|exec|execute|call|use|describe|desc)\s+(?:if\s+(?:not\s+)?exists\s+)?(` + reQualSQL + `)(.{0,12})`)
	reDepoisDDL = regexp.MustCompile(`^\s*(?:\(|;|@|\n|$)|^\s+as\s+(?:select|\()`)
	reDepoisMin = regexp.MustCompile(`(?i)^(?:\s*(?:;|$)|\s+(?:where|join|inner|left|right|full|cross|limit|order|group|having|union|values|set|select|as\s+\w+\s+(?:where|join)|\(|[a-z]\w{0,2}\s+(?:where|join|on)\b)|\s*\n)`)
)

func formaInequivoca(corpo, kw string) bool {
	var alvo, depois string
	switch kw {
	case "SELECT":
		m := reSelectMin.FindStringSubmatch(corpo)
		if m == nil {
			return false
		}
		alvo, depois = m[2], m[3]
		if m[1] == "*" || strings.Contains(m[1], ",") {
			return reDepoisMin.MatchString(depois) || caraDeIdentificador(alvo)
		}
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "EXEC", "EXECUTE", "CALL", "USE", "DESCRIBE":
		// nome com cara de identificador, ou seguido do que só o SQL põe ali: "(", ";", "@"
		// parâmetro, fim da linha. Prosa ("drop table permissions") não tem nenhum dos dois.
		m := reDDLMin.FindStringSubmatch(corpo)
		if m == nil {
			return false
		}
		alvo := strings.Trim(m[1], "[]\"`")
		if kw == "USE" || kw == "DESCRIBE" || kw == "EXEC" || kw == "EXECUTE" || kw == "CALL" {
			return caraDeIdentificador(alvo) && reDepoisDDL.MatchString(m[2])
		}
		return caraDeIdentificador(alvo) || strings.HasPrefix(strings.TrimSpace(m[2]), "(") || strings.HasPrefix(strings.TrimSpace(m[2]), ";")
	case "INSERT", "DELETE", "UPDATE":
		m := reAlvoMin.FindStringSubmatch(corpo)
		if m == nil {
			return false
		}
		alvo, depois = m[1], m[2]
	default:
		return false
	}
	return caraDeIdentificador(alvo) || reDepoisMin.MatchString(depois)
}

// instrucaoSQL classifica os identificadores de uma instrução já reconhecida.
func instrucaoSQL(s string, base int, corpo, kw string, forte bool, add func(ObjAchado)) {
	lit := reLitSQL.ReplaceAllStringFunc(corpo, func(x string) string { return strings.Repeat(" ", len(x)) })
	ent := map[[2]int]string{} // parte -> entidade
	objetos := map[string]bool{}
	marcarQual := func(a, b int, ult string) {
		var vs [][2]int
		for _, p := range partesSQL(lit, a, b) {
			if v := strings.Trim(lit[p[0]:p[1]], "[]\"`"); len(v) > 1 && !publicoSQL(v) {
				vs = append(vs, p)
			}
		}
		if len(vs) == 0 {
			return
		}
		for k, e := range entQual(len(vs), ult) {
			ent[vs[k]] = e
		}
		objetos[strings.ToLower(strings.Trim(lit[vs[len(vs)-1][0]:vs[len(vs)-1][1]], "[]\"`"))] = true
	}
	up := strings.ToUpper(lit)
	for _, p := range rePosObjSQL {
		if p.tem != nil && !temAlguma(up, p.tem) {
			continue
		}
		for _, m := range p.re.FindAllStringSubmatchIndex(lit, -1) {
			marcarQual(m[2], m[3], p.ent)
			if p.ent == "tabela" { // FROM a, b, c
				for k := m[3]; ; {
					l := reListaSQL.FindStringSubmatchIndex(lit[k:])
					if l == nil {
						break
					}
					marcarQual(k+l[2], k+l[3], "tabela")
					k += l[3]
				}
			}
		}
	}
	if kw == "CREATE" || kw == "GRANT" || kw == "REVOKE" || kw == "ALTER" {
		for _, m := range reOnObjSQL.FindAllStringSubmatchIndex(lit, -1) {
			marcarQual(m[2], m[3], "tabela")
		}
	}
	for _, q := range qualsSQLEm(lit) {
		ps := partesSQL(lit, q[0], q[1])
		for k, p := range ps {
			a, b := p[0], p[1]
			if b <= a {
				continue
			}
			v := lit[a:b]
			e, temPos := ent[[2]int{a, b}]
			if !temPos {
				if publicoSQL(strings.Trim(v, "[]\"`")) || v[0] == '@' || (a > 0 && (lit[a-1] == ':' || lit[a-1] == '@' || lit[a-1] == '$')) {
					continue
				}
				if v[0] != '[' && v[0] != '"' && v[0] != '`' && len(ps) == 1 && b < len(lit) && lit[b] == '(' {
					continue // chamada de função
				}
				e = "coluna"
				if k < len(ps)-1 { // qualificador de uma coluna: tabela (ou alias curto, que fica)
					if len(v) <= 3 && !objetos[strings.ToLower(v)] {
						continue
					}
					e = "tabela"
				} else if len(ps) == 1 && len(v) <= 2 {
					continue // alias curto (p, t1)
				}
			}
			ia, ib := tirarCitacao(lit, a, b)
			if ib <= ia {
				continue
			}
			add(ObjAchado{base + ia, base + ib, e, "sql", forte && temPos && e != "coluna"})
		}
	}
}

// Mensagem de erro que cita um objeto: palavra do tipo seguida do nome entre aspas ou
// colchetes ("relation \"x\" does not exist", "Invalid object name 'x'", "Table 'db.x'
// doesn't exist", "Object 'A.B.C' does not exist"), ou "Table/Dataset projeto:dataset(.x)".
// Regra genérica: vale para qualquer banco, sem conhecer o texto de cada mensagem.
var (
	reErroBQ = regexp.MustCompile(`(?i)\b(table|dataset)\s+([A-Za-z][\w-]{2,62}):([A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?)\b`)
	entErro  = map[string]string{"table": "tabela", "relation": "tabela", "object": "tabela", "view": "tabela", "column": "coluna",
		"schema": "schema", "database": "database", "procedure": "procedure", "function": "procedure", "index": "indice",
		"sequence": "tabela", "collection": "tabela", "dataset": "schema"}
)

func acharErroObjeto(s string, add func(ObjAchado)) {
	letra := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	var buf [10]byte
	palavraAntes := func(k int) (int, string) { // a palavra que termina em s[k] (inclusive)
		a := k
		for a > 0 && letra(s[a-1]) && k-a < 10 {
			a--
		}
		if a > 0 && letra(s[a-1]) || k-a+1 > 10 {
			return a, ""
		}
		n := k - a + 1
		for x := 0; x < n; x++ {
			buf[x] = s[a+x] | 0x20
		}
		return a, string(buf[:n])
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c != '"' && c != '\'' && c != '`' && c != '[' {
			continue
		}
		k := i - 1
		for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
			k--
		}
		if k < 2 || k == i-1 || !letra(s[k]) { // precisa de espaço entre a palavra e a aspa
			continue
		}
		a, w := palavraAntes(k)
		if w == "name" { // "object name 'x'"
			k = a - 1
			for k >= 0 && s[k] == ' ' {
				k--
			}
			if k < 2 || !letra(s[k]) {
				continue
			}
			_, w = palavraAntes(k)
		}
		e, ok := entErro[w]
		if !ok {
			continue
		}
		fecha := c
		if c == '[' {
			fecha = ']'
		}
		b := i + 1
		for b < len(s) && b-i <= 200 && s[b] != fecha && s[b] != ' ' && s[b] != '\n' && s[b] != '"' && s[b] != '\'' && s[b] != '`' {
			b++
		}
		if b >= len(s) || s[b] != fecha || b == i+1 {
			continue
		}
		erroEm(s, i+1, b, e, add)
	}
	if strings.IndexByte(s, '.') >= 0 {
		acharTipoQualificado(s, add)
	}
	if strings.Contains(s, ":") && (strings.Contains(s, "able ") || strings.Contains(s, "ataset ") || strings.Contains(s, "ABLE ") || strings.Contains(s, "ATASET ")) {
		acharErroBQ(s, add)
	}
}

// palavras de tipo em português (sem acento: o texto é lido em ASCII) que também citam objeto
var entTipoPT = map[string]string{"tabela": "tabela", "objeto": "tabela", "esquema": "schema", "banco": "database",
	"coluna": "coluna", "indice": "indice", "colecao": "tabela", "visao": "tabela"}

// acharTipoQualificado: nome qualificado SEM aspas logo depois de palavra de tipo, em qualquer
// frase ("tabela fin.t_x: 1200 linhas", "Loading table a.b.c", "created sql table model
// fin.t_x"). Entre a palavra e o nome cabe uma palavra em minúsculas ("table model", "view
// model"). Sem aspas, só o nome qualificado vale: "table x" sozinho é prosa demais. Evidência
// forte com a palavra de tipo colada ao nome; com a palavra no meio, fraca.
func acharTipoQualificado(s string, add func(ObjAchado)) {
	letra := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	var buf [10]byte
	for i := 0; i < len(s); i++ {
		if !letra(s[i]) || i > 0 && (letra(s[i-1]) || s[i-1] == '_' || s[i-1] == '.' || s[i-1] >= '0' && s[i-1] <= '9') {
			continue
		}
		j := i
		for j < len(s) && letra(s[j]) && j-i < 10 {
			j++
		}
		if j-i < 4 || j < len(s) && (letra(s[j]) || s[j] != ' ' && s[j] != '\t') {
			i = j
			continue
		}
		n := j - i
		for x := 0; x < n; x++ {
			buf[x] = s[i+x] | 0x20
		}
		w := string(buf[:n])
		e, ok := entErro[w]
		if !ok {
			e, ok = entTipoPT[w]
		}
		if !ok {
			i = j - 1
			continue
		}
		p := j
		for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
			p++
		}
		meio := false
		if q := p; q < len(s) && s[q] >= 'a' && s[q] <= 'z' { // uma palavra no meio ("table model")
			for q < len(s) && s[q] >= 'a' && s[q] <= 'z' {
				q++
			}
			if q-p >= 2 && q-p <= 12 && q < len(s) && (s[q] == ' ' || s[q] == '\t') {
				r := q
				for r < len(s) && (s[r] == ' ' || s[r] == '\t') {
					r++
				}
				if a, b := qualificadoEm(s, r); b > a {
					meio = true
					p = r
				}
			}
		}
		a, b := qualificadoEm(s, p)
		if b <= a {
			i = j - 1
			continue
		}
		ps := strings.Split(s[a:b], ".")
		if receptoresCodigo[strings.ToLower(ps[0])] || publicoDev(ps[0]) {
			i = b
			continue
		}
		forte := !meio && e != "coluna"
		ents := entQual(len(ps), e)
		for k, v := range ps {
			if !publicoSQL(v) {
				add(ObjAchado{a, a + len(v), ents[k], "tipo-qualificado", forte && len(v) >= 3})
			}
			a += len(v) + 1
		}
		i = b
	}
}

// qualificadoEm: o nome qualificado (2 ou 3 partes, sem aspas) que começa em s[p], ou a == b.
// Não vale arquivo (fin.csv), domínio público, chamada (a.b(), caminho (a.b/c) nem versão.
func qualificadoEm(s string, p int) (int, int) {
	ident := func(c byte) bool { return ehAlnum(c) || c == '_' || c == '$' }
	if p >= len(s) || !(letraD(s[p]) || s[p] == '_') {
		return p, p
	}
	q, partes := p, 0
	for {
		k := q
		for k < len(s) && ident(s[k]) {
			k++
		}
		if k == q {
			return p, p
		}
		partes++
		q = k
		if q+1 < len(s) && s[q] == '.' && (letraD(s[q+1]) || s[q+1] == '_') && partes < 4 {
			q++
			continue
		}
		break
	}
	if partes < 2 || partes > 3 || q < len(s) && (ident(s[q]) || s[q] == '(' || s[q] == '/' || s[q] == '-' || s[q] == '.' && q+1 < len(s) && ehAlnum(s[q+1])) {
		return p, p
	}
	ult := s[strings.LastIndexByte(s[p:q], '.')+p+1 : q]
	if strings.IndexByte(s[p:q], '.') < 2 || len(ult) < 3 { // "i.e.", "e.g."
		return p, p
	}
	if l := strings.ToLower(ult); extensoesArquivo[l] || tldsPublicos[l] {
		return p, p
	}
	return p, q
}

// palavrasErro: a linha tem cara de mensagem de erro (só então o nome citado ensina)
var palavrasErro = []string{"error", "erro", "exist", "not found", "invalid", "unknown", "not authorized", "denied",
	"failed", "falhou", "inválid", "não encontrad", "msg ", "ora-", "sqlstate", "exception"}

func linhaDeErro(s string, i int) bool {
	a, z := inicioLinhaJ(s, i), fimLinhaJ(s, i)
	l := strings.ToLower(s[max(a, i-300):min(z, i+300)])
	for _, p := range palavrasErro {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

func erroEm(s string, a, b int, e string, add func(ObjAchado)) {
	if publicoSQL(s[a:b]) {
		return
	}
	forte := e != "coluna" && linhaDeErro(s, a)
	ps := strings.Split(s[a:b], ".")
	ents := entQual(len(ps), e)
	for k, p := range ps {
		if p != "" && reIdentSimples.MatchString(p) {
			add(ObjAchado{a, a + len(p), ents[k], "erro", forte})
		}
		a += len(p) + 1
	}
}

func acharErroBQ(s string, add func(ObjAchado)) {
	for _, m := range reErroBQ.FindAllStringSubmatchIndex(s, -1) {
		add(ObjAchado{m[4], m[5], "database", "erro", true})
		a := m[6]
		ps := strings.Split(s[m[6]:m[7]], ".")
		ents := []string{"schema", "tabela"}
		for k, p := range ps {
			add(ObjAchado{a, a + len(p), ents[k], "erro", true})
			a += len(p) + 1
		}
	}
}

// reIdentSimples: um identificador (com letras acentuadas: SQL e os catálogos aceitam).
var reIdentSimples = regexp.MustCompile(`^[\p{L}_][\p{L}0-9_$#-]*$`)

// Varredura sem regex das peças de SQL (o leitor roda em cada instrução; numa linha longa com
// milhares de instruções curtas, as regex sem caixa custavam a maior parte do tempo). Fazem o
// mesmo que reParteSQL, reQualTok e reClausulasSQL.

// parteSQLEm: o fim do identificador de SQL (reIdSQL) que começa em s[i], ou -1.
func parteSQLEm(s string, i int) int {
	switch c := s[i]; c {
	case '[', '"', '`':
		f := c
		if c == '[' {
			f = ']'
		}
		for k, n := i+1, 0; k < len(s) && n <= 128; n++ { // até 128 caracteres (não bytes)
			if s[k] == '\n' {
				return -1
			}
			if s[k] == f {
				if k == i+1 {
					return -1
				}
				return k + 1
			}
			_, sz := utf8.DecodeRuneInString(s[k:])
			k += sz
		}
		return -1
	}
	k := i
	if c := s[i]; c < 0x80 {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
			return -1
		}
		k++
	} else if r, n := utf8.DecodeRuneInString(s[i:]); unicode.IsLetter(r) {
		k += n
	} else {
		return -1
	}
	for k < len(s) {
		if c := s[k]; c < 0x80 {
			if ehAlnum(c) || c == '_' || c == '$' || c == '#' {
				k++
				continue
			}
			break
		}
		r, n := utf8.DecodeRuneInString(s[k:])
		if !unicode.IsLetter(r) {
			break
		}
		k += n
	}
	return k
}

// partesSQLEm: as posições de cada identificador de s, como reParteSQL.FindAllStringIndex.
func partesSQLEm(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		if f := parteSQLEm(s, i); f > i {
			out = append(out, [2]int{i, f})
			i = f
			continue
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		i += n
	}
	return out
}

// qualsSQLEm: os nomes qualificados (a.b.c) de s, como reQualTok.FindAllStringIndex.
func qualsSQLEm(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		f := parteSQLEm(s, i)
		if f <= i {
			_, n := utf8.DecodeRuneInString(s[i:])
			i += n
			continue
		}
		for f+1 < len(s) && s[f] == '.' {
			g := parteSQLEm(s, f+1)
			if g <= f+1 {
				break
			}
			f = g
		}
		out = append(out, [2]int{i, f})
		i = f
	}
	return out
}

var clausulasSQL = conj("FROM", "WHERE", "JOIN", "SET", "VALUES", "INTO", "HAVING", "UNION", "AS", "ON", "TABLE")

// contarClausulas: quantas cláusulas (reClausulasSQL) há em s, até max.
func contarClausulas(s string, max int) int {
	n := 0
	palavra := func(c byte) bool { return ehAlnum(c) || c == '_' }
	var buf [6]byte
	for i := 0; i < len(s) && n < max; {
		if !palavra(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && palavra(s[j]) {
			j++
		}
		if l := j - i; l >= 2 && l <= 6 {
			for k := 0; k < l; k++ {
				buf[k] = s[i+k] &^ 0x20
			}
			w := string(buf[:l])
			if clausulasSQL[w] {
				n++
			} else if w == "GROUP" || w == "ORDER" {
				k := j
				for k < len(s) && (s[k] == ' ' || s[k] == '\t' || s[k] == '\n' || s[k] == '\r' || s[k] == '\f' || s[k] == '\v') {
					k++
				}
				if k > j && k+2 <= len(s) && s[k]&^0x20 == 'B' && s[k+1]&^0x20 == 'Y' && (k+2 == len(s) || !palavra(s[k+2])) {
					n++
					j = k + 2
				}
			}
		}
		i = j
	}
	return n
}
