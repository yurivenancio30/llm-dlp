package mask

import (
	"regexp"
	"strings"
)

// Leitor de esquema (ver docs/pt-BR/estruturas.md, seção Esquemas): onde um nome vem com um tipo de
// dado ao lado, o nome é coluna. Nenhum leitor existente lê essa forma (não é chave-valor de
// configuração, não é SQL, não é tabela): "nome    int64" por linha (dtypes), Index([...])
// de colunas, " |-- nome: string" (printSchema), "nome: int64" por linha no começo da linha
// (schema do Arrow), "string nome = 1;" (protobuf). O vocabulário é só o de tipos de dado
// (vocab_tipos.go). "nome    tipo" é decidido pelo TIPO, com ou sem recuo: o bloco é esquema
// quando pelo menos um tipo é só de dado (object, category, datetime64[ns], VARCHAR(n),
// TIMESTAMP...), ou quando vem o rodapé "dtype: object" do pandas. Tipo que também é tipo de
// linguagem (string, int, bool, uint64, float64 — medido no código Go público,
// tipos_linguagem.txt) não decide sozinho: um struct Go escreve "nome    tipo" igual. Tipo com
// forma de código (*T, []T, pacote.Tipo, map[K]V) nunca é de esquema. As formas aninhadas em YAML/JSON (colunas de modelo, "fields" do Avro) ficam
// com o motor de YAML/JSON (acharNomePorContexto).

var (
	reEsqEspaco   = regexp.MustCompile(`^[ \t]*([\p{L}_][\p{L}\w$#.\-]*)[ \t]{2,}(\S+)[ \t]*$`)
	reEsqPrint    = regexp.MustCompile(`^[ |]*\|-- ([\p{L}_][\p{L}\w$#.\-]*): (\S+)`)
	reEsqDoisPont = regexp.MustCompile(`^([\p{L}_][\p{L}\w$#.\-]*): (\S+(?: not null)?)[ \t]*$`)
	reEsqProto    = regexp.MustCompile(`^[ \t]*(?:(?:repeated|optional|required)[ \t]+)?([A-Za-z_][\w.]*)[ \t]+([A-Za-z_]\w*)[ \t]*=[ \t]*\d+[ \t]*(?:\[[^\]]*\])?[ \t]*;`)
	reIndexCols   = regexp.MustCompile(`Index\(\[((?:\s*'[^'\n]*',?)+)\s*\],\s*dtype='object'`)
)

func acharEsquema(s string, add func(ObjAchado)) {
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
				addColuna(s, i+1, i+1+j, add)
				i += j + 1
			}
		}
	}
	if !strings.Contains(s, "\n") {
		return
	}
	// por linha: um bloco de linhas consecutivas na mesma forma (pelo menos 2, ou 1 com o
	// rodapé "dtype: object" do pandas)
	type par struct{ a, b int }
	var bloco []par
	forma := 0
	dado := false // "nome    tipo": algum tipo do bloco é só de dado
	fechar := func(rodape bool) {
		if (len(bloco) >= 2 || rodape && len(bloco) == 1 || forma == 3 || forma == 4) && (forma != 1 || dado || rodape) {
			for _, p := range bloco {
				addColuna(s, p.a, p.b, add)
			}
		}
		bloco, forma, dado = bloco[:0], 0, false
	}
	for _, l := range quebraLinhas(s) {
		linha := s[l[0]:l[1]]
		if strings.HasPrefix(linha, "dtype: ") { // rodapé do pandas: fecha o bloco de cima
			fechar(true)
			continue
		}
		f, a, b := 0, -1, -1
		tl := false
		if m := reEsqEspaco.FindStringSubmatchIndex(linha); m != nil && ehTipoDado(linha[m[4]:m[5]]) && !formaDeTipoDeCodigo(linha[m[4]:m[5]]) {
			f, a, b = 1, m[2], m[3]
			tl = !tipoSoDeDado(linha[m[4]:m[5]])
		} else if m := reEsqDoisPont.FindStringSubmatchIndex(linha); m != nil && ehTipoDado(strings.TrimSuffix(linha[m[4]:m[5]], " not null")) {
			f, a, b = 2, m[2], m[3]
		} else if m := reEsqPrint.FindStringSubmatchIndex(linha); m != nil && ehTipoDado(linha[m[4]:m[5]]) {
			f, a, b = 3, m[2], m[3]
		} else if m := reEsqProto.FindStringSubmatchIndex(linha); m != nil && ehTipoDado(linha[m[2]:m[3]]) {
			f, a, b = 4, m[4], m[5]
		}
		if f == 0 {
			fechar(false)
			continue
		}
		if forma != 0 && f != forma {
			fechar(false)
		}
		forma = f
		dado = dado || f == 1 && !tl
		bloco = append(bloco, par{l[0] + a, l[0] + b})
	}
	fechar(false)
}

