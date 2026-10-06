package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detecção pelo nome do campo, parte 2: "campo: valor", JSON, SQL INSERT e XML.

var naoEValor = map[string]bool{"": true, "null": true, "none": true, "nil": true, "nan": true, "n/a": true, "na": true, "-": true, "--": true,
	"true": true, "false": true, "undefined": true, "string": true, "varchar": true, "text": true, "number": true, "integer": true, "date": true}

// contas de serviço e nomes genéricos: mascarar "user=root" só atrapalharia

var usuarioComum = map[string]bool{"root": true, "admin": true, "administrator": true, "postgres": true, "mysql": true, "sa": true, "app": true,
	"user": true, "usuario": true, "test": true, "teste": true, "ubuntu": true, "ec2-user": true, "airflow": true, "datahub": true, "system": true,
	"sys": true, "sysadmin": true, "guest": true, "nobody": true, "www-data": true, "default": true, "anonymous": true, "public": true, "git": true,
	"docker": true, "oracle": true, "hadoop": true, "spark": true, "hive": true, "kafka": true, "jenkins": true, "claude": true, "assistant": true}

var rePseudoPronto = regexp.MustCompile(`^(?:[A-Z:]+-[a-z2-7]{8}|Pessoa [a-z2-7]{8}|\S+\.invalid)$`)

// marcar aplica valorDoCampo ao trecho s[ini:fim] e registra o achado. Num campo de nome,
// "Fulano de Tal / Beltrana Souza" são duas pessoas: cada parte é um achado.
func marcar(s string, ini, fim int, classe string, add func(ini, fim int, tipo string)) {
	if classe == "nomesolto" {
		return // só vale em tabela, onde vira "nomecompleto" (ver cabecalho)
	}
	if (classe == "nome" || classe == "nomecompleto") && strings.ContainsAny(s[ini:fim], "/;&") {
		for p := ini; p < fim; {
			q := p
			for q < fim && s[q] != '/' && s[q] != ';' && s[q] != '&' {
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
				if tipo, ok := valorDoCampo(classe, s[a:b]); ok {
					add(a, b, tipo)
				}
			}
			p = q + 1
		}
		return
	}
	if tipo, ok := valorDoCampo(classe, s[ini:fim]); ok {
		add(ini, fim, tipo)
	}
}

