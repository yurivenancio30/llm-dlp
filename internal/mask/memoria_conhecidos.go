package mask

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Values já mascarados uma vez são reconhecidos depois em qualquer lugar (memória em RAM).

// Values conhecidos: tudo que foi mascarado por um detector que depende de CONTEXTO
// (a palavra "rg" por perto, "password:" antes da senha...) precisa ser reconhecido
// depois em qualquer lugar. Senão acontece isto:
//
//	você:    "RG: 12.345.678-9"            -> mascarado (tem a palavra RG)
//	modelo:  "o documento RG-abc está ok"  -> você vê "o documento 12.345.678-9 está ok"
//	próxima mensagem: o histórico volta com "o documento 12.345.678-9 está ok"
//	                  -> sem a palavra RG por perto, o número passaria sem máscara
//
// Duas memórias:
//   - em RAM, o valor exato (busca literal, pega qualquer formato já visto);
//   - em disco, só o hash com a chave (para sobreviver a reinícios sem guardar o valor).

var contextual = map[string]bool{"segredo": true, "rg": true, "cnh": true, "pis": true, "cep": true, "conta": true,
	"pix": true, "nascimento": true, "telefone": true, "cartao": true, "cpf": true, "cnpj": true,
	"usuario": true, "doc": true}

// forEachCode chama fn para cada "palavra" de s que pode ser um código de usuário ou de
// documento: de 3 a 40 letras, dígitos, "_", "." ou "-", começando por letra ou dígito.

func forEachCode(s string, fn func(ini, end int)) {
	for i := 0; i < len(s); {
		if !isAlnum(s[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && (isAlnum(s[j]) || s[j] == '_' || s[j] == '.' || s[j] == '-') {
			j++
		}
		end := j
		for end > i && !isAlnum(s[end-1]) {
			end-- // pontuação do fim da frase não faz parte
		}
		if n := end - i; n >= 3 && n <= 40 {
			fn(i, end)
		}
		i = j
	}
}

// canonicalKey: forma do valor usada para comparar independentemente do tipo e da
// pontuação ("12.345.678-9" e "123456789" são o mesmo número).
func canonicalKey(typ, real string) string {
	if isObject(typ) {
		return canonObj(strings.TrimPrefix(typ, objTypePrefix), real)
	}
	switch typ {
	case "segredo":
		return "s:" + real
	case "pix":
		return "u:" + strings.ToLower(real)
	case "usuario", "doc":
		return "c:" + strings.ToUpper(real)
	case "nome":
		return "p:" + NormName(real)
	}
	return "n:" + canonNum(real)
}

func canonNum(v string) string {
	d := digitsOnly(v)
	if strings.HasSuffix(strings.ToLower(v), "x") {
		d += "x"
	}
	return d
}

// conhecidos é a memória em RAM. A busca no texto não percorre a lista de valores: usa um
// índice pelo começo de cada valor (os 4 primeiros bytes), como fazem os buscadores de
// vários padrões (prefixo + conferência). O custo depende do tamanho do texto, não de
// quantos valores há. Has teto: passou de maxKnown, a metade mais antiga sai da RAM
// (o hash dela continua em disco).
type knownSet struct {
	mu    sync.RWMutex
	base  int               // quantos valores já saíram da RAM (mantém a "geração" crescente)
	reals []string          // em ordem de chegada; geração do valor = base + índice
	typ   map[string]string // valor exato -> tipo
	seq   map[string]int    // valor exato -> geração em que foi aprendido
	canon map[string]string // chave canônica -> tipo
	// chave canônica -> geração em que foi aprendida. Serve para refazer um texto memorizado
	// em que o valor apareceu em OUTRA grafia (RG sem pontos, código em minúsculas).
	canonSeq map[string]int
	nTypes   map[string]int
	nObj     int // quantos nomes de objeto (ver objetos.go)
	// 4 primeiros bytes -> TAMANHOS dos valores que começam assim. Com o começo e o tamanho,
	// o trecho do texto é consultado direto em seq (uma busca em mapa). Guardar a lista de
	// valores aqui ficaria lento quando milhares começam igual (RGs "12.3...", CPFs em sequência).
	pfx  map[uint32][]int32
	pair [1 << 16 / 64]uint64 // filtro rápido: há algum valor começando com estes 2 bytes?
}

const maxKnown = 50_000

func newKnown() *knownSet {
	c := &knownSet{}
	c.reset()
	return c
}

func (c *knownSet) reset() {
	c.reals = nil
	c.typ, c.seq, c.canon, c.canonSeq = map[string]string{}, map[string]int{}, map[string]string{}, map[string]int{}
	c.nTypes, c.pfx, c.nObj = map[string]int{}, map[uint32][]int32{}, 0
	c.pair = [1 << 16 / 64]uint64{}
}

func (c *knownSet) generation() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base + len(c.reals)
}

func (c *knownSet) insert(typ, real string) {
	c.typ[real] = typ
	c.seq[real] = c.base + len(c.reals)
	c.canon[canonicalKey(typ, real)] = typ
	c.canonSeq[canonicalKey(typ, real)] = c.base + len(c.reals)
	c.reals = append(c.reals, real)
	c.nTypes[typ]++
	if isObject(typ) {
		c.nObj++
	}
	k := uint32(real[0])<<24 | uint32(real[1])<<16 | uint32(real[2])<<8 | uint32(real[3])
	has := false
	for _, n := range c.pfx[k] {
		if int(n) == len(real) {
			has = true
			break
		}
	}
	if !has {
		c.pfx[k] = append(c.pfx[k], int32(len(real)))
	}
	c.pair[k>>22] |= 1 << (k >> 16 & 63)
}

func (c *knownSet) learnIt(typ, real string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.typ[real]; ok {
		return false
	}
	if len(c.reals) >= maxKnown { // teto: fica a metade mais recente, em mapas novos
		stayOnes := append([]string(nil), c.reals[len(c.reals)/2:]...)
		types := make([]string, len(stayOnes))
		for i, v := range stayOnes {
			types[i] = c.typ[v]
		}
		c.base += len(c.reals) - len(stayOnes)
		c.reset()
		for i, v := range stayOnes {
			c.insert(types[i], v)
		}
	}
	c.insert(typ, real)
	return true
}

// varrer chama fn para cada ocorrência, em s, de um valor aprendido a partir da geração
// desde. fn devolve false para parar.
func (c *knownSet) scan(s string, since int, fn func(ini, end int, typ string) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.reals) == 0 {
		return
	}
	for i := 0; i+4 <= len(s); i++ {
		k2 := uint32(s[i])<<8 | uint32(s[i+1])
		if c.pair[k2>>6]&(1<<(k2&63)) == 0 {
			continue
		}
		for _, n := range c.pfx[k2<<16|uint32(s[i+2])<<8|uint32(s[i+3])] {
			end := i + int(n)
			if end > len(s) {
				continue
			}
			v := s[i:end]
			g, ok := c.seq[v]
			if !ok || g < since {
				continue
			}
			if isAlnum(v[0]) && i > 0 && isAlnum(s[i-1]) {
				continue // pedaço de outra palavra/número
			}
			if isAlnum(v[len(v)-1]) && end < len(s) && isAlnum(s[end]) {
				continue
			}
			if tp := c.typ[v]; isObject(tp) && (i > 0 && (isIdent(s[i-1]) || utf8LetterBefore(s, i)) || end < len(s) && (isIdent(s[end]) || utf8Letter(s, end) > 0)) {
				continue // nome de objeto só vale inteiro ("tb_x" não é pedaço de "tb_x_hist")
			}
			if !fn(i, end, c.typ[v]) {
				return
			}
		}
	}
}