// formaDeTipoDeCodigo: o tipo tem forma que só o código escreve: ponteiro (*T), lista ([]T),
// tipo qualificado por pacote (strings.Builder), mapa (map[K]V), canal.
func formaDeTipoDeCodigo(t string) bool {
	return t != "" && (t[0] == '*' || t[0] == '[' || strings.Contains(t, ".") || strings.HasPrefix(t, "map[") || strings.HasPrefix(t, "chan"))
}

// tipoSoDeDado: o tipo de dado não é também tipo de linguagem (tiposLinguagem, medido). Tipo
// em maiúsculas (INT, TIMESTAMP) é SQL: linguagem nenhuma escreve tipo assim.
func tipoSoDeDado(t string) bool {
	if strings.ToUpper(t) == t && strings.ToLower(t) != t {
		return true
	}
	if i := strings.IndexAny(t, "([<"); i > 0 {
		t = t[:i]
	}
	return !tiposLinguagem[strings.ToLower(t)]
}

// addColuna: nome de coluna (não é propagado; vale no lugar).
func addColuna(s string, a, b int, add func(ObjAchado)) {
	v := s[a:b]
	if v == "" || ehTipoDado(v) || publicoSQL(v) || rotulosSaida[strings.ToLower(v)] || atributoChave[strings.ToLower(v)] || !reIdentSimples.MatchString(strings.ReplaceAll(v, ".", "_")) {
		return
	}
	add(ObjAchado{a, b, "coluna", "esquema", true})
}

// ---------------------------------------------------------------------------------------
// "name" pelo contexto, em YAML e JSON: o "name" de um item de lista ou de um objeto vale pelo
// nome do contêiner ("columns: - name: x" é coluna; "tables:", "models:" é tabela; "fields" do
// Avro é coluna; {"table": {"name": "x"}} é tabela; "owner" é usuário). O contêiner diz o tipo
// pela palavra de tipo (entChave), no singular ou no plural.

// contêineres de nomes que não são palavra de tipo de recurso, mas dizem o que guardam
var conteinerNome = map[string]string{"columns": "coluna", "column": "coluna", "fields": "coluna", "field": "coluna",
	"colunas": "coluna", "coluna": "coluna", "campos": "coluna", "models": "tabela", "model": "tabela",
	"sources": "schema", "seeds": "tabela", "snapshots": "tabela", "views": "tabela", "view": "tabela",
	"datasets": "schema", "tabelas": "tabela"}

func entConteiner(k string) string {
	if e, ok := conteinerNome[strings.ToLower(k)]; ok {
		return e
	}
	if e, forte := entChave(k); forte {
		return e
	}
	return ""
}

func acharNomePorContexto(s string, add func(ObjAchado)) {
	if !strings.Contains(s, "name") {
		return
	}
	if !temAlgum(s, []string{"columns", "fields", "tables", "models", "sources", "table", "dataset", "schema", "database", "_name",
		"seeds", "views", "owner", "colunas", "tabelas"}) {
		return
	}
	w := &yColetor{}
	w.lerYAML(s)
	if strings.Contains(s, `"name"`) {
		w.lerJSON(s)
	}
	irmaos := map[int]map[string]bool{} // contêiner -> chaves
	for i := range w.ents {
		e := &w.ents[i]
		if irmaos[e.pai()] == nil {
			irmaos[e.pai()] = map[string]bool{}
		}
		irmaos[e.pai()][strings.ToLower(e.chave())] = true
	}
	visto := map[[2]int]bool{}
	for i := range w.ents {
		e := &w.ents[i]
		if e.vi < 0 || e.tmpl || len(e.path) < 1 {
			continue
		}
		k := e.chave()
		ent := ""
		switch strings.ToLower(k) {
		case "name", "nome":
			if len(e.path) >= 2 {
				c := e.path[len(e.path)-2]
				if c == "-" && len(e.path) >= 3 {
					c = e.path[len(e.path)-3]
				}
				ent = entConteiner(c)
			}
			// saída de listagem (SHOW em JSON): o nome ao lado do schema é tabela; ao lado só do
			// banco, é schema
			if ir := irmaos[e.pai()]; ir["schema_name"] || ir["table_schema"] {
				ent = "tabela"
			} else if ent == "" && (ir["database_name"] || ir["table_catalog"]) {
				ent = "schema"
			}
		case "owner":
			ent = "usuario"
		}
		if ent == "" || visto[[2]int{e.vi, e.vf}] {
			continue
		}
		visto[[2]int{e.vi, e.vf}] = true
		if v := s[e.vi:e.vf]; ent == "coluna" {
			addColuna(s, e.vi, e.vf, add)
		} else if strings.Count(v, ".") > 0 && strings.Count(v, ".") <= 3 && reCelulaIdent.MatchString(v) {
			addPartesCelula(s, e.vi, e.vf, ent, add) // "db.schema.tabela": nome qualificado
		} else {
			addNome(s, e.vi, e.vf, ent, "nome-contexto", true, add)
		}
	}
}