// valorDoCampo decide se v, num campo da classe dada, deve ser mascarado, e com que tipo.
func valorDoCampo(classe, v string) (string, bool) {
	if naoEValor[strings.ToLower(v)] || len(v) > 120 || rePseudoPronto.MatchString(v) || ehPseudoObj(v) {
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
	switch classe {
	case "cartao", "conta":
		return classe, dig >= 4 && let <= 4
	case "quase":
		return classe, let+dig >= 1 && len(v) <= 60
	case "cpf", "cnpj", "rg", "cnh", "pis", "telefone", "cep", "doc":
		// uma data ou hora não é documento, por mais dígitos que tenha ("2026-10-03",
		// "12:30:00"): sem isto, uma data numa coluna dessas seria lembrada e mascarada em
		// todo lugar
		if pareceDataOuHora(v) {
			return "", false
		}
		if classe == "doc" && dig >= 3 && dig+let >= 6 {
			return "doc", true
		}
		// quantidade de dígitos de cada documento (com folga para zeros à esquerda cortados)
		faixa := map[string][2]int{"cpf": {9, 11}, "cnpj": {12, 14}, "rg": {5, 14}, "cnh": {9, 11}, "pis": {9, 11},
			"telefone": {8, 13}, "cep": {7, 8}, "doc": {5, 20}}[classe]
		if classe == "cnpj" && let > 0 { // CNPJ novo, com letras
			return classe, dig+let == 14 && dig >= 2
		}
		return classe, dig >= faixa[0] && dig <= faixa[1] && let <= 3
	case "nascimento":
		return classe, dig >= 6 && dig <= 14 && let <= 12
	case "endereco":
		return classe, let >= 4 && len(v) >= 6
	case "usuario":
		// não vale variável de ambiente, parâmetro de consulta (":user_id") nem trecho de código
		return classe, dig+let >= 2 && !usuarioComum[strings.ToLower(v)] && !strings.ContainsAny(v, " ()[]{}$<>:%")
	case "sensivel":
		return classe, let+dig >= 1
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
		return valorDoCampo("nome", v)
	case "nome":
		// nome de gente: letras, espaços e pouco mais (não "SVC_CARGA", "a.b:c", "x@y")
		for _, c := range v {
			if !unicode.IsLetter(c) && c != ' ' && c != '\'' && c != '-' && c != '.' {
				return "", false
			}
		}
		return classe, let >= 3
	}
	return "", false
}

// acharCampos procura "campo: valor", "campo=valor", "campo": "valor" (JSON) com campo
// sensível. Não usa regex: anda só pelos ":" e "=" do texto, olha a palavra que vem antes
// (o rótulo) e, se ela for de um campo sensível, pega o valor que vem depois. Em texto sem
// campo sensível o custo é uma consulta por separador.
func (m *Masker) acharCampos(s string, add func(ini, fim int, tipo string)) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != ':' && c != '=' {
			continue
		}
		prox, ant := byte(0), byte(0)
		if i+1 < len(s) {
			prox = s[i+1]
		}
		if i > 0 {
			ant = s[i-1]
		}
		// não são separadores de campo: "::", "==", "!=", "<=", ">=", ":=" (vale o "="), "://"
		if (c == ':' && (prox == ':' || prox == '=' || prox == '/' || ant == ':')) ||
			(c == '=' && (prox == '=' || ant == '=' || ant == '!' || ant == '<' || ant == '>')) {
			continue
		}
		classe := m.classeAntes(s, i)
		if classe == "" {
			continue
		}
		// valor
		j := i + 1
		if c == '=' && prox == '>' {
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
		j = depoisPrefixoLiteral(s, j)
		ini, fim := j, j
		if q := s[j]; q == '"' || q == '\'' || q == '`' {
			ini = j + 1
			fim = ini
			for fim < len(s) && s[fim] != q && s[fim] != '\n' && fim-ini < 120 {
				fim++
			}
		} else {
			// a crase e o ">" fecham código e marcação em volta do valor ("`User ID=x`", "<b>x</b>")
			for fim < len(s) && fim-ini < 120 && !strings.ContainsRune("\n\"'`,;|{}[]<>", rune(s[fim])) {
				fim++
			}
			if c == '=' {
				// "a=1 b=2" (log, .env): o valor acaba no espaço. Um nome de pessoa pode ter
				// espaços; aí vai até a palavra que já é de outro "campo=valor".
				p := ini
				for p < fim && s[p] != ' ' && s[p] != '\t' {
					p++
				}
				if strings.HasPrefix(classe, "nome") {
					for q := p; q < fim; {
						for q < fim && (s[q] == ' ' || s[q] == '\t') {
							q++
						}
						r := q
						for r < fim && s[r] != ' ' && s[r] != '\t' {
							r++
						}
						if r == q || strings.ContainsAny(s[q:r], "=:") {
							break
						}
						p, q = r, r
					}
				}
				fim = p
			}
		}
		fim = apararValor(s, ini, fim)
		if fim > ini && (classe == "quase" || !soLetrasCurto(s[ini:fim])) && !padraoOuModelo(s[ini:fim]) {
			marcar(s, ini, fim, classe, add)
		}
	}
}

// depoisPrefixoLiteral: se s[j:] começa com um prefixo de literal de string do Python (r, b,
// f, u, rb, br, fr, rf, em qualquer caixa) colado numa aspa, devolve a posição da aspa.
func depoisPrefixoLiteral(s string, j int) int {
	for n := 1; n <= 2 && j+n < len(s); n++ {
		if q := s[j+n]; q != '"' && q != '\'' {
			continue
		}
		switch strings.ToLower(s[j : j+n]) {
		case "r", "b", "f", "u", "rb", "br", "fr", "rf":
			if j == 0 || !ehIdent(s[j-1]) {
				return j + n
			}
		}
		return j
	}
	return j
}

