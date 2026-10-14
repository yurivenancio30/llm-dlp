package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detecção pelo nome do campo, parte 3: tabelas (CSV, markdown, terminal, planilha).

// findTables procura tabelas com cabeçalho e mascara as células das colunas sensíveis.
// Só vale para as linhas logo abaixo do cabeçalho, com a mesma forma. Formas aceitas:
//   - com separador: CSV, TSV, ";", "|", markdown, tabela de terminal (mysql, psql);
//   - linhas no formato do Python: ('NOME', 'CPF') / ['NOME', 'CPF'] (openpyxl, csv.reader);
//   - colunas alinhadas por espaços: a impressão de uma planilha ou DataFrame (pandas),
//     "column -t", saída de terminal sem bordas.
//
// É assim que planilha Excel é coberta: o .xlsx nunca vai para a API como arquivo; o que
// vai é o texto que uma ferramenta tirou dele (CSV, markdown, DataFrame impresso, tuplas).
func (m *Masker) findTables(s string, add func(ini, end int, typ string)) {
	line := func(ini int) (string, int) { // a linha que começa em ini, e o começo da próxima
		f := strings.IndexByte(s[ini:], '\n')
		if f < 0 {
			return s[ini:], len(s)
		}
		return s[ini : ini+f], ini + f + 1
	}
	for ini := 0; ini < len(s); {
		l, next := line(ini)
		// A ferramenta de leitura do Claude Code põe o número da linha antes de cada linha
		// ("12<tab>conteúdo"). Sem tirar isso, a primeira coluna de toda tabela ficaria com o
		// número grudado no valor e não seria reconhecida.
		num := lineNumber(l)
		lineStart := ini
		l, ini = l[num:], ini+num
		if classes, sep := m.header(l); classes != nil && !continuesTable(s, lineStart, sep, len(classes)) {
			ini = next
			for ini < len(s) {
				l, next = line(ini)
				if num > 0 { // se o cabeçalho tinha número de linha, as linhas também têm
					k := lineNumber(l)
					l, ini = l[k:], ini+k
				}
				if !isMarkdownSeparator(l) {
					cells := splitText(l, sep)
					if sep == ',' && notTableLine(l) {
						cells = nil
					}
					if len(cells) != len(classes) {
						break // fim da tabela; esta linha volta a ser candidata a cabeçalho
					}
					for i, c := range cells {
						if classes[i] != "" {
							a, b := trim(l, c[0], c[1])
							markIt(s, ini+a, ini+b, classes[i], add)
						}
					}
				}
				ini = next
			}
			continue
		}
		if m.alignedHeader(l) != nil && !continuesTable(s, lineStart, ' ', 0) {
			if end := m.alignedTable(s, ini, num > 0, add); end > ini {
				ini = end
				continue
			}
		}
		ini = next
	}
}

// continuesTable: a linha que começa em ini é a continuação de uma tabela que já vinha das
// linhas de cima? Um cabeçalho só pode ser a PRIMEIRA linha da tabela: numa tabela de
// documentação ("| Column | Type | Description |"), a linha de dados "| USER_ID | NUMBER |
// ... |" não é cabeçalho, por mais que "USER_ID" seja nome de campo.
func continuesTable(s string, ini int, sep byte, n int) bool {
	if ini == 0 {
		return false
	}
	a := strings.LastIndexByte(s[:ini-1], '\n') + 1
	prev := s[a : ini-1]
	prev = prev[lineNumber(prev):]
	if strings.TrimSpace(prev) == "" {
		return false
	}
	if isMarkdownSeparator(prev) {
		// separador logo abaixo de um cabeçalho ("|---|"): continuação. Borda de cima de uma
		// tabela de terminal ("+----+", sem nada acima): não é.
		if a == 0 {
			return false
		}
		b := strings.LastIndexByte(s[:a-1], '\n') + 1
		above := s[b : a-1]
		above = above[lineNumber(above):]
		return strings.TrimSpace(above) != "" && !isMarkdownSeparator(above)
	}
	if sep == ' ' { // tabela alinhada por espaços: a linha de cima também tem colunas separadas por 2+ espaços
		return strings.Contains(strings.TrimSpace(prev), "  ")
	}
	return len(splitText(prev, sep)) == n
}

