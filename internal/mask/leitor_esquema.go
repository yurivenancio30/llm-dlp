package mask

import (
	"regexp"
	"strings"
)

// Reader de esquema (ver docs/pt-BR/estruturas.md, seção Esquemas): onde um nome vem com um tipo de
// dado ao lado, o nome é coluna. Nenhum leitor existente lê essa forma (não é chave-valor de
// configuração, não é SQL, não é tabela): "nome    int64" por linha (dtypes), Index([...])
// de colunas, " |-- nome: string" (printSchema), "nome: int64" por linha no começo da linha
// (schema do Arrow), "string nome = 1;" (protobuf). O vocabulário é só o de tipos de dado
// (vocab_tipos.go). "nome    tipo" é decidido pelo TIPO, com ou sem recuo: o bloco é esquema
// quando pelo menos um tipo é só de dado (object, category, datetime64[ns], VARCHAR(n),
// TIMESTAMP...), ou quando vem o rodapé "dtype: object" do pandas. Type que também é tipo de
// linguagem (string, int, bool, uint64, float64 — medido no código Go público,
// tipos_linguagem.txt) não decide sozinho: um struct Go escreve "nome    tipo" igual. Type com
// forma de código (*T, []T, pacote.Type, map[K]V) nunca é de esquema. As formas aninhadas em YAML/JSON (colunas de modelo, "fields" do Avro) ficam
// com o motor de YAML/JSON (findNameByContext).

var (
	reSchemeSpace = regexp.MustCompile(`^[ \t]*([\p{L}_][\p{L}\w$#.\-]*)[ \t]{2,}(\S+)[ \t]*$`)
	reSchemePrint = regexp.MustCompile(`^[ |]*\|-- ([\p{L}_][\p{L}\w$#.\-]*): (\S+)`)
	reSchemeColon = regexp.MustCompile(`^([\p{L}_][\p{L}\w$#.\-]*): (\S+(?: not null)?)[ \t]*$`)
	reSchemeProto = regexp.MustCompile(`^[ \t]*(?:(?:repeated|optional|required)[ \t]+)?([A-Za-z_][\w.]*)[ \t]+([A-Za-z_]\w*)[ \t]*=[ \t]*\d+[ \t]*(?:\[[^\]]*\])?[ \t]*;`)
	reIndexCols   = regexp.MustCompile(`Index\(\[((?:\s*'[^'\n]*',?)+)\s*\],\s*dtype='object'`)
)

func findSchema(s string, add func(ObjFinding)) {
	if strings.Contains(s, "Index([") {
		for _, m := range reIndexCols.FindAllStringSubmatchIndex(s, -1) {
			for i := m[2]; i < m[3]; i++ {
				if s[i] != '\'' {
					continue
				}
				j := strings.IndexByte(s[i+1:m[3]], '\'')
				if j < 0 {
					break
				}
				addColumn(s, i+1, i+1+j, add)
				i += j + 1
			}
		}
	}
	if !strings.Contains(s, "\n") {
		return
	}
	// por linha: um bloco de linhas consecutivas na mesma forma (pelo menos 2, ou 1 com o
	// rodapé "dtype: object" do pandas)
	type pair struct{ a, b int }
	var block []pair
	shape := 0
	datum := false // "nome    tipo": algum tipo do bloco é só de dado
	closeIt := func(footer bool) {
		if (len(block) >= 2 || footer && len(block) == 1 || shape == 3 || shape == 4) && (shape != 1 || datum || footer) {
			for _, p := range block {
				addColumn(s, p.a, p.b, add)
			}
		}
		block, shape, datum = block[:0], 0, false
	}
	for _, l := range wrapLines(s) {
		line := s[l[0]:l[1]]
		if strings.HasPrefix(line, "dtype: ") { // rodapé do pandas: fecha o bloco de cima
			closeIt(true)
			continue
		}
		f, a, b := 0, -1, -1
		tl := false
		if m := reSchemeSpace.FindStringSubmatchIndex(line); m != nil && isDataType(line[m[4]:m[5]]) && !codeTypeShape(line[m[4]:m[5]]) {
			f, a, b = 1, m[2], m[3]
			tl = !dataOnlyType(line[m[4]:m[5]])
		} else if m := reSchemeColon.FindStringSubmatchIndex(line); m != nil && isDataType(strings.TrimSuffix(line[m[4]:m[5]], " not null")) {
			f, a, b = 2, m[2], m[3]
		} else if m := reSchemePrint.FindStringSubmatchIndex(line); m != nil && isDataType(line[m[4]:m[5]]) {
			f, a, b = 3, m[2], m[3]
		} else if m := reSchemeProto.FindStringSubmatchIndex(line); m != nil && isDataType(line[m[2]:m[3]]) {
			f, a, b = 4, m[4], m[5]
		}
		if f == 0 {
			closeIt(false)
			continue
		}
		if shape != 0 && f != shape {
			closeIt(false)
		}
		shape = f
		datum = datum || f == 1 && !tl
		block = append(block, pair{l[0] + a, l[0] + b})
	}
	closeIt(false)
}