// soLetrasCurto: 1 ou 2 letras e nada mais ("r", "pt", "M"): no "campo: valor" isso é sigla,
// prefixo ou pedaço de código, não o dado. Os quase identificadores (sexo "F", UF "MG") são a
// exceção: lá o valor é curto mesmo (e o detector é opcional).
func soLetrasCurto(v string) bool {
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

// padraoOuModelo: o valor é um padrão de regex ("(?!x)\\b...", "\\d+") ou um modelo de texto
// ("{valor}", "%s", "%(nome)s"): é código, não o dado do campo.
func padraoOuModelo(v string) bool {
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

// apararValor tira do fim do valor o que é da frase ou da marcação em volta, e não do valor:
// espaço, ponto final, e ")", "]", "}" ou "*" sem a abertura correspondente dentro do valor
// ("(User ID=x)", "**User ID=x**"). Sem isto, o fechamento ia junto para dentro do
// pseudônimo: o modelo não o via, e um valor com ")" era descartado inteiro (e saía em claro).
func apararValor(s string, ini, fim int) int {
	for fim > ini {
		v := s[ini:fim]
		switch s[fim-1] {
		case ' ', '\t', '\r', '.', '*':
		case ')':
			if strings.Count(v, "(") >= strings.Count(v, ")") {
				return fim
			}
		case ']':
			if strings.Count(v, "[") >= strings.Count(v, "]") {
				return fim
			}
		case '}':
			if strings.Count(v, "{") >= strings.Count(v, "}") {
				return fim
			}
		default:
			return fim
		}
		fim--
	}
	return fim
}

// classeAntes classifica o rótulo que termina logo antes de s[i] (o separador). Tenta com a
// última palavra, depois com as duas últimas, depois com as três ("cpf", "cliente cpf",
// "nome da mae"), e fica com a primeira que for um campo sensível.
func (m *Masker) classeAntes(s string, i int) string {
	fim := i
	for fim > 0 && (s[fim-1] == ' ' || s[fim-1] == '\t') {
		fim--
	}
	if fim > 0 && (s[fim-1] == '"' || s[fim-1] == '\'' || s[fim-1] == '`') {
		fim--
	}
	letra := func(b byte) bool { return ehAlnum(b) || b == '_' || b == '.' || b == '-' || b >= 0x80 }
	ini := fim
	for palavras := 0; palavras < 3; palavras++ {
		p := ini
		if palavras > 0 {
			if p == 0 || s[p-1] != ' ' {
				break
			}
			p--
		}
		q := p
		for q > 0 && letra(s[q-1]) && fim-q < 60 {
			q--
		}
		if q == p {
			break
		}
		ini = q
		if c := m.classe(s[ini:fim]); c != "" {
			return c
		}
	}
	return ""
}

// acharInsert: INSERT INTO t (col1, col2) VALUES (v1, v2), (...). As colunas dão o rótulo de
// cada valor.
func (m *Masker) acharInsert(s, baixo string, add func(ini, fim int, tipo string)) {
	if len(s) != len(baixo) {
		return
	}
	for pos := 0; ; {
		i := strings.Index(baixo[pos:], "insert into")
		if i < 0 {
			return
		}
		pos += i + 11
		a := strings.IndexByte(s[pos:], '(')
		v := strings.Index(baixo[pos:], "values")
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
			c := m.classe(strings.Trim(col, " \t\n\r`\"[]"))
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
			p = pularBranco(s, p, true)
			if p >= len(s) || s[p] != '(' {
				break
			}
			p++
			for col := 0; p < len(s); col++ {
				p = pularBranco(s, p, false)
				ini, fim := p, p
				if p < len(s) && s[p] == '\'' {
					ini = p + 1
					fim = ini
					for fim < len(s) && !(s[fim] == '\'' && (fim+1 >= len(s) || s[fim+1] != '\'')) {
						if s[fim] == '\'' {
							fim++ // aspa dobrada dentro do texto
						}
						fim++
					}
					p = fim + 1
				} else {
					for fim < len(s) && s[fim] != ',' && s[fim] != ')' {
						fim++
					}
					p = fim
				}
				if col < len(classes) && classes[col] != "" && fim > ini && fim <= len(s) {
					marcar(s, ini, fim, classes[col], add)
				}
				p = pularBranco(s, p, false)
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

// acharXML: <campo>valor</campo>, com campo sensível.
func (m *Masker) acharXML(s string, add func(ini, fim int, tipo string)) {
	for pos := 0; ; {
		i := strings.Index(s[pos:], "</")
		if i < 0 {
			return
		}
		fecha := pos + i
		pos = fecha + 2
		f := strings.IndexByte(s[pos:], '>')
		if f < 1 || f > 60 {
			continue
		}
		rotulo := s[pos : pos+f]
		classe := m.classe(rotulo)
		if classe == "" || classe == "nomesolto" {
			continue
		}
		// o valor é o que está entre o ">" da abertura e o "</"
		a0 := max(0, fecha-122)
		ab := strings.LastIndexByte(s[a0:fecha], '>')
		if ab >= 0 {
			ab += a0
		}
		if ab < 0 || fecha-ab-1 > 120 || fecha-ab-1 < 1 || strings.ContainsAny(s[ab+1:fecha], "<\n") {
			continue
		}
		marcar(s, ab+1, fecha, classe, add)
	}
}

// Coluna descreve como um nome de campo foi entendido (para o comando "llm-dlp colunas").
type Coluna struct {
	Nome, Classe string
}

// DescreverCampos acha os nomes de campo de um texto (o cabeçalho de uma tabela, ou as
// chaves de "campo: valor"/JSON) e diz como cada um foi classificado.
func (m *Masker) DescreverCampos(s string) []Coluna {
	var out []Coluna
	visto := map[string]bool{}
	por := func(nome string) {
		nome = strings.TrimSpace(nome)
		if nome == "" || visto[nome] || len(nome) > 60 {
			return
		}
		visto[nome] = true
		out = append(out, Coluna{nome, m.vocab.classeRotulo(nome)})
	}
	// 1) primeira linha com cara de cabeçalho de tabela
	for _, linha := range strings.SplitN(s, "\n", 30) {
		if ehSeparadorMarkdown(linha) || strings.ContainsAny(linha, "={}()") {
			continue
		}
		melhor, n := byte(0), 0
		for _, sep := range []byte{'\t', '|', ';', ','} {
			if c := strings.Count(linha, string(sep)); c > n {
				melhor, n = sep, c
			}
		}
		if n == 0 {
			continue
		}
		cels := celulas(linha, melhor)
		ok := len(cels) >= 2
		for _, c := range cels {
			a, b := aparar(linha, c[0], c[1])
			if b-a > 48 || strings.Contains(linha[a:b], ":") {
				ok = false
			}
		}
		if ok {
			for _, c := range cels {
				a, b := aparar(linha, c[0], c[1])
				por(linha[a:b])
			}
			return out
		}
	}
	// 2) chaves de "campo: valor" / JSON
	for i := 0; i < len(s) && len(out) < 400; i++ {
		if s[i] != ':' && s[i] != '=' {
			continue
		}
		fim := i
		for fim > 0 && (s[fim-1] == ' ' || s[fim-1] == '"' || s[fim-1] == '\'') {
			fim--
		}
		ini := fim
		for ini > 0 && (ehAlnum(s[ini-1]) || s[ini-1] == '_' || s[ini-1] == '.' || s[ini-1] == '-' || s[ini-1] >= 0x80) {
			ini--
		}
		por(s[ini:fim])
	}
	return out
}

// pareceDataOuHora: "2026-10-03", "03/10/2026", "2026-10-03T12:30:00", "12:30:00".
var reDataHora = regexp.MustCompile(`^(?:\d{4}[-/.]\d{1,2}[-/.]\d{1,2}|\d{1,2}[-/.]\d{1,2}[-/.]\d{4})(?:[T ]\d{1,2}:\d{2}(?::\d{2}(?:[.,]\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?$|^\d{1,2}:\d{2}(?::\d{2})?$`)

func pareceDataOuHora(v string) bool { return reDataHora.MatchString(v) }

// pularBranco avança espaços, quebras de linha (e vírgulas, se virgula) e o número de linha
// que a ferramenta de leitura do Claude Code põe no começo de cada linha ("12<tab>").
func pularBranco(s string, p int, virgula bool) int {
	for p < len(s) {
		switch c := s[p]; {
		case c == ' ' || c == '\t' || c == '\r' || (virgula && c == ','):
			p++
		case c == '\n':
			p++
			p += numeroDeLinha(s[p:min(len(s), p+16)])
		default:
			return p
		}
	}
	return p
}