// containsSince diz se s contém algum valor aprendido a partir da geração g (ou se já não dá
// para saber, porque valores daquela época saíram da RAM).
func (c *knownSet) containsSince(s string, g int) bool {
	c.mu.RLock()
	lost := g < c.base
	c.mu.RUnlock()
	if lost {
		return true
	}
	foundIt := false
	c.scan(s, g, func(int, int, string) bool { foundIt = true; return false })
	if foundIt {
		return true
	}
	// a mesma coisa em outra grafia: o número sem pontuação, o código em outra caixa
	c.mu.RLock()
	defer c.mu.RUnlock()
	newOne := func(key string) bool { gen, ok := c.canonSeq[key]; return ok && gen >= g }
	for _, ix := range reNumTok.FindAllStringIndex(s, -1) {
		if v := s[ix[0]:ix[1]]; len(digitsOnly(v)) >= 8 && newOne("n:"+canonNum(v)) {
			return true
		}
	}
	for _, ix := range rePix.FindAllStringIndex(s, -1) {
		if newOne("u:" + strings.ToLower(s[ix[0]:ix[1]])) {
			return true
		}
	}
	forEachCode(s, func(ini, end int) {
		if !foundIt && newOne("c:"+strings.ToUpper(s[ini:end])) {
			foundIt = true
		}
	})
	if !foundIt && c.nObj > 0 {
		objTokens(s, func(a, b int) {
			for _, f := range dottedShapes(s, a, b) { // também com ponto ("top_x.eventos")
				if v := s[a:f]; !foundIt && (newOne("O:"+v) || newOne("o:"+strings.ToLower(v))) {
					foundIt = true
				}
			}
		})
		if foundIt {
			return true
		}
	}
	return foundIt
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// isIdent: caractere que continua um identificador (letra, dígito, _ $ # -).
func isIdent(b byte) bool { return isAlnum(b) || b == '_' || b == '$' || b == '#' || b == '-' }

// utf8Letter: se s[i] começa uma letra fora do ASCII (á, ç, ñ...), o tamanho dela em bytes;
// senão 0.
func utf8Letter(s string, i int) int {
	if s[i] < 0x80 {
		return 0
	}
	r, n := utf8.DecodeRuneInString(s[i:])
	if r == utf8.RuneError || !unicode.IsLetter(r) {
		return 0
	}
	return n
}

// utf8LetterBefore: o caractere que termina em s[i-1] é uma letra fora do ASCII?
func utf8LetterBefore(s string, i int) bool {
	if i == 0 || s[i-1] < 0x80 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r != utf8.RuneError && unicode.IsLetter(r)
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// lineWindow limita a busca do começo/fim da linha em volta de uma ocorrência. Sem limite,
// numa linha única longa (JSON minificado, resposta de API, HTML) cada ocorrência percorre a
// linha inteira e o custo vira quadrático.
const lineWindow = 1000

// lineStartJ: começo da linha de s[i], ou o começo da janela se a linha for mais longa.
func lineStartJ(s string, i int) int {
	a := max(0, i-lineWindow)
	return a + strings.LastIndexByte(s[a:i], '\n') + 1
}

// lineEndJ: fim da linha de s[i] (o '\n' ou len(s)), ou o fim da janela.
func lineEndJ(s string, i int) int {
	z := min(len(s), i+lineWindow)
	if k := strings.IndexByte(s[i:z], '\n'); k >= 0 {
		return i + k
	}
	return z
}

// separadores em que uma senha costuma estar grudada; maxCuts limita o trabalho por palavra
const (
	passwordSep = "=&:/?@|#+"
	maxCuts     = 17
)

var (
	reNumTok   = regexp.MustCompile(`\d[\d.\-/]*[\dxX]`)
	reTokenSeg = regexp.MustCompile("[^\\s\"'`,;()\\[\\]{}<>]{8,}")
)

// conhecidos acrescenta a out as ocorrências, em s, de valores já conhecidos.
func (m *Masker) findKnown(s string, longNum bool, add0 func(ini, end int, typ string)) {
	c := m.known
	add := func(ini, end int, typ string) {
		if isObject(typ) {
			if !m.objPropagates(strings.TrimPrefix(typ, objTypePrefix)) {
				return // tipo desligado depois de aprendido
			}
			// o conhecimento acumulado (RAM, vistos.json) segue as regras do rastreamento:
			// palavra comum não se espalha por ele e vocabulário público nunca é aplicado
			// (um nome aprendido errado antes não volta em todo texto; ver memoria_rastreamento.go)
			if v := s[ini:end]; !identifierShape(v) || publicGeneral(v) {
				return
			}
		}
		add0(ini, end, typ)
	}
	m.findKnownObjects(s, add)

	// 1) valores em RAM, pelo texto exato (pega qualquer formato já visto)
	c.scan(s, 0, func(ini, end int, typ string) bool { add(ini, end, typ); return true })

	// Finding pelo hash: o valor real está aqui no texto, então volta para a memória. Assim
	// a busca pelo texto exato (1) passa a pegá-lo também grudado em outra coisa.
	gotIt := func(ini, end int, tp string) {
		add(ini, end, tp)
		if end-ini >= 4 {
			c.learnIt(tp, s[ini:end])
		}
	}

	// 2) candidatos pela forma canônica: mesmo número com outra pontuação, e valores
	//    aprendidos antes de um reinício (em disco só há o hash)
	hasHash := m.seenSet != nil && !m.seenSet.IsEmpty()
	c.mu.RLock()
	hasCanon := len(c.canon) > 0
	nSeg := c.nTypes["segredo"]
	c.mu.RUnlock()
	if !hasHash && !hasCanon {
		return
	}
	look := func(key string) (string, bool) {
		c.mu.RLock()
		tp, ok := c.canon[key]
		c.mu.RUnlock()
		if ok {
			return tp, true
		}
		if hasHash {
			return m.seenSet.Type(m.p.ID("visto", key))
		}
		return "", false
	}
	if longNum || hasHash || hasCanon {
		for _, ix := range reNumTok.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			// só números longos: um número curto aprendido (agência "4321-0") não pode fazer
			// todo "43210" do texto virar dado sensível. Os curtos ficam com a busca literal.
			if !digitEdge(s, ix[0], ix[1]) || len(digitsOnly(v)) < 8 {
				continue
			}
			if tp, ok := look("n:" + canonNum(v)); ok {
				gotIt(ix[0], ix[1], tp)
			}
		}
		for _, ix := range reTel.FindAllStringIndex(s, -1) { // telefones com espaço/parênteses
			if tp, ok := look("n:" + canonNum(s[ix[0]:ix[1]])); ok && digitEdge(s, ix[0], ix[1]) {
				gotIt(ix[0], ix[1], tp)
			}
		}
	}
	if strings.Contains(s, "-") {
		for _, ix := range rePix.FindAllStringIndex(s, -1) {
			if tp, ok := look("u:" + strings.ToLower(s[ix[0]:ix[1]])); ok {
				gotIt(ix[0], ix[1], tp)
			}
		}
	}
	// códigos de usuário e documentos aprendidos (em qualquer caixa, e depois de um reinício)
	c.mu.RLock()
	nCode := c.nTypes["usuario"] + c.nTypes["doc"]
	c.mu.RUnlock()
	if nCode > 0 || (hasHash && (m.seenSet.Has("usuario") || m.seenSet.Has("doc"))) {
		forEachCode(s, func(ini, end int) {
			v := s[ini:end]
			if !hasDigit(v) || len(v) < 5 {
				return
			}
			if tp, ok := look("c:" + strings.ToUpper(v)); ok {
				gotIt(ini, end, tp)
			}
		})
	}
	// segredos de antes de um reinício: só o hash existe, então testa cada "palavra" longa
	if hasHash && (m.seenSet.Has("segredo") || nSeg > 0) {
		for _, ix := range reTokenSeg.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			c.mu.RLock()
			_, alreadyKnown := c.typ[v]
			c.mu.RUnlock()
			if alreadyKnown {
				continue
			}
			if m.seenSet.HasSize(len(v)) {
				if tp, ok := m.seenSet.Type(m.p.ID("visto", "s:"+v)); ok {
					gotIt(ix[0], ix[1], tp)
					continue
				}
			}
			// Senha grudada em outra coisa ("k=Senha@1&b=2", "user:Senha@1@host"): com o hash
			// só dá para perguntar por um pedaço exato, então testa os pedaços entre
			// separadores. A senha pode conter separadores, por isso todos os intervalos
			// (de um corte a outro), e não só os pedaços mínimos.
			cuts := []int{0}
			for i := 0; i < len(v) && len(cuts) < maxCuts; i++ {
				if strings.IndexByte(passwordSep, v[i]) >= 0 {
					cuts = append(cuts, i, i+1) // antes e depois do separador
				}
			}
			if len(cuts) == 1 {
				continue
			}
			cuts = append(cuts, len(v))
		pieces:
			for a := 0; a < len(cuts); a++ {
				for b := len(cuts) - 1; b > a; b-- {
					ini, end := cuts[a], cuts[b]
					// só pedaços do tamanho de alguma senha conhecida: evita calcular o hash
					// de quase todos (o cálculo do hash é a parte cara)
					if end-ini < 8 || (ini == 0 && end == len(v)) || !m.seenSet.HasSize(end-ini) {
						continue
					}
					if tp, ok := m.seenSet.Type(m.p.ID("visto", "s:"+v[ini:end])); ok {
						gotIt(ix[0]+ini, ix[0]+end, tp)
						break pieces
					}
				}
			}
		}
	}
}