// notTableLine: a linha tem vírgulas mas é uma lista de colunas de SQL ou uma
// enumeração em texto, não uma linha de CSV. Sinais: começa recuada, termina em vírgula,
// tem crases ou parênteses, ou começa com uma palavra de SQL.
//
//	select start_time, user_name, role_name     (SQL)
//	    user_name,                              (SQL, uma coluna por linha)
//	`user_id`, `user_name`, `role_name`         (texto)
var reSQLStart = regexp.MustCompile(`(?i)^(?:select|from|where|group|order|having|insert|update|delete|values|set|join|left|right|inner|on|and|or|with|union|create|alter|partition|by|case|when)\b`)

func notTableLine(l string) bool {
	if strings.IndexByte(l, ',') < 0 || strings.IndexByte(l, ';') >= 0 || strings.IndexByte(l, '\t') >= 0 || strings.IndexByte(l, '|') >= 0 {
		return false // só se aplica a linhas separadas por vírgula
	}
	t := strings.TrimRight(l, " \r")
	if t == "" {
		return true
	}
	if l[0] == ' ' || t[len(t)-1] == ',' || strings.ContainsAny(t, "`()") {
		return true
	}
	return reSQLStart.MatchString(t)
}

// lineNumber devolve o tamanho do prefixo de número de linha de l ("  12<tab>" ou
// "  12→"), ou 0 se não houver.
func lineNumber(l string) int {
	i := 0
	for i < len(l) && l[i] == ' ' {
		i++
	}
	j := i
	for j < len(l) && j-i < 8 && l[j] >= '0' && l[j] <= '9' {
		j++
	}
	if j == i || j >= len(l) {
		return 0
	}
	if l[j] == '\t' {
		return j + 1
	}
	if strings.HasPrefix(l[j:], "→") {
		return j + len("→")
	}
	return 0
}

// dividir: as células de uma linha de tabela. sep 'P' é o formato do Python: (a, b) ou [a, b].
func splitText(l string, sep byte) [][2]int {
	if sep != 'P' {
		return cellsOf(l, sep)
	}
	a, b := 0, len(l)
	for a < b && l[a] == ' ' {
		a++
	}
	for b > a && (l[b-1] == ' ' || l[b-1] == ',' || l[b-1] == '\r') {
		b--
	}
	if b-a < 2 || !((l[a] == '(' && l[b-1] == ')') || (l[a] == '[' && l[b-1] == ']')) {
		return nil
	}
	a, b = a+1, b-1
	var out [][2]int
	ini, quote := a, byte(0)
	for i := a; i < b; i++ {
		switch c := l[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ',':
			out = append(out, [2]int{ini, i})
			ini = i + 1
		}
	}
	return append(out, [2]int{ini, b})
}

type alignedColumn struct {
	ini, end int // posição do nome da coluna na linha do cabeçalho, em caracteres
	class    string
}

