package mask

import (
	"regexp"
	"strings"
	"unicode"
)

// Detecção pelo nome do campo, parte 3: tabelas (CSV, markdown, terminal, planilha).

// acharTabelas procura tabelas com cabeçalho e mascara as células das colunas sensíveis.
// Só vale para as linhas logo abaixo do cabeçalho, com a mesma forma. Formas aceitas:
//   - com separador: CSV, TSV, ";", "|", markdown, tabela de terminal (mysql, psql);
//   - linhas no formato do Python: ('NOME', 'CPF') / ['NOME', 'CPF'] (openpyxl, csv.reader);
//   - colunas alinhadas por espaços: a impressão de uma planilha ou DataFrame (pandas),
//     "column -t", saída de terminal sem bordas.
//
// É assim que planilha Excel é coberta: o .xlsx nunca vai para a API como arquivo; o que
// vai é o texto que uma ferramenta tirou dele (CSV, markdown, DataFrame impresso, tuplas).
func (m *Masker) acharTabelas(s string, add func(ini, fim int, tipo string)) {
	linha := func(ini int) (string, int) { // a linha que começa em ini, e o começo da próxima
		f := strings.IndexByte(s[ini:], '\n')
		if f < 0 {
			return s[ini:], len(s)
		}
		return s[ini : ini+f], ini + f + 1
	}
	for ini := 0; ini < len(s); {
		l, prox := linha(ini)
		// A ferramenta de leitura do Claude Code põe o número da linha antes de cada linha
		// ("12<tab>conteúdo"). Sem tirar isso, a primeira coluna de toda tabela ficaria com o
		// número grudado no valor e não seria reconhecida.
		num := numeroDeLinha(l)
		iniLinha := ini
		l, ini = l[num:], ini+num
		if classes, sep := m.cabecalho(l); classes != nil && !continuaTabela(s, iniLinha, sep, len(classes)) {
			ini = prox
			for ini < len(s) {
				l, prox = linha(ini)
				if num > 0 { // se o cabeçalho tinha número de linha, as linhas também têm
					k := numeroDeLinha(l)
					l, ini = l[k:], ini+k
				}
				if !ehSeparadorMarkdown(l) {
					cels := dividir(l, sep)
					if sep == ',' && naoEhLinhaDeTabela(l) {
						cels = nil
					}
					if len(cels) != len(classes) {
						break // fim da tabela; esta linha volta a ser candidata a cabeçalho
					}
					for i, c := range cels {
						if classes[i] != "" {
							a, b := aparar(l, c[0], c[1])
							marcar(s, ini+a, ini+b, classes[i], add)
						}
					}
				}
				ini = prox
			}
			continue
		}
		if m.cabecalhoAlinhado(l) != nil && !continuaTabela(s, iniLinha, ' ', 0) {
			if fim := m.tabelaAlinhada(s, ini, num > 0, add); fim > ini {
				ini = fim
				continue
			}
		}
		ini = prox
	}
}

// continuaTabela: a linha que começa em ini é a continuação de uma tabela que já vinha das
// linhas de cima? Um cabeçalho só pode ser a PRIMEIRA linha da tabela: numa tabela de
// documentação ("| Column | Type | Description |"), a linha de dados "| USER_ID | NUMBER |
// ... |" não é cabeçalho, por mais que "USER_ID" seja nome de campo.
func continuaTabela(s string, ini int, sep byte, n int) bool {
	if ini == 0 {
		return false
	}
	a := strings.LastIndexByte(s[:ini-1], '\n') + 1
	ant := s[a : ini-1]
	ant = ant[numeroDeLinha(ant):]
	if strings.TrimSpace(ant) == "" {
		return false
	}
	if ehSeparadorMarkdown(ant) {
		// separador logo abaixo de um cabeçalho ("|---|"): continuação. Borda de cima de uma
		// tabela de terminal ("+----+", sem nada acima): não é.
		if a == 0 {
			return false
		}
		b := strings.LastIndexByte(s[:a-1], '\n') + 1
		acima := s[b : a-1]
		acima = acima[numeroDeLinha(acima):]
		return strings.TrimSpace(acima) != "" && !ehSeparadorMarkdown(acima)
	}
	if sep == ' ' { // tabela alinhada por espaços: a linha de cima também tem colunas separadas por 2+ espaços
		return strings.Contains(strings.TrimSpace(ant), "  ")
	}
	return len(dividir(ant, sep)) == n
}