// codeTypeShape: o tipo tem forma que só o código escreve: ponteiro (*T), lista ([]T),
// tipo qualificado por pacote (strings.Builder), mapa (map[K]V), canal.
func codeTypeShape(t string) bool {
	return t != "" && (t[0] == '*' || t[0] == '[' || strings.Contains(t, ".") || strings.HasPrefix(t, "map[") || strings.HasPrefix(t, "chan"))
}

// dataOnlyType: o tipo de dado não é também tipo de linguagem (languageTypes, medido). Type
// em maiúsculas (INT, TIMESTAMP) é SQL: linguagem nenhuma escreve tipo assim.
func dataOnlyType(t string) bool {
	if strings.ToUpper(t) == t && strings.ToLower(t) != t {
		return true
	}
	if i := strings.IndexAny(t, "([<"); i > 0 {
		t = t[:i]
	}
	return !languageTypes[strings.ToLower(t)]
}

// addColumn: nome de coluna (não é propagado; vale no lugar).
func addColumn(s string, a, b int, add func(ObjFinding)) {
	v := s[a:b]
	if v == "" || isDataType(v) || publicSQL(v) || outputLabels[strings.ToLower(v)] || keyAttribute[strings.ToLower(v)] || !reSimpleIdent.MatchString(strings.ReplaceAll(v, ".", "_")) {
		return
	}
	add(ObjFinding{a, b, "coluna", "esquema", true})
}

// ---------------------------------------------------------------------------------------
// "name" pelo contexto, em YAML e JSON: o "name" de um item de lista ou de um objeto vale pelo
// nome do contêiner ("columns: - name: x" é coluna; "tables:", "models:" é tabela; "fields" do
// Avro é coluna; {"table": {"name": "x"}} é tabela; "owner" é usuário). O contêiner diz o tipo
// pela palavra de tipo (entKey), no singular ou no plural.

// contêineres de nomes que não são palavra de tipo de recurso, mas dizem o que guardam
var containerName = map[string]string{"columns": "coluna", "column": "coluna", "fields": "coluna", "field": "coluna",
	"colunas": "coluna", "coluna": "coluna", "campos": "coluna", "models": "tabela", "model": "tabela",
	"sources": "schema", "seeds": "tabela", "snapshots": "tabela", "views": "tabela", "view": "tabela",
	"datasets": "schema", "tabelas": "tabela"}

func entContainer(k string) string {
	if e, ok := containerName[strings.ToLower(k)]; ok {
		return e
	}
	if e, strong := entKey(k); strong {
		return e
	}
	return ""
}

func findNameByContext(s string, add func(ObjFinding)) {
	if !strings.Contains(s, "name") {
		return
	}
	if !hasAny(s, []string{"columns", "fields", "tables", "models", "sources", "table", "dataset", "schema", "database", "_name",
		"seeds", "views", "owner", "colunas", "tabelas"}) {
		return
	}
	w := &yCollector{}
	w.readYAML(s)
	if strings.Contains(s, `"name"`) {
		w.readJSON(s)
	}
	siblings := map[int]map[string]bool{} // contêiner -> chaves
	for i := range w.ents {
		e := &w.ents[i]
		if siblings[e.parent()] == nil {
			siblings[e.parent()] = map[string]bool{}
		}
		siblings[e.parent()][strings.ToLower(e.key())] = true
	}
	seen := map[[2]int]bool{}
	for i := range w.ents {
		e := &w.ents[i]
		if e.vi < 0 || e.tmpl || len(e.path) < 1 {
			continue
		}
		k := e.key()
		ent := ""
		switch strings.ToLower(k) {
		case "name", "nome":
			if len(e.path) >= 2 {
				c := e.path[len(e.path)-2]
				if c == "-" && len(e.path) >= 3 {
					c = e.path[len(e.path)-3]
				}
				ent = entContainer(c)
			}
			// saída de listagem (SHOW em JSON): o nome ao lado do schema é tabela; ao lado só do
			// banco, é schema
			if ir := siblings[e.parent()]; ir["schema_name"] || ir["table_schema"] {
				ent = "tabela"
			} else if ent == "" && (ir["database_name"] || ir["table_catalog"]) {
				ent = "schema"
			}
		case "owner":
			ent = "usuario"
		}
		if ent == "" || seen[[2]int{e.vi, e.vf}] {
			continue
		}
		seen[[2]int{e.vi, e.vf}] = true
		if v := s[e.vi:e.vf]; ent == "coluna" {
			addColumn(s, e.vi, e.vf, add)
		} else if strings.Count(v, ".") > 0 && strings.Count(v, ".") <= 3 && reIdentCell.MatchString(v) {
			addCellParts(s, e.vi, e.vf, ent, add) // "db.schema.tabela": nome qualificado
		} else {
			addName(s, e.vi, e.vf, ent, "nome-contexto", true, add)
		}
	}
}
