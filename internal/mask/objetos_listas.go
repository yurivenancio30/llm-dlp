package mask

import (
	"regexp"
	"strings"
)

// Listas homogêneas (ver docs/pt-BR/estruturas.md, seção Saídas soltas de script e de shell): numa
// lista de nomes, se pelo menos metade dos itens já são nomes de um mesmo tipo (aprendidos
// antes ou achados pelos leitores neste texto), os outros itens com cara de identificador são
// do mesmo tipo. Não é um leitor: depende do que já se sabe, e por isso roda depois dos
// leitores, como a propagação. As formas de lista são só de formato:
//   - coluna solta: um item por linha (com marcador "- ", "* " e aspas/vírgula no fim);
//   - contagem + valor ("   12 t_x" de sort | uniq -c, "t_x    12" de value_counts);
//   - linha decorada de laço ("== t_x ==", "--- t_x", "## t_x"), agrupada pela decoração;
//   - lista entre vírgulas numa linha, com ou sem aspas e colchetes (json.dumps, print).
// E o nome qualificado em que uma parte já é conhecida: "fin.<x>" com fin aprendido como
// schema faz de <x> uma tabela.

const minLista = 3

var (
	// item de lista: identificador, talvez qualificado (sem espaço)
	reItemLista    = `["'\x60]?[A-Za-z_][\w$#\-]*(?:\.[A-Za-z_][\w$#\-]*){0,2}["'\x60]?`
	reLinhaItem    = regexp.MustCompile(`^[ \t]*(?:[-*•][ \t]+)?(` + reItemLista + `)[ \t]*[,;]?[ \t]*$`)
	reLinhaContEsq = regexp.MustCompile(`^[ \t]*\d+[ \t]+(` + reItemLista + `)[ \t]*$`)
	reLinhaContDir = regexp.MustCompile(`^[ \t]*(` + reItemLista + `)[ \t]{2,}\d+[ \t]*$`)
	reLinhaDecor   = regexp.MustCompile(`^[ \t]*(={2,}|-{2,}|#{1,6}|\*{2,}|>{2,}|\[)[ \t]*(` + reItemLista + `)[ \t]*(?:={2,}|-{2,}|\*{2,}|\])?[ \t]*:?[ \t]*$`)
	reItemInteiro  = regexp.MustCompile(`^` + reItemLista + `$`)
)

// itemLista: o nome de um item (sem aspas), nas posições de s.
func itemLista(s string, a, b int) celula {
	for a < b && (s[a] == '"' || s[a] == '\'' || s[a] == '`') {
		a++
	}
	for b > a && (s[b-1] == '"' || s[b-1] == '\'' || s[b-1] == '`') {
		b--
	}
	return celula{a, b}
}

// listasEm: os grupos de itens de s nas formas acima.
func listasEm(s string) [][]celula {
	var grupos [][]celula
	var corrida []celula
	forma := 0
	fechar := func() {
		if len(corrida) >= minLista {
			grupos = append(grupos, corrida)
		}
		corrida, forma = nil, 0
	}
	decor := map[string][]celula{}
	var ordemDecor []string
	for _, l := range quebraLinhas(s) {
		linha := s[l[0]:l[1]]
		if len(linha) > 260 || palavras(linha) > 3 { // nenhuma das formas tem mais de 3 palavras
			fechar()
			continue
		}
		if m := reLinhaDecor.FindStringSubmatchIndex(linha); m != nil {
			k := linha[m[2]:m[3]]
			if _, ok := decor[k]; !ok {
				ordemDecor = append(ordemDecor, k)
			}
			decor[k] = append(decor[k], itemLista(s, l[0]+m[4], l[0]+m[5]))
			fechar()
			continue
		}
		f, a, b := 0, -1, -1
		if m := reLinhaItem.FindStringSubmatchIndex(linha); m != nil {
			f, a, b = 1, m[2], m[3]
		} else if m := reLinhaContEsq.FindStringSubmatchIndex(linha); m != nil {
			f, a, b = 2, m[2], m[3]
		} else if m := reLinhaContDir.FindStringSubmatchIndex(linha); m != nil {
			f, a, b = 3, m[2], m[3]
		}
		if f == 0 || forma != 0 && f != forma {
			fechar()
		}
		if f == 0 {
			continue
		}
		forma = f
		corrida = append(corrida, itemLista(s, l[0]+a, l[0]+b))
	}
	fechar()
	for _, k := range ordemDecor {
		if len(decor[k]) >= minLista {
			grupos = append(grupos, decor[k])
		}
	}
	if strings.IndexByte(s, ',') >= 0 {
		for _, l := range quebraLinhas(s) {
			if strings.Count(s[l[0]:l[1]], ",") >= minLista-1 {
				grupos = append(grupos, listasVirgula(s, l[0], l[1])...)
			}
		}
	}
	return grupos
}

