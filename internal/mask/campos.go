package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detecção pelo nome do campo, parte 2: "campo: valor", JSON, SQL INSERT e XML.

var notValue = map[string]bool{"": true, "null": true, "none": true, "nil": true, "nan": true, "n/a": true, "na": true, "-": true, "--": true,
	"true": true, "false": true, "undefined": true, "string": true, "varchar": true, "text": true, "number": true, "integer": true, "date": true}

// contas de serviço e nomes genéricos: mascarar "user=root" só atrapalharia

var commonUser = map[string]bool{"root": true, "admin": true, "administrator": true, "postgres": true, "mysql": true, "sa": true, "app": true,
	"user": true, "usuario": true, "test": true, "teste": true, "ubuntu": true, "ec2-user": true, "airflow": true, "datahub": true, "system": true,
	"sys": true, "sysadmin": true, "guest": true, "nobody": true, "www-data": true, "default": true, "anonymous": true, "public": true, "git": true,
	"docker": true, "oracle": true, "hadoop": true, "spark": true, "hive": true, "kafka": true, "jenkins": true, "claude": true, "assistant": true}

var reReadyPseudo = regexp.MustCompile(`^(?:[A-Z:]+-[a-z2-7]{8}|[Pp]essoa [a-z2-7]{8}|PESSOA [A-Z2-7]{8}|\S+\.invalid)$`)

// marcar aplica fieldValue ao trecho s[ini:fim] e registra o achado. Num campo de nome,
// "Fulano de Tal / Beltrana Souza" são duas pessoas: cada parte é um achado.
func markIt(s string, ini, end int, class string, add func(ini, end int, typ string)) {
	if class == "nomesolto" {
		return // só vale em tabela ou registro com outro campo de pessoa, onde vira "nomecompleto" (ver cabecalho e nameInRegistry)
	}
	if (class == "nome" || class == "nomecompleto") && strings.ContainsAny(s[ini:end], "/;&") {
		for p := ini; p < end; {
			q := p
			for q < end && s[q] != '/' && s[q] != ';' && s[q] != '&' {
				q++
			}
			a, b := p, q
			for a < b && s[a] == ' ' {
				a++
			}
			for b > a && s[b-1] == ' ' {
				b--
			}
			if b > a {
				if typ, ok := fieldValue(class, s[a:b]); ok {
					add(a, b, typ)
				}
			}
			p = q + 1
		}
		return
	}
	if typ, ok := fieldValue(class, s[ini:end]); ok {
		add(ini, end, typ)
	}
}

// fieldValue decide se v, num campo da classe dada, deve ser mascarado, e com que tipo.
func fieldValue(class, v string) (string, bool) {
	if notValue[strings.ToLower(v)] || len(v) > 120 || reReadyPseudo.MatchString(v) || isPseudoObj(v) || isPseudoLabel(v) {
		return "", false
	}
	dig, let := 0, 0
	for _, c := range v {
		switch {
		case unicode.IsDigit(c):
			dig++
		case unicode.IsLetter(c):
			let++
		}
	}
	switch class {
	case "cartao", "conta":
		return class, dig >= 4 && let <= 4
	case "quase":
		return class, let+dig >= 1 && len(v) <= 60
	case "cpf", "cnpj", "rg", "cnh", "pis", "telefone", "cep", "doc":
		// uma data ou hora não é documento, por mais dígitos que tenha ("2026-10-03",
		// "12:30:00"): sem isto, uma data numa coluna dessas seria lembrada e mascarada em
		// todo lugar
		if looksLikeDateTime(v) {
			return "", false
		}
		if class == "doc" && dig >= 3 && dig+let >= 6 {
			return "doc", true
		}
		// quantidade de dígitos de cada documento (com folga para zeros à esquerda cortados)
		rng := map[string][2]int{"cpf": {9, 11}, "cnpj": {12, 14}, "rg": {5, 14}, "cnh": {9, 11}, "pis": {9, 11},
			"telefone": {8, 13}, "cep": {7, 8}, "doc": {5, 20}}[class]
		if class == "cnpj" && let > 0 { // CNPJ novo, com letras
			return class, dig+let == 14 && dig >= 2
		}
		return class, dig >= rng[0] && dig <= rng[1] && let <= 3
	case "nascimento":
		return class, dig >= 6 && dig <= 14 && let <= 12
	case "endereco":
		return class, let >= 4 && len(v) >= 6
	case "usuario":
		// não vale variável de ambiente, parâmetro de consulta (":user_id") nem trecho de código;
		// nem valor sem dono ("stub-user", "my_user", "XXXXXXXXX", ver vocab_dev.go)
		return class, dig+let >= 2 && !commonUser[strings.ToLower(v)] && !strings.ContainsAny(v, " ()[]{}$<>:%") &&
			!ownerless(v, nil)
	case "sensivel":
		return class, let+dig >= 1
	case "nomecompleto":
		// nome e sobrenome, cada um começando com maiúscula ("Maria Lopes", "MARIA LOPES")
		ps := strings.Fields(v)
		if len(ps) < 2 {
			return "", false
		}
		for _, p := range []string{ps[0], ps[len(ps)-1]} {
			if r := []rune(p)[0]; !unicode.IsUpper(r) {
				return "", false
			}
		}
		return fieldValue("nome", v)
	case "nome":
		// nome de gente: letras, espaços e pouco mais (não "SVC_CARGA", "a.b:c", "x@y")
		for _, c := range v {
			if !unicode.IsLetter(c) && c != ' ' && c != '\'' && c != '-' && c != '.' {
				return "", false
			}
		}
		return class, let >= 3
	}
	return "", false
}