// alignedHeader: a linha é um cabeçalho de tabela alinhada por espaços (nomes de coluna
// separados por espaços), com alguma coluna sensível?
func (m *Masker) alignedHeader(l string) []alignedColumn {
	if len(l) < 5 || len(l) > 2000 || strings.ContainsAny(l, "=(){}@:;|,\t\"'.!?") {
		return nil
	}
	rs := []rune(l)
	splitAt := func(minSpace int) []alignedColumn {
		var cols []alignedColumn
		for i := 0; i < len(rs); {
			for i < len(rs) && rs[i] == ' ' {
				i++
			}
			if i >= len(rs) {
				break
			}
			ini := i
			for i < len(rs) && !(rs[i] == ' ' && (minSpace == 1 || i+1 >= len(rs) || rs[i+1] == ' ')) {
				i++
			}
			cols = append(cols, alignedColumn{ini, i, m.class(string(rs[ini:i]))})
		}
		return cols
	}
	// quantas colunas sensíveis a divisão reconhece (0 = não serve como cabeçalho)
	sensitives := func(cols []alignedColumn) int {
		if len(cols) < 2 {
			return 0
		}
		sens := 0
		for _, c := range cols {
			name := string(rs[c.ini:c.end])
			// palavra solta de frase ("o", "de", "foi"): não é nome de coluna
			if len(name) > 48 || (len(name) <= 3 && name == strings.ToLower(name) && c.class == "") {
				return 0
			}
			if c.class != "" && c.class != "nomesolto" {
				sens++
			}
		}
		return sens
	}
	// Os nomes podem estar separados por 2+ espaços (e ter espaço dentro) ou por um espaço só
	// (o pandas, sem índice, deixa um espaço entre colunas). Fica a divisão que reconhece
	// mais colunas sensíveis.
	cols, c1 := splitAt(2), splitAt(1)
	if sensitives(c1) > sensitives(cols) {
		cols = c1
	}
	if sensitives(cols) == 0 {
		return nil
	}
	for i := range cols {
		if cols[i].class == "nomesolto" {
			cols[i].class = "nomecompleto"
		}
	}
	return cols
}