// listasVirgula: as listas entre vírgulas da linha [a, b): itens inteiros entre as vírgulas; o
// primeiro pode vir depois de um rótulo ("carregadas: a, b, c") e o último antes de um fecho.
func listasVirgula(s string, a, b int) [][]celula {
	var segs []celula
	for i := a; i <= b; i++ {
		if i == b || s[i] == ',' {
			c := celula{a, i}
			for c.a < c.b && (s[c.a] == ' ' || s[c.a] == '\t') {
				c.a++
			}
			for c.b > c.a && (s[c.b-1] == ' ' || s[c.b-1] == '\t') {
				c.b--
			}
			segs = append(segs, c)
			a = i + 1
		}
	}
	inteiro := func(c celula) bool { return c.b > c.a && c.b-c.a <= 200 && reItemInteiro.MatchString(s[c.a:c.b]) }
	borda := func(c celula, cauda bool) (celula, bool) { // a palavra do fim (ou do começo) do pedaço
		if cauda {
			k := c.b
			for k > c.a && !strings.ContainsRune(" \t[(:={", rune(s[k-1])) {
				k--
			}
			c.a = k
		} else {
			k := c.a
			for k < c.b && !strings.ContainsRune(" \t])};", rune(s[k])) {
				k++
			}
			c.b = k
		}
		return c, inteiro(c)
	}
	var out [][]celula
	for i := 0; i+1 < len(segs); {
		ini, ok := borda(segs[i], true)
		if !ok || chamadaAntes(s, ini.a) {
			i++
			continue
		}
		g := []celula{itemLista(s, ini.a, ini.b)}
		j := i + 1
		for j < len(segs) && inteiro(segs[j]) {
			g = append(g, itemLista(s, segs[j].a, segs[j].b))
			j++
		}
		if j < len(segs) {
			if fim, ok := borda(segs[j], false); ok {
				g = append(g, itemLista(s, fim.a, fim.b))
			}
		}
		if len(g) >= minLista {
			out = append(out, g)
		}
		i = j
	}
	return out
}

// palavras: quantas palavras separadas por espaço há em l (para de contar em 4).
func palavras(l string) int {
	n, dentro := 0, false
	for i := 0; i < len(l) && n < 4; i++ {
		b := l[i] == ' ' || l[i] == '\t'
		if !b && !dentro {
			n++
		}
		dentro = !b
	}
	return n
}

// chamadaAntes: a lista que começa em i está entre os parênteses de uma chamada ("f(a, b").
func chamadaAntes(s string, i int) bool {
	k := i - 1
	for k >= 0 && (s[k] == ' ' || s[k] == '\t') {
		k--
	}
	return k >= 1 && s[k] == '(' && (ehAlnum(s[k-1]) || s[k-1] == '_')
}

// tipoConhecido: o tipo de v se é um nome aprendido (em RAM ou, só o hash, no vistos.json).
func (m *Masker) tipoConhecido(v string) (string, bool) {
	c := m.conh
	c.mu.RLock()
	tp, ok := c.canon["O:"+v]
	if !ok {
		tp, ok = c.canon["o:"+strings.ToLower(v)]
		ok = ok && entSQL[strings.TrimPrefix(tp, prefTipoObj)]
	}
	c.mu.RUnlock()
	if ok {
		ent := strings.TrimPrefix(tp, prefTipoObj)
		return ent, ehObjeto(tp) && m.objPropaga(ent)
	}
	if m.vistos == nil || !m.vistos.TemObj() || !caraDeIdentificador(v) {
		return "", false
	}
	ent, visto, ok := m.vistos.Obj(m.idObj("O:" + v))
	if !ok {
		ent, visto, ok = m.vistos.Obj(m.idObj("o:" + strings.ToLower(v)))
		ok = ok && entSQL[ent]
	}
	if !ok || hoje()-visto > validadeObj || !m.objPropaga(ent) {
		return "", false
	}
	return ent, true
}

// publicoLista: o vocabulário que nunca vira item de lista.
func publicoLista(v string) bool { return publicoSQL(v) || publicoDev(v) || publicoDevops(v) }

// acharListas: as listas homogêneas e os nomes qualificados com uma parte conhecida (aprendida).
func (m *Masker) acharListas(s string, aprende bool, add func(ini, fim int, tipo string)) {
	if !m.cfg.Objetos.Ligado {
		return
	}
	c := m.conh
	c.mu.RLock()
	nRAM := c.nObj
	c.mu.RUnlock()
	if nRAM == 0 && (m.vistos == nil || !m.vistos.TemObj()) {
		return
	}
	// só o que já foi APRENDIDO (com os freios: evidência forte ou duas regras), e não qualquer
	// achado deste texto: um achado fraco errado puxaria a lista inteira junto
	conhecido := m.tipoConhecido
	m.rodarLeitor(Leitor{Nome: "lista", Publico: publicoLista, Achar: func(s string, add func(ObjAchado)) {
		listasHomogeneas(s, conhecido, add)
		qualificadosConhecidos(s, conhecido, add)
	}}, s, aprende, add)
}