// findFields procura "campo: valor", "campo=valor", "campo": "valor" (JSON) com campo
// sensível. Não usa regex: anda só pelos ":" e "=" do texto, olha a palavra que vem antes
// (o rótulo) e, se ela for de um campo sensível, pega o valor que vem depois. Em texto sem
// campo sensível o custo é uma consulta por separador.
func (m *Masker) findFields(s string, add func(ini, end int, typ string)) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !fieldSeparator(s, i) {
			continue
		}
		next := byte(0)
		if i+1 < len(s) {
			next = s[i+1]
		}
		class := m.classBefore(s, i)
		if class == "nomesolto" {
			class = m.nameInRegistry(s, i) // "nome" sozinho: de pessoa só com outro campo de pessoa ao lado
		}
		if class == "" {
			continue
		}
		// valor
		j := i + 1
		if c == '=' && next == '>' {
			j++
		}
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j >= len(s) {
			break
		}
		// prefixo de literal de string (r'...', b"...", f'...', rb'...', u'...'): não é o
		// valor, o valor é o que está entre as aspas
		j = afterLiteralPrefix(s, j)
		ini, end := j, j
		if q := s[j]; q == '"' || q == '\'' || q == '`' {
			ini = j + 1
			end = ini
			for end < len(s) && s[end] != q && s[end] != '\n' && end-ini < 120 {
				end++
			}
		} else {
			// a crase e o ">" fecham código e marcação em volta do valor ("`User ID=x`", "<b>x</b>")
			for end < len(s) && end-ini < 120 && !strings.ContainsRune("\n\"'`,;|{}[]<>", rune(s[end])) {
				end++
			}
			if c == '=' {
				// "a=1 b=2" (log, .env): o valor acaba no espaço. Um nome de pessoa pode ter
				// espaços; aí vai até a palavra que já é de outro "campo=valor".
				p := ini
				for p < end && s[p] != ' ' && s[p] != '\t' {
					p++
				}
				if strings.HasPrefix(class, "nome") {
					for q := p; q < end; {
						for q < end && (s[q] == ' ' || s[q] == '\t') {
							q++
						}
						r := q
						for r < end && s[r] != ' ' && s[r] != '\t' {
							r++
						}
						if r == q || strings.ContainsAny(s[q:r], "=:") {
							break
						}
						p, q = r, r
					}
				}
				end = p
			}
		}
		end = trimValue(s, ini, end)
		if end > ini && (class == "quase" || !shortLettersOnly(s[ini:end])) && !defaultOrModel(s[ini:end]) {
			markIt(s, ini, end, class, add)
		}
	}
}

// fieldSeparator: s[i] é o ":" ou "=" de um "campo: valor" ou "campo=valor". Não são: "::",
// "==", "!=", "<=", ">=", ":=" (vale o "="), "://".
func fieldSeparator(s string, i int) bool {
	c := s[i]
	if c != ':' && c != '=' {
		return false
	}
	next, prev := byte(0), byte(0)
	if i+1 < len(s) {
		next = s[i+1]
	}
	if i > 0 {
		prev = s[i-1]
	}
	return !(c == ':' && (next == ':' || next == '=' || next == '/' || prev == ':')) &&
		!(c == '=' && (next == '=' || prev == '=' || prev == '!' || prev == '<' || prev == '>'))
}