// alignedTable trata a tabela alinhada por espaços cujo cabeçalho começa em s[ini].
// Devolve onde a tabela acaba (ini se a linha não era um cabeçalho de tabela).
//
// Os limites de cada coluna são procurados ENTRE os nomes do cabeçalho: tem que haver, entre
// um nome e o seguinte, uma posição que seja espaço em todas as linhas (o "corredor").
// Funciona com valores alinhados à direita (pandas), à esquerda ("column -t"), com coluna de
// índice e com um espaço só entre colunas.
func (m *Masker) alignedTable(s string, ini int, numbered bool, add func(ini, end int, typ string)) int {
	type lineV struct {
		ini int // onde a linha começa em s
		rs  []rune
		pos []int // caractere -> byte dentro da linha
	}
	var ls []lineV
	wid := 0
	for p := ini; p < len(s) && len(ls) < 5000; {
		f := strings.IndexByte(s[p:], '\n')
		txt := s[p:]
		if f >= 0 {
			txt = s[p : p+f]
		}
		q := p // onde o conteúdo da linha começa (depois do número de linha, se houver)
		if numbered && len(ls) > 0 {
			k := lineNumber(txt)
			txt, q = txt[k:], p+k
		}
		if (len(ls) > 0 && strings.TrimSpace(txt) == "") || len(txt) > 4000 {
			break
		}
		rs := []rune(strings.TrimRight(txt, "\r"))
		pos := make([]int, len(rs)+1)
		b := 0
		for i, r := range rs {
			pos[i] = b
			b += len(string(r))
		}
		pos[len(rs)] = b
		ls = append(ls, lineV{q, rs, pos})
		if len(rs) > wid {
			wid = len(rs)
		}
		if f < 0 {
			break
		}
		p += f + 1
	}
	cols := m.alignedHeader(string(ls[0].rs))
	if cols == nil || len(ls) < 2 {
		return ini
	}
	// Quais linhas são da tabela: uma linha entra enquanto sobrar, entre cada nome do cabeçalho
	// e o seguinte, alguma posição que é espaço em todas as linhas aceitas. A primeira que
	// fecha um desses corredores (texto que veio logo depois, sem linha em branco) encerra a
	// tabela.
	free := make([]bool, wid+2) // posição é espaço em todas as linhas aceitas
	for c := range free {
		free[c] = c >= len(ls[0].rs) || ls[0].rs[c] == ' '
	}
	n, hasIndex := 1, false
	for i := 1; i < len(ls); i++ {
		tmp := append([]bool(nil), free...)
		for c, r := range ls[i].rs {
			if r != ' ' {
				tmp[c] = false
			}
		}
		fits := true
		if hasIndex { // a linha tem que manter o corredor entre o índice e a primeira coluna
			fits = false
			for c := 1; c < cols[0].ini; c++ {
				if tmp[c] {
					fits = true
					break
				}
			}
		}
		for j := 0; j+1 < len(cols) && fits; j++ {
			fits = false
			for c := cols[j].end; c < cols[j+1].ini; c++ {
				if tmp[c] {
					fits = true
					break
				}
			}
		}
		if !fits {
			break
		}
		free, n = tmp, i+1
		if i == 1 {
			// a primeira linha de dados diz se há coluna de índice (pandas): algo escrito e,
			// depois, um corredor, tudo antes do primeiro nome do cabeçalho
			for c := 1; c < cols[0].ini && c < len(ls[1].rs); c++ {
				// dois espaços seguidos: um espaço só pode ser o de dentro de um nome
				if free[c] && free[c+1] && c+1 < cols[0].ini && strings.TrimSpace(string(ls[1].rs[:c])) != "" {
					hasIndex = true
					break
				}
			}
		}
	}
	if n < 2 {
		return ini
	}
	if n == 2 {
		// Uma linha de dados só: não há como saber onde uma coluna acaba e a outra começa
		// (todo espaço da linha parece um corredor). Na dúvida, mascara: todo número longo
		// se a tabela tem coluna de documento, e toda sequência de palavras se tem coluna de
		// nome. Pode pegar um valor de outra coluna; não deixa passar o da coluna sensível.
		hasName := false
		var others []string // classes sensíveis que não são de nome
		for _, c := range cols {
			switch c.class {
			case "nome", "nomecompleto":
				hasName = true
			case "":
			default:
				others = append(others, c.class)
			}
		}
		classNum := ""
		if len(others) > 0 {
			classNum = others[0]
		}
		// um valor que não é nome é marcado com a primeira classe sensível que o aceitar
		markOther := func(a, b int) {
			for _, c := range others {
				if typ, ok := fieldValue(c, s[a:b]); ok {
					add(a, b, typ)
					return
				}
			}
		}
		l := ls[1]
		letter := func(r rune) bool { return unicode.IsLetter(r) || r == '\'' || r == '-' || r == '.' }
		for i := 0; i < len(l.rs); {
			if l.rs[i] == ' ' {
				i++
				continue
			}
			j := i
			lettersOnly := true
			for j < len(l.rs) && l.rs[j] != ' ' {
				lettersOnly = lettersOnly && letter(l.rs[j])
				j++
			}
			if lettersOnly && hasName {
				for j+1 < len(l.rs) && l.rs[j] == ' ' && letter(l.rs[j+1]) { // junta as palavras seguintes
					k := j + 1
					for k < len(l.rs) && letter(l.rs[k]) {
						k++
					}
					if k < len(l.rs) && l.rs[k] != ' ' {
						break
					}
					j = k
				}
				// só o que tem cara de nome de gente (nome e sobrenome com maiúscula)
				markIt(s, l.ini+l.pos[i], l.ini+l.pos[j], "nomecompleto", add)
			} else if !lettersOnly && classNum != "" && (i > 0 || cols[0].ini == 0) { // i > 0: não o índice da linha
				markOther(l.ini+l.pos[i], l.ini+l.pos[j])
			}
			i = j
		}
		return min(len(s), l.ini+l.pos[len(l.rs)]+1)
	}
	{
		lane := func(c int) (int, int) { // o corredor que contém a posição c: [a, b)
			a, b := c, c+1
			for a > 0 && free[a-1] {
				a--
			}
			for b < wid && free[b] {
				b++
			}
			return a, b
		}
		left := make([]int, len(cols)) // limites de cada coluna, em caracteres: [esq, dir)
		dir := make([]int, len(cols))
		ok := true
		// antes da primeira coluna pode haver um índice (pandas): a coluna começa depois do
		// primeiro corredor que vem após algum caractere
		left[0] = 0
		for c := 0; c < cols[0].ini; c++ {
			if free[c] {
				_, b := lane(c)
				if c == 0 || b <= cols[0].ini {
					left[0] = b
				}
				if c > 0 {
					break
				}
				c = b
			}
		}
		if left[0] > cols[0].ini {
			left[0] = cols[0].ini
		}
		dir[len(cols)-1] = wid
		for j := 0; j+1 < len(cols) && ok; j++ {
			e, f := cols[j].end, cols[j+1].ini-1 // logo depois de um nome, logo antes do seguinte
			switch {
			case free[e] && free[f]:
				a1, b1 := lane(e)
				a2, b2 := lane(f)
				if a1 == a2 { // um corredor só entre os dois nomes
					dir[j], left[j+1] = a1, b1
				} else if cols[j].class != "" { // há valores no meio: na dúvida, ficam com a coluna sensível
					dir[j], left[j+1] = a2, b2
				} else {
					dir[j], left[j+1] = a1, b1
				}
			case free[e]: // valores da coluna seguinte passam à esquerda do nome dela (alinhados à direita)
				a, b := lane(e)
				dir[j], left[j+1] = a, b
			case free[f]: // valores desta coluna passam à direita do nome dela (alinhados à esquerda)
				a, b := lane(f)
				dir[j], left[j+1] = a, b
			default: // procura um corredor no meio
				ok = false
				for c := e; c <= f; c++ {
					if free[c] {
						a, b := lane(c)
						dir[j], left[j+1], ok = a, b, true
						break
					}
				}
			}
		}
		if !ok {
			return ini
		}
		for _, l := range ls[1:n] {
			if isMarkdownSeparator(string(l.rs)) {
				continue
			}
			for j, c := range cols {
				if c.class == "" {
					continue
				}
				a, b := min(left[j], len(l.rs)), min(dir[j], len(l.rs))
				for a < b && l.rs[a] == ' ' {
					a++
				}
				for b > a && l.rs[b-1] == ' ' {
					b--
				}
				if b > a {
					markIt(s, l.ini+l.pos[a], l.ini+l.pos[b], c.class, add)
				}
			}
		}
		lastPos := ls[n-1]
		return min(len(s), lastPos.ini+lastPos.pos[len(lastPos.rs)]+1)
	}
}