// listasHomogeneas: em cada lista, o tipo da maioria conhecida vale para os itens que faltam.
func listasHomogeneas(s string, conhecido func(string) (string, bool), add func(ObjAchado)) {
	for _, g := range listasEm(s) {
		n := map[string]int{}
		sabe := make([]bool, len(g))
		for i, c := range g {
			v := s[c.a:c.b]
			if p := strings.LastIndexByte(v, '.'); p >= 0 {
				v = v[p+1:]
			}
			if e, ok := conhecido(v); ok {
				n[e]++
				sabe[i] = true
			}
		}
		tipo, max := "", 0
		for e, k := range n {
			if k > max || k == max && e < tipo {
				tipo, max = e, k
			}
		}
		if max*2 < len(g) {
			continue
		}
		for i, c := range g {
			if sabe[i] {
				continue
			}
			v := s[c.a:c.b]
			if !caraDeIdentificador(v) || ehTipoDado(v) || ehPseudoObj(v) {
				continue
			}
			ps := strings.Split(v, ".")
			if len(ps) > 1 && extensoesArquivo[strings.ToLower(ps[len(ps)-1])] {
				continue // arquivo.csv
			}
			ents := entQual(len(ps), tipo)
			a := c.a
			for k, p := range ps {
				if _, ok := conhecido(p); !ok && reIdentSimples.MatchString(p) {
					add(ObjAchado{a, a + len(p), ents[k], "lista", ents[k] != "coluna" && caraDeIdentificador(p)})
				}
				a += len(p) + 1
			}
		}
	}
}

// atribuiAtributo: o nome s[a:b] abre a linha e recebe uma atribuição ("mock.scalars.return_value
// = x", "self.cfg.schema = y"): é atributo de objeto no código, não nome de tabela.
func atribuiAtributo(s string, a, b int) bool {
	for k := a - 1; k >= 0 && s[k] != '\n'; k-- {
		if s[k] != ' ' && s[k] != '\t' {
			return false
		}
	}
	for b < len(s) && (s[b] == ' ' || s[b] == '\t') {
		b++
	}
	return b+1 < len(s) && s[b] == '=' && s[b+1] != '='
}

// qualificadosConhecidos: "a.b" ou "a.b.c" em que uma parte é um schema ou banco conhecido na
// posição em que fica num nome de tabela: as outras partes ganham o tipo da posição.
func qualificadosConhecidos(s string, conhecido func(string) (string, bool), add func(ObjAchado)) {
	if strings.IndexByte(s, '.') < 0 {
		return
	}
	ident := func(c byte) bool { return ehAlnum(c) || c == '_' || c == '$' }
	for i := 0; i < len(s); {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') || i > 0 && (ident(s[i-1]) || s[i-1] == '.' || s[i-1] == '/' || s[i-1] == '-') {
			i++
			continue
		}
		var ps [][2]int
		j := i
		for {
			k := j
			for k < len(s) && ident(s[k]) {
				k++
			}
			if k == j {
				break
			}
			ps = append(ps, [2]int{j, k})
			j = k
			if j+1 < len(s) && s[j] == '.' && (letraD(s[j+1]) || s[j+1] == '_') && len(ps) < 4 {
				j++
				continue
			}
			break
		}
		i = max(j, i+1)
		if len(ps) < 2 || len(ps) > 3 || j < len(s) && (s[j] == '(' || s[j] == '-' || s[j] == '/' || s[j] == '.' && j+1 < len(s) && ehAlnum(s[j+1])) {
			continue
		}
		if ult := strings.ToLower(s[ps[len(ps)-1][0]:ps[len(ps)-1][1]]); extensoesArquivo[ult] || tldsPublicos[ult] {
			continue
		}
		ents := entQual(len(ps), "tabela")
		casa := false
		var sabe [3]bool
		for k, p := range ps {
			if e, ok := conhecido(s[p[0]:p[1]]); ok {
				sabe[k] = true
				if (e == "schema" || e == "database") && e == ents[k] {
					casa = true
				}
			}
		}
		if !casa || atribuiAtributo(s, ps[0][0], j) {
			continue
		}
		for k, p := range ps {
			if v := s[p[0]:p[1]]; !sabe[k] && !ehPseudoObj(v) && len(v) >= 2 {
				add(ObjAchado{p[0], p[1], ents[k], "qualificado", caraDeIdentificador(v)})
			}
		}
	}
}