// nameInRegistry: "nome" ou "name" sozinho é nome de pessoa, como numa tabela (ver
// cabecalho), quando o MESMO registro tem outro campo sensível ({"nome": x, "cpf": y},
// <nome>x</nome><cpf>y</cpf>, nome=x cpf=y). O nome de um produto ou de um recurso fica.
// O registro termina em s[i] (o separador ou o "</" do campo): o "{...}" em volta; sem
// chaves, as linhas vizinhas com o mesmo recuo (XML, YAML, .env) ou a própria linha (log).
func (m *Masker) nameInRegistry(s string, i int) string {
	a, b := registryIn(s, i)
	for k := a; k < b; k++ {
		var c string
		switch {
		case k == i:
			continue
		case s[k] == ':' || s[k] == '=':
			if fieldSeparator(s, k) {
				c = m.classBefore(s, k)
			}
		case s[k] == '<' && k+1 < b && s[k+1] == '/':
			if f := strings.IndexByte(s[k+2:b], '>'); f > 0 && f <= 60 {
				c = m.class(s[k+2 : k+2+f])
			}
		}
		if c != "" && c != "nomesolto" {
			return "nomecompleto"
		}
	}
	return ""
}

// registryIn: o registro em volta de s[i] (ver nameInRegistry), com teto de tamanho.
func registryIn(s string, i int) (int, int) {
	const ceiling = 8 << 10
	depth := 0
	for a := i - 1; a >= 0 && i-a <= ceiling; a-- {
		switch s[a] {
		case '}':
			depth++
		case '{':
			if depth > 0 {
				depth--
				continue
			}
			p := 0
			for b := a + 1; b < len(s) && b-a <= 2*ceiling; b++ {
				switch s[b] {
				case '{':
					p++
				case '}':
					if p == 0 {
						return a, b + 1
					}
					p--
				}
			}
			return a, min(len(s), a+2*ceiling)
		case '\n':
			if depth == 0 && a > 0 && s[a-1] == '\n' {
				a = -1 // linha em branco antes de qualquer "{": não está entre chaves
			}
		}
		if a < 0 {
			break
		}
	}
	// sem chaves: as linhas vizinhas não vazias com o mesmo recuo
	ini := strings.LastIndexByte(s[:i], '\n') + 1
	indent := func(l int) int {
		n := 0
		for l+n < len(s) && (s[l+n] == ' ' || s[l+n] == '\t') {
			n++
		}
		return n
	}
	r := indent(ini)
	a := ini
	for n := 0; n < 60 && a > 0; n++ {
		p := strings.LastIndexByte(s[:a-1], '\n') + 1
		if p == a-1 || indent(p) != r || p+r >= len(s) || s[p+r] == '\n' {
			break
		}
		a = p
	}
	b := lineEndIn(s, i)
	for n := 0; n < 60 && b < len(s); n++ {
		p := b + 1
		if p >= len(s) || s[p] == '\n' || indent(p) != r || p+r >= len(s) || s[p+r] == '\n' {
			break
		}
		b = lineEndIn(s, p)
	}
	return a, b
}

func lineEndIn(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(s)
}

// afterLiteralPrefix: se s[j:] começa com um prefixo de literal de string do Python (r, b,
// f, u, rb, br, fr, rf, em qualquer caixa) colado numa aspa, devolve a posição da aspa.
func afterLiteralPrefix(s string, j int) int {
	for n := 1; n <= 2 && j+n < len(s); n++ {
		if q := s[j+n]; q != '"' && q != '\'' {
			continue
		}
		switch strings.ToLower(s[j : j+n]) {
		case "r", "b", "f", "u", "rb", "br", "fr", "rf":
			if j == 0 || !isIdent(s[j-1]) {
				return j + n
			}
		}
		return j
	}
	return j
}

// shortLettersOnly: 1 ou 2 letras e nada mais ("r", "pt", "M"): no "campo: valor" isso é sigla,
// prefixo ou pedaço de código, não o dado. Os quase identificadores (sexo "F", UF "MG") são a
// exceção: lá o valor é curto mesmo (e o detector é opcional).
func shortLettersOnly(v string) bool {
	if len(v) > 2 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if !(v[i] >= 'a' && v[i] <= 'z' || v[i] >= 'A' && v[i] <= 'Z') {
			return false
		}
	}
	return true
}