// cabecalho: a linha é o cabeçalho de uma tabela com alguma coluna sensível? Devolve a
// classe de cada coluna e o separador. Rejeita cedo o que não tem cara de cabeçalho.
func (m *Masker) header(line string) ([]string, byte) {
	if len(line) < 5 || len(line) > 4000 {
		return nil, 0
	}
	if cells := splitText(line, 'P'); len(cells) >= 2 { // ('NOME', 'CPF') ou ['NOME', 'CPF']
		cs := make([]string, len(cells))
		sens := 0
		for i, c := range cells {
			a, b := trim(line, c[0], c[1])
			if b-a > 48 {
				return nil, 0
			}
			if cs[i] = m.class(line[a:b]); cs[i] != "" && cs[i] != "nomesolto" {
				sens++
			}
		}
		if sens == 0 {
			return nil, 0
		}
		for i := range cs {
			if cs[i] == "nomesolto" {
				cs[i] = "nomecompleto"
			}
		}
		return cs, 'P'
	}
	if notTableLine(line) {
		return nil, 0
	}
	// o separador é o mais frequente entre tab, "|", ";" e ","
	var n [4]int
	seps := [4]byte{'\t', '|', ';', ','}
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\t':
			n[0]++
		case '|':
			n[1]++
		case ';':
			n[2]++
		case ',':
			n[3]++
		case '=', '(', ')', '{', '}', '@':
			return nil, 0 // código ou dado, não nomes de coluna
		}
	}
	best := -1
	for i := range n {
		if n[i] > 0 && (best < 0 || n[i] > n[best]) {
			best = i
		}
	}
	if best < 0 {
		return nil, 0
	}
	cells := cellsOf(line, seps[best])
	if len(cells) < 2 {
		return nil, 0
	}
	cs := make([]string, len(cells))
	sens := 0
	for i, c := range cells {
		a, b := trim(line, c[0], c[1])
		cellText := line[a:b]
		if len(cellText) > 48 || strings.Contains(cellText, ":") || strings.Contains(cellText, "//") {
			return nil, 0
		}
		if cs[i] = m.class(cellText); cs[i] != "" && cs[i] != "nomesolto" {
			sens++
		}
	}
	if sens == 0 {
		return nil, 0
	}
	for i := range cs { // "nome" sozinho, numa tabela com outra coluna de pessoa, é nome de pessoa
		if cs[i] == "nomesolto" {
			cs[i] = "nomecompleto"
		}
	}
	return cs, seps[best]
}