// naoEhLinhaDeTabela: a linha tem vírgulas mas é uma lista de colunas de SQL ou uma
// enumeração em texto, não uma linha de CSV. Sinais: começa recuada, termina em vírgula,
// tem crases ou parênteses, ou começa com uma palavra de SQL.
//
//	select start_time, user_name, role_name     (SQL)
//	    user_name,                              (SQL, uma coluna por linha)
//	`user_id`, `user_name`, `role_name`         (texto)
var reComecoSQL = regexp.MustCompile(`(?i)^(?:select|from|where|group|order|having|insert|update|delete|values|set|join|left|right|inner|on|and|or|with|union|create|alter|partition|by|case|when)\b`)

func naoEhLinhaDeTabela(l string) bool {
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
	return reComecoSQL.MatchString(t)
}

// numeroDeLinha devolve o tamanho do prefixo de número de linha de l ("  12<tab>" ou
// "  12→"), ou 0 se não houver.
func numeroDeLinha(l string) int {
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
func dividir(l string, sep byte) [][2]int {
	if sep != 'P' {
		return celulas(l, sep)
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
	ini, aspa := a, byte(0)
	for i := a; i < b; i++ {
		switch c := l[i]; {
		case aspa != 0:
			if c == aspa {
				aspa = 0
			}
		case c == '\'' || c == '"':
			aspa = c
		case c == ',':
			out = append(out, [2]int{ini, i})
			ini = i + 1
		}
	}
	return append(out, [2]int{ini, b})
}

type colunaAlinhada struct {
	ini, fim int // posição do nome da coluna na linha do cabeçalho, em caracteres
	classe   string
}

// cabecalhoAlinhado: a linha é um cabeçalho de tabela alinhada por espaços (nomes de coluna
// separados por espaços), com alguma coluna sensível?
func (m *Masker) cabecalhoAlinhado(l string) []colunaAlinhada {
	if len(l) < 5 || len(l) > 2000 || strings.ContainsAny(l, "=(){}@:;|,\t\"'.!?") {
		return nil
	}
	rs := []rune(l)
	partir := func(minEsp int) []colunaAlinhada {
		var cols []colunaAlinhada
		for i := 0; i < len(rs); {
			for i < len(rs) && rs[i] == ' ' {
				i++
			}
			if i >= len(rs) {
				break
			}
			ini := i
			for i < len(rs) && !(rs[i] == ' ' && (minEsp == 1 || i+1 >= len(rs) || rs[i+1] == ' ')) {
				i++
			}
			cols = append(cols, colunaAlinhada{ini, i, m.classe(string(rs[ini:i]))})
		}
		return cols
	}
	// quantas colunas sensíveis a divisão reconhece (0 = não serve como cabeçalho)
	sensiveis := func(cols []colunaAlinhada) int {
		if len(cols) < 2 {
			return 0
		}
		sens := 0
		for _, c := range cols {
			nome := string(rs[c.ini:c.fim])
			// palavra solta de frase ("o", "de", "foi"): não é nome de coluna
			if len(nome) > 48 || (len(nome) <= 3 && nome == strings.ToLower(nome) && c.classe == "") {
				return 0
			}
			if c.classe != "" && c.classe != "nomesolto" {
				sens++
			}
		}
		return sens
	}
	// Os nomes podem estar separados por 2+ espaços (e ter espaço dentro) ou por um espaço só
	// (o pandas, sem índice, deixa um espaço entre colunas). Fica a divisão que reconhece
	// mais colunas sensíveis.
	cols, c1 := partir(2), partir(1)
	if sensiveis(c1) > sensiveis(cols) {
		cols = c1
	}
	if sensiveis(cols) == 0 {
		return nil
	}
	for i := range cols {
		if cols[i].classe == "nomesolto" {
			cols[i].classe = "nomecompleto"
		}
	}
	return cols
}

// tabelaAlinhada trata a tabela alinhada por espaços cujo cabeçalho começa em s[ini].
// Devolve onde a tabela acaba (ini se a linha não era um cabeçalho de tabela).
//
// Os limites de cada coluna são procurados ENTRE os nomes do cabeçalho: tem que haver, entre
// um nome e o seguinte, uma posição que seja espaço em todas as linhas (o "corredor").
// Funciona com valores alinhados à direita (pandas), à esquerda ("column -t"), com coluna de
// índice e com um espaço só entre colunas.
func (m *Masker) tabelaAlinhada(s string, ini int, numerada bool, add func(ini, fim int, tipo string)) int {
	type lin struct {
		ini int // onde a linha começa em s
		rs  []rune
		pos []int // caractere -> byte dentro da linha
	}
	var ls []lin
	larg := 0
	for p := ini; p < len(s) && len(ls) < 5000; {
		f := strings.IndexByte(s[p:], '\n')
		txt := s[p:]
		if f >= 0 {
			txt = s[p : p+f]
		}
		q := p // onde o conteúdo da linha começa (depois do número de linha, se houver)
		if numerada && len(ls) > 0 {
			k := numeroDeLinha(txt)
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
		ls = append(ls, lin{q, rs, pos})
		if len(rs) > larg {
			larg = len(rs)
		}
		if f < 0 {
			break
		}
		p += f + 1
	}
	cols := m.cabecalhoAlinhado(string(ls[0].rs))
	if cols == nil || len(ls) < 2 {
		return ini
	}
	// Quais linhas são da tabela: uma linha entra enquanto sobrar, entre cada nome do cabeçalho
	// e o seguinte, alguma posição que é espaço em todas as linhas aceitas. A primeira que
	// fecha um desses corredores (texto que veio logo depois, sem linha em branco) encerra a
	// tabela.
	livre := make([]bool, larg+2) // posição é espaço em todas as linhas aceitas
	for c := range livre {
		livre[c] = c >= len(ls[0].rs) || ls[0].rs[c] == ' '
	}
	n, temIndice := 1, false
	for i := 1; i < len(ls); i++ {
		tmp := append([]bool(nil), livre...)
		for c, r := range ls[i].rs {
			if r != ' ' {
				tmp[c] = false
			}
		}
		cabe := true
		if temIndice { // a linha tem que manter o corredor entre o índice e a primeira coluna
			cabe = false
			for c := 1; c < cols[0].ini; c++ {
				if tmp[c] {
					cabe = true
					break
				}
			}
		}
		for j := 0; j+1 < len(cols) && cabe; j++ {
			cabe = false
			for c := cols[j].fim; c < cols[j+1].ini; c++ {
				if tmp[c] {
					cabe = true
					break
				}
			}
		}
		if !cabe {
			break
		}
		livre, n = tmp, i+1
		if i == 1 {
			// a primeira linha de dados diz se há coluna de índice (pandas): algo escrito e,
			// depois, um corredor, tudo antes do primeiro nome do cabeçalho
			for c := 1; c < cols[0].ini && c < len(ls[1].rs); c++ {
				// dois espaços seguidos: um espaço só pode ser o de dentro de um nome
				if livre[c] && livre[c+1] && c+1 < cols[0].ini && strings.TrimSpace(string(ls[1].rs[:c])) != "" {
					temIndice = true
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
		temNome := false
		var outras []string // classes sensíveis que não são de nome
		for _, c := range cols {
			switch c.classe {
			case "nome", "nomecompleto":
				temNome = true
			case "":
			default:
				outras = append(outras, c.classe)
			}
		}
		classeNum := ""
		if len(outras) > 0 {
			classeNum = outras[0]
		}
		// um valor que não é nome é marcado com a primeira classe sensível que o aceitar
		marcarOutro := func(a, b int) {
			for _, c := range outras {
				if tipo, ok := valorDoCampo(c, s[a:b]); ok {
					add(a, b, tipo)
					return
				}
			}
		}
		l := ls[1]
		letra := func(r rune) bool { return unicode.IsLetter(r) || r == '\'' || r == '-' || r == '.' }
		for i := 0; i < len(l.rs); {
			if l.rs[i] == ' ' {
				i++
				continue
			}
			j := i
			soLetras := true
			for j < len(l.rs) && l.rs[j] != ' ' {
				soLetras = soLetras && letra(l.rs[j])
				j++
			}
			if soLetras && temNome {
				for j+1 < len(l.rs) && l.rs[j] == ' ' && letra(l.rs[j+1]) { // junta as palavras seguintes
					k := j + 1
					for k < len(l.rs) && letra(l.rs[k]) {
						k++
					}
					if k < len(l.rs) && l.rs[k] != ' ' {
						break
					}
					j = k
				}
				// só o que tem cara de nome de gente (nome e sobrenome com maiúscula)
				marcar(s, l.ini+l.pos[i], l.ini+l.pos[j], "nomecompleto", add)
			} else if !soLetras && classeNum != "" && (i > 0 || cols[0].ini == 0) { // i > 0: não o índice da linha
				marcarOutro(l.ini+l.pos[i], l.ini+l.pos[j])
			}
			i = j
		}
		return min(len(s), l.ini+l.pos[len(l.rs)]+1)
	}
	{
		corredor := func(c int) (int, int) { // o corredor que contém a posição c: [a, b)
			a, b := c, c+1
			for a > 0 && livre[a-1] {
				a--
			}
			for b < larg && livre[b] {
				b++
			}
			return a, b
		}
		esq := make([]int, len(cols)) // limites de cada coluna, em caracteres: [esq, dir)
		dir := make([]int, len(cols))
		ok := true
		// antes da primeira coluna pode haver um índice (pandas): a coluna começa depois do
		// primeiro corredor que vem após algum caractere
		esq[0] = 0
		for c := 0; c < cols[0].ini; c++ {
			if livre[c] {
				_, b := corredor(c)
				if c == 0 || b <= cols[0].ini {
					esq[0] = b
				}
				if c > 0 {
					break
				}
				c = b
			}
		}
		if esq[0] > cols[0].ini {
			esq[0] = cols[0].ini
		}
		dir[len(cols)-1] = larg
		for j := 0; j+1 < len(cols) && ok; j++ {
			e, f := cols[j].fim, cols[j+1].ini-1 // logo depois de um nome, logo antes do seguinte
			switch {
			case livre[e] && livre[f]:
				a1, b1 := corredor(e)
				a2, b2 := corredor(f)
				if a1 == a2 { // um corredor só entre os dois nomes
					dir[j], esq[j+1] = a1, b1
				} else if cols[j].classe != "" { // há valores no meio: na dúvida, ficam com a coluna sensível
					dir[j], esq[j+1] = a2, b2
				} else {
					dir[j], esq[j+1] = a1, b1
				}
			case livre[e]: // valores da coluna seguinte passam à esquerda do nome dela (alinhados à direita)
				a, b := corredor(e)
				dir[j], esq[j+1] = a, b
			case livre[f]: // valores desta coluna passam à direita do nome dela (alinhados à esquerda)
				a, b := corredor(f)
				dir[j], esq[j+1] = a, b
			default: // procura um corredor no meio
				ok = false
				for c := e; c <= f; c++ {
					if livre[c] {
						a, b := corredor(c)
						dir[j], esq[j+1], ok = a, b, true
						break
					}
				}
			}
		}
		if !ok {
			return ini
		}
		for _, l := range ls[1:n] {
			if ehSeparadorMarkdown(string(l.rs)) {
				continue
			}
			for j, c := range cols {
				if c.classe == "" {
					continue
				}
				a, b := min(esq[j], len(l.rs)), min(dir[j], len(l.rs))
				for a < b && l.rs[a] == ' ' {
					a++
				}
				for b > a && l.rs[b-1] == ' ' {
					b--
				}
				if b > a {
					marcar(s, l.ini+l.pos[a], l.ini+l.pos[b], c.classe, add)
				}
			}
		}
		ult := ls[n-1]
		return min(len(s), ult.ini+ult.pos[len(ult.rs)]+1)
	}
}

// cabecalho: a linha é o cabeçalho de uma tabela com alguma coluna sensível? Devolve a
// classe de cada coluna e o separador. Rejeita cedo o que não tem cara de cabeçalho.
func (m *Masker) cabecalho(linha string) ([]string, byte) {
	if len(linha) < 5 || len(linha) > 4000 {
		return nil, 0
	}
	if cels := dividir(linha, 'P'); len(cels) >= 2 { // ('NOME', 'CPF') ou ['NOME', 'CPF']
		cs := make([]string, len(cels))
		sens := 0
		for i, c := range cels {
			a, b := aparar(linha, c[0], c[1])
			if b-a > 48 {
				return nil, 0
			}
			if cs[i] = m.classe(linha[a:b]); cs[i] != "" && cs[i] != "nomesolto" {
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
	if naoEhLinhaDeTabela(linha) {
		return nil, 0
	}
	// o separador é o mais frequente entre tab, "|", ";" e ","
	var n [4]int
	seps := [4]byte{'\t', '|', ';', ','}
	for i := 0; i < len(linha); i++ {
		switch linha[i] {
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
	melhor := -1
	for i := range n {
		if n[i] > 0 && (melhor < 0 || n[i] > n[melhor]) {
			melhor = i
		}
	}
	if melhor < 0 {
		return nil, 0
	}
	cels := celulas(linha, seps[melhor])
	if len(cels) < 2 {
		return nil, 0
	}
	cs := make([]string, len(cels))
	sens := 0
	for i, c := range cels {
		a, b := aparar(linha, c[0], c[1])
		cel := linha[a:b]
		if len(cel) > 48 || strings.Contains(cel, ":") || strings.Contains(cel, "//") {
			return nil, 0
		}
		if cs[i] = m.classe(cel); cs[i] != "" && cs[i] != "nomesolto" {
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
	return cs, seps[melhor]
}

// celulas divide a linha pelo separador, respeitando aspas duplas; devolve [ini,fim) de cada célula.
func celulas(linha string, sep byte) [][2]int {
	if strings.IndexByte(linha, sep) < 0 {
		return nil
	}
	var out [][2]int
	ini, aspas := 0, false
	for i := 0; i < len(linha); i++ {
		switch {
		case linha[i] == '"':
			aspas = !aspas
		case linha[i] == sep && !aspas:
			out = append(out, [2]int{ini, i})
			ini = i + 1
		}
	}
	out = append(out, [2]int{ini, len(linha)})
	if sep == '|' && len(out) >= 3 { // "| a | b |": as pontas vazias não são colunas
		if strings.TrimSpace(linha[out[0][0]:out[0][1]]) == "" {
			out = out[1:]
		}
		if n := len(out); n > 0 && strings.TrimSpace(linha[out[n-1][0]:out[n-1][1]]) == "" {
			out = out[:n-1]
		}
	}
	return out
}

func aparar(linha string, a, b int) (int, int) {
	for a < b && (linha[a] == ' ' || linha[a] == '\t' || linha[a] == '"' || linha[a] == '\'' || linha[a] == '\r') {
		a++
	}
	for b > a && (linha[b-1] == ' ' || linha[b-1] == '\t' || linha[b-1] == '"' || linha[b-1] == '\'' || linha[b-1] == '\r') {
		b--
	}
	return a, b
}

// ehSeparadorMarkdown: linha só de traços, como a que separa o cabeçalho numa tabela
// markdown ("|---|:-:|") ou de terminal ("+----+----+", "-----+-----", "=====").
func ehSeparadorMarkdown(linha string) bool {
	t := strings.TrimSpace(linha)
	return len(t) >= 3 && strings.Trim(t, "|-:+= ") == ""
}

// identificaPessoa: tipos de achado que, numa linha de tabela, mostram que a linha é de uma
// pessoa.
var identificaPessoa = map[string]bool{"cpf": true, "email": true, "telefone": true, "rg": true, "cnh": true, "pis": true, "nascimento": true}

// nomesNaLinha: numa linha de tabela sem cabeçalho (colada no chat, um pedaço de CSV) que já
// tem um CPF, e-mail ou telefone detectado, a célula com cara de nome completo é o nome da
// pessoa. "Maria Lopes | 529.982.247-25 | maria@x.com" -> "Maria Lopes" também é mascarado.
func nomesNaLinha(s string, achados []Achado) []Achado {
	linhas := map[int]bool{} // começo das linhas que têm um identificador de pessoa
	for _, a := range achados {
		if identificaPessoa[a.Tipo] {
			// linha de mais de 2000 caracteres não é linha de tabela: a busca para aí
			a0 := max(0, a.Ini-2001)
			k := strings.LastIndexByte(s[a0:a.Ini], '\n')
			if k < 0 && a0 > 0 {
				continue
			}
			linhas[a0+k+1] = true
		}
	}
	var out []Achado
	for ini := range linhas {
		z := min(len(s), ini+2001)
		fim := strings.IndexByte(s[ini:z], '\n')
		if fim < 0 {
			fim = z - ini
		}
		l := s[ini : ini+fim]
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
		for _, c := range celulas(l, sep) {
			a, b := aparar(l, c[0], c[1])
			if _, ok := valorDoCampo("nomecompleto", l[a:b]); ok && len(strings.Fields(l[a:b])) >= 2 {
				out = append(out, Achado{ini + a, ini + b, "nome", l[a:b]})
			}
		}
	}
	return out
}