// aprender registra um valor contextual em RAM e (só o hash) em disco.
func (m *Masker) learnIt(typ, real string) {
	if typ == "nome" {
		// Name achado pelo rótulo do campo ("NOM_CLIENTE"): a pessoa entra no registro, como
		// num importar-pessoas, e passa a ser reconhecida em qualquer lugar e grafia.
		if m.people != nil && len(strings.Fields(real)) >= 2 && len(real) <= 80 && m.people.PID(m.p, "nome", NormName(real)) == "" {
			m.people.Import(m.p, real, "", "")
			m.known.learnIt("nome", real) // faz os textos já memorizados com esse nome serem refeitos
		}
		return
	}
	if !contextual[typ] || len(real) < 4 {
		return
	}
	// código curto ou sem dígito ("jsilva", "ana") só é mascarado junto do rótulo: lembrá-lo
	// faria toda palavra igual virar dado sensível
	if (typ == "usuario" || typ == "doc") && (len(real) < 5 || !hasDigit(real)) {
		return
	}
	if m.known.learnIt(typ, real) && m.seenSet != nil {
		if typ == "segredo" {
			m.seenSet.MarkSize(len(real))
		}
		m.seenSet.Mark(m.p.ID("visto", canonicalKey(typ, real)), typ)
	}
}