// celulas divide a linha pelo separador, respeitando aspas duplas; devolve [ini,fim) de cada célula.
func cellsOf(line string, sep byte) [][2]int {
	if strings.IndexByte(line, sep) < 0 {
		return nil
	}
	var out [][2]int
	ini, quotes := 0, false
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '"':
			quotes = !quotes
		case line[i] == sep && !quotes:
			out = append(out, [2]int{ini, i})
			ini = i + 1
		}
	}
	out = append(out, [2]int{ini, len(line)})
	if sep == '|' && len(out) >= 3 { // "| a | b |": as pontas vazias não são colunas
		if strings.TrimSpace(line[out[0][0]:out[0][1]]) == "" {
			out = out[1:]
		}
		if n := len(out); n > 0 && strings.TrimSpace(line[out[n-1][0]:out[n-1][1]]) == "" {
			out = out[:n-1]
		}
	}
	return out
}

func trim(line string, a, b int) (int, int) {
	for a < b && (line[a] == ' ' || line[a] == '\t' || line[a] == '"' || line[a] == '\'' || line[a] == '\r') {
		a++
	}
	for b > a && (line[b-1] == ' ' || line[b-1] == '\t' || line[b-1] == '"' || line[b-1] == '\'' || line[b-1] == '\r') {
		b--
	}
	return a, b
}

// isMarkdownSeparator: linha só de traços, como a que separa o cabeçalho numa tabela
// markdown ("|---|:-:|") ou de terminal ("+----+----+", "-----+-----", "=====").
func isMarkdownSeparator(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) >= 3 && strings.Trim(t, "|-:+= ") == ""
}

// identifiesPerson: tipos de achado que, numa linha de tabela, mostram que a linha é de uma
// pessoa.
var identifiesPerson = map[string]bool{"cpf": true, "email": true, "telefone": true, "rg": true, "cnh": true, "pis": true, "nascimento": true}

// namesInLine: numa linha de tabela sem cabeçalho (colada no chat, um pedaço de CSV) que já
// tem um CPF, e-mail ou telefone detectado, a célula com cara de nome completo é o nome da
// pessoa. "Maria Lopes | 529.982.247-25 | maria@x.com" -> "Maria Lopes" também é mascarado.
func namesInLine(s string, findings []Finding) []Finding {
	lines := map[int]bool{} // começo das linhas que têm um identificador de pessoa
	for _, a := range findings {
		if identifiesPerson[a.Type] {
			// linha de mais de 2000 caracteres não é linha de tabela: a busca para aí
			a0 := max(0, a.Start-2001)
			k := strings.LastIndexByte(s[a0:a.Start], '\n')
			if k < 0 && a0 > 0 {
				continue
			}
			lines[a0+k+1] = true
		}
	}
	var out []Finding
	for ini := range lines {
		z := min(len(s), ini+2001)
		end := strings.IndexByte(s[ini:z], '\n')
		if end < 0 {
			end = z - ini
		}
		l := s[ini : ini+end]
		if len(l) > 2000 {
			continue
		}
		var sep byte
		for _, c := range []byte{'|', '\t', ';', ','} {
			if strings.Count(l, string(c)) >= 1 {
				sep = c
				break
			}
		}
		if sep == 0 {
			continue
		}
		for _, c := range cellsOf(l, sep) {
			a, b := trim(l, c[0], c[1])
			if _, ok := fieldValue("nomecompleto", l[a:b]); ok && len(strings.Fields(l[a:b])) >= 2 {
				out = append(out, Finding{ini + a, ini + b, "nome", l[a:b]})
			}
		}
	}
	return out
}