// defaultOrModel: o valor é um padrão de regex ("(?!x)\\b...", "\\d+") ou um modelo de texto
// ("{valor}", "%s", "%(nome)s"): é código, não o dado do campo.
func defaultOrModel(v string) bool {
	if strings.Contains(v, "(?") || len(v) >= 2 && v[0] == '{' && v[len(v)-1] == '}' || v == "%s" || strings.HasPrefix(v, "%(") {
		return true
	}
	for i := 0; i+1 < len(v); i++ {
		if v[i] == '\\' && strings.IndexByte("dDwWsSbB", v[i+1]) >= 0 {
			return true
		}
	}
	return false
}

// trimValue tira do fim do valor o que é da frase ou da marcação em volta, e não do valor:
// espaço, ponto final, e ")", "]", "}" ou "*" sem a abertura correspondente dentro do valor
// ("(User ID=x)", "**User ID=x**"). Sem isto, o fechamento ia junto para dentro do
// pseudônimo: o modelo não o via, e um valor com ")" era descartado inteiro (e saía em claro).
func trimValue(s string, ini, end int) int {
	for end > ini {
		v := s[ini:end]
		switch s[end-1] {
		case ' ', '\t', '\r', '.', '*':
		case ')':
			if strings.Count(v, "(") >= strings.Count(v, ")") {
				return end
			}
		case ']':
			if strings.Count(v, "[") >= strings.Count(v, "]") {
				return end
			}
		case '}':
			if strings.Count(v, "{") >= strings.Count(v, "}") {
				return end
			}
		default:
			return end
		}
		end--
	}
	return end
}

// classBefore classifica o rótulo que termina logo antes de s[i] (o separador). Tenta com a
// última palavra, depois com as duas últimas, depois com as três ("cpf", "cliente cpf",
// "nome da mae"), e fica com a primeira que for um campo sensível.
func (m *Masker) classBefore(s string, i int) string {
	end := i
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	if end > 0 && (s[end-1] == '"' || s[end-1] == '\'' || s[end-1] == '`') {
		end--
	}
	letter := func(b byte) bool { return isAlnum(b) || b == '_' || b == '.' || b == '-' || b >= 0x80 }
	ini := end
	for words := 0; words < 3; words++ {
		p := ini
		if words > 0 {
			if p == 0 || s[p-1] != ' ' {
				break
			}
			p--
		}
		q := p
		for q > 0 && letter(s[q-1]) && end-q < 60 {
			q--
		}
		if q == p {
			break
		}
		ini = q
		if c := m.class(s[ini:end]); c != "" {
			return c
		}
	}
	return ""
}

// findInsert: INSERT INTO t (col1, col2) VALUES (v1, v2), (...). As colunas dão o rótulo de
// cada valor.
func (m *Masker) findInsert(s, lower string, add func(ini, end int, typ string)) {
	if len(s) != len(lower) {
		return
	}
	for pos := 0; ; {
		i := strings.Index(lower[pos:], "insert into")
		if i < 0 {
			return
		}
		pos += i + 11
		a := strings.IndexByte(s[pos:], '(')
		v := strings.Index(lower[pos:], "values")
		if a < 0 || v < 0 || a > v || v > 4000 {
			continue
		}
		b := strings.IndexByte(s[pos+a:], ')')
		if b < 0 || pos+a+b > pos+v {
			continue
		}
		var classes []string
		sens := 0
		for _, col := range strings.Split(s[pos+a+1:pos+a+b], ",") {
			c := m.class(strings.Trim(col, " \t\n\r`\"[]"))
			if c == "nomesolto" {
				c = "nomecompleto"
			}
			if c != "" {
				sens++
			}
			classes = append(classes, c)
		}
		p := pos + v + 6
		if sens == 0 {
			pos = p
			continue
		}
		for { // cada "(v1, v2, ...)"
			p = skipBlank(s, p, true)
			if p >= len(s) || s[p] != '(' {
				break
			}
			p++
			for col := 0; p < len(s); col++ {
				p = skipBlank(s, p, false)
				ini, end := p, p
				if p < len(s) && s[p] == '\'' {
					ini = p + 1
					end = ini
					for end < len(s) && !(s[end] == '\'' && (end+1 >= len(s) || s[end+1] != '\'')) {
						if s[end] == '\'' {
							end++ // aspa dobrada dentro do texto
						}
						end++
					}
					p = end + 1
				} else {
					for end < len(s) && s[end] != ',' && s[end] != ')' {
						end++
					}
					p = end
				}
				if col < len(classes) && classes[col] != "" && end > ini && end <= len(s) {
					markIt(s, ini, end, classes[col], add)
				}
				p = skipBlank(s, p, false)
				if p >= len(s) || s[p] != ',' {
					break
				}
				p++
			}
			if p < len(s) && s[p] == ')' {
				p++
			}
		}
		pos = min(p, len(s))
	}
}

// findXML: <campo>valor</campo>, com campo sensível.
func (m *Masker) findXML(s string, add func(ini, end int, typ string)) {
	for pos := 0; ; {
		i := strings.Index(s[pos:], "</")
		if i < 0 {
			return
		}
		closes := pos + i
		pos = closes + 2
		f := strings.IndexByte(s[pos:], '>')
		if f < 1 || f > 60 {
			continue
		}
		label := s[pos : pos+f]
		class := m.class(label)
		if class == "nomesolto" {
			class = m.nameInRegistry(s, closes)
		}
		if class == "" {
			continue
		}
		// o valor é o que está entre o ">" da abertura e o "</"
		a0 := max(0, closes-122)
		ab := strings.LastIndexByte(s[a0:closes], '>')
		if ab >= 0 {
			ab += a0
		}
		if ab < 0 || closes-ab-1 > 120 || closes-ab-1 < 1 || strings.ContainsAny(s[ab+1:closes], "<\n") {
			continue
		}
		markIt(s, ab+1, closes, class, add)
	}
}

// Column descreve como um nome de campo foi entendido (para o comando "llm-dlp columns").
type Column struct {
	Name, Class string
}

// DescribeFields acha os nomes de campo de um texto (o cabeçalho de uma tabela, ou as
// chaves de "campo: valor"/JSON) e diz como cada um foi classificado.
func (m *Masker) DescribeFields(s string) []Column {
	var out []Column
	seen := map[string]bool{}
	by := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || len(name) > 60 {
			return
		}
		seen[name] = true
		out = append(out, Column{name, m.vocab.labelClass(name)})
	}
	// 1) primeira linha com cara de cabeçalho de tabela
	for _, line := range strings.SplitN(s, "\n", 30) {
		if isMarkdownSeparator(line) || strings.ContainsAny(line, "={}()") {
			continue
		}
		best, n := byte(0), 0
		for _, sep := range []byte{'\t', '|', ';', ','} {
			if c := strings.Count(line, string(sep)); c > n {
				best, n = sep, c
			}
		}
		if n == 0 {
			continue
		}
		cells := cellsOf(line, best)
		ok := len(cells) >= 2
		for _, c := range cells {
			a, b := trim(line, c[0], c[1])
			if b-a > 48 || strings.Contains(line[a:b], ":") {
				ok = false
			}
		}
		if ok {
			for _, c := range cells {
				a, b := trim(line, c[0], c[1])
				by(line[a:b])
			}
			return out
		}
	}
	// 2) chaves de "campo: valor" / JSON
	for i := 0; i < len(s) && len(out) < 400; i++ {
		if s[i] != ':' && s[i] != '=' {
			continue
		}
		end := i
		for end > 0 && (s[end-1] == ' ' || s[end-1] == '"' || s[end-1] == '\'') {
			end--
		}
		ini := end
		for ini > 0 && (isAlnum(s[ini-1]) || s[ini-1] == '_' || s[ini-1] == '.' || s[ini-1] == '-' || s[ini-1] >= 0x80) {
			ini--
		}
		by(s[ini:end])
	}
	return out
}

// looksLikeDateTime: "2026-10-03", "03/10/2026", "2026-10-03T12:30:00", "12:30:00".
var reDateTime = regexp.MustCompile(`^(?:\d{4}[-/.]\d{1,2}[-/.]\d{1,2}|\d{1,2}[-/.]\d{1,2}[-/.]\d{4})(?:[T ]\d{1,2}:\d{2}(?::\d{2}(?:[.,]\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?$|^\d{1,2}:\d{2}(?::\d{2})?$`)

func looksLikeDateTime(v string) bool { return reDateTime.MatchString(v) }

// skipBlank avança espaços, quebras de linha (e vírgulas, se virgula) e o número de linha
// que a ferramenta de leitura do Claude Code põe no começo de cada linha ("12<tab>").
func skipBlank(s string, p int, comma bool) int {
	for p < len(s) {
		switch c := s[p]; {
		case c == ' ' || c == '\t' || c == '\r' || (comma && c == ','):
			p++
		case c == '\n':
			p++
			p += lineNumber(s[p:min(len(s), p+16)])
		default:
			return p
		}
	}
	return p
}
