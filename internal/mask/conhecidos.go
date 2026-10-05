package mask

import (
	"regexp"
	"strings"
	"sync"
)

// Valores já mascarados uma vez são reconhecidos depois em qualquer lugar (memória em RAM).

// Valores conhecidos: tudo que foi mascarado por um detector que depende de CONTEXTO
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

var contextuais = map[string]bool{"segredo": true, "rg": true, "cnh": true, "pis": true, "cep": true, "conta": true,
	"pix": true, "nascimento": true, "telefone": true, "cartao": true, "cpf": true, "cnpj": true,
	"usuario": true, "doc": true}

// paraCadaCodigo chama fn para cada "palavra" de s que pode ser um código de usuário ou de
// documento: de 3 a 40 letras, dígitos, "_", "." ou "-", começando por letra ou dígito.

func paraCadaCodigo(s string, fn func(ini, fim int)) {
	for i := 0; i < len(s); {
		if !ehAlnum(s[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && (ehAlnum(s[j]) || s[j] == '_' || s[j] == '.' || s[j] == '-') {
			j++
		}
		fim := j
		for fim > i && !ehAlnum(s[fim-1]) {
			fim-- // pontuação do fim da frase não faz parte
		}
		if n := fim - i; n >= 3 && n <= 40 {
			fn(i, fim)
		}
		i = j
	}
}

// chaveCanonica: forma do valor usada para comparar independentemente do tipo e da
// pontuação ("12.345.678-9" e "123456789" são o mesmo número).
func chaveCanonica(tipo, real string) string {
	if ehObjeto(tipo) {
		return canonObj(strings.TrimPrefix(tipo, prefTipoObj), real)
	}
	switch tipo {
	case "segredo":
		return "s:" + real
	case "pix":
		return "u:" + strings.ToLower(real)
	case "usuario", "doc":
		return "c:" + strings.ToUpper(real)
	case "nome":
		return "p:" + NormNome(real)
	}
	return "n:" + canonNum(real)
}

func canonNum(v string) string {
	d := soDigitos(v)
	if strings.HasSuffix(strings.ToLower(v), "x") {
		d += "x"
	}
	return d
}

// conhecidos é a memória em RAM. A busca no texto não percorre a lista de valores: usa um
// índice pelo começo de cada valor (os 4 primeiros bytes), como fazem os buscadores de
// vários padrões (prefixo + conferência). O custo depende do tamanho do texto, não de
// quantos valores há. Tem teto: passou de maxConhecidos, a metade mais antiga sai da RAM
// (o hash dela continua em disco).
type conhecidos struct {
	mu    sync.RWMutex
	base  int               // quantos valores já saíram da RAM (mantém a "geração" crescente)
	reais []string          // em ordem de chegada; geração do valor = base + índice
	tipo  map[string]string // valor exato -> tipo
	seq   map[string]int    // valor exato -> geração em que foi aprendido
	canon map[string]string // chave canônica -> tipo
	// chave canônica -> geração em que foi aprendida. Serve para refazer um texto memorizado
	// em que o valor apareceu em OUTRA grafia (RG sem pontos, código em minúsculas).
	canonSeq map[string]int
	nTipos   map[string]int
	nObj     int // quantos nomes de objeto (ver objetos.go)
	// 4 primeiros bytes -> TAMANHOS dos valores que começam assim. Com o começo e o tamanho,
	// o trecho do texto é consultado direto em seq (uma busca em mapa). Guardar a lista de
	// valores aqui ficaria lento quando milhares começam igual (RGs "12.3...", CPFs em sequência).
	pref map[uint32][]int32
	par  [1 << 16 / 64]uint64 // filtro rápido: há algum valor começando com estes 2 bytes?
}

const maxConhecidos = 50_000

func novosConhecidos() *conhecidos {
	c := &conhecidos{}
	c.zerar()
	return c
}

func (c *conhecidos) zerar() {
	c.reais = nil
	c.tipo, c.seq, c.canon, c.canonSeq = map[string]string{}, map[string]int{}, map[string]string{}, map[string]int{}
	c.nTipos, c.pref, c.nObj = map[string]int{}, map[uint32][]int32{}, 0
	c.par = [1 << 16 / 64]uint64{}
}

func (c *conhecidos) geracao() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base + len(c.reais)
}

func (c *conhecidos) inserir(tipo, real string) {
	c.tipo[real] = tipo
	c.seq[real] = c.base + len(c.reais)
	c.canon[chaveCanonica(tipo, real)] = tipo
	c.canonSeq[chaveCanonica(tipo, real)] = c.base + len(c.reais)
	c.reais = append(c.reais, real)
	c.nTipos[tipo]++
	if ehObjeto(tipo) {
		c.nObj++
	}
	k := uint32(real[0])<<24 | uint32(real[1])<<16 | uint32(real[2])<<8 | uint32(real[3])
	tem := false
	for _, n := range c.pref[k] {
		if int(n) == len(real) {
			tem = true
			break
		}
	}
	if !tem {
		c.pref[k] = append(c.pref[k], int32(len(real)))
	}
	c.par[k>>22] |= 1 << (k >> 16 & 63)
}

func (c *conhecidos) aprender(tipo, real string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tipo[real]; ok {
		return false
	}
	if len(c.reais) >= maxConhecidos { // teto: fica a metade mais recente, em mapas novos
		ficam := append([]string(nil), c.reais[len(c.reais)/2:]...)
		tipos := make([]string, len(ficam))
		for i, v := range ficam {
			tipos[i] = c.tipo[v]
		}
		c.base += len(c.reais) - len(ficam)
		c.zerar()
		for i, v := range ficam {
			c.inserir(tipos[i], v)
		}
	}
	c.inserir(tipo, real)
	return true
}

// varrer chama fn para cada ocorrência, em s, de um valor aprendido a partir da geração
// desde. fn devolve false para parar.
func (c *conhecidos) varrer(s string, desde int, fn func(ini, fim int, tipo string) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.reais) == 0 {
		return
	}
	for i := 0; i+4 <= len(s); i++ {
		k2 := uint32(s[i])<<8 | uint32(s[i+1])
		if c.par[k2>>6]&(1<<(k2&63)) == 0 {
			continue
		}
		for _, n := range c.pref[k2<<16|uint32(s[i+2])<<8|uint32(s[i+3])] {
			fim := i + int(n)
			if fim > len(s) {
				continue
			}
			v := s[i:fim]
			g, ok := c.seq[v]
			if !ok || g < desde {
				continue
			}
			if ehAlnum(v[0]) && i > 0 && ehAlnum(s[i-1]) {
				continue // pedaço de outra palavra/número
			}
			if ehAlnum(v[len(v)-1]) && fim < len(s) && ehAlnum(s[fim]) {
				continue
			}
			if tp := c.tipo[v]; ehObjeto(tp) && (i > 0 && ehIdent(s[i-1]) || fim < len(s) && ehIdent(s[fim])) {
				continue // nome de objeto só vale inteiro ("tb_x" não é pedaço de "tb_x_hist")
			}
			if !fn(i, fim, c.tipo[v]) {
				return
			}
		}
	}
}

// contemDesde diz se s contém algum valor aprendido a partir da geração g (ou se já não dá
// para saber, porque valores daquela época saíram da RAM).
func (c *conhecidos) contemDesde(s string, g int) bool {
	c.mu.RLock()
	perdeu := g < c.base
	c.mu.RUnlock()
	if perdeu {
		return true
	}
	achou := false
	c.varrer(s, g, func(int, int, string) bool { achou = true; return false })
	if achou {
		return true
	}
	// a mesma coisa em outra grafia: o número sem pontuação, o código em outra caixa
	c.mu.RLock()
	defer c.mu.RUnlock()
	novo := func(chave string) bool { gen, ok := c.canonSeq[chave]; return ok && gen >= g }
	for _, ix := range reNumTok.FindAllStringIndex(s, -1) {
		if v := s[ix[0]:ix[1]]; len(soDigitos(v)) >= 8 && novo("n:"+canonNum(v)) {
			return true
		}
	}
	for _, ix := range rePix.FindAllStringIndex(s, -1) {
		if novo("u:" + strings.ToLower(s[ix[0]:ix[1]])) {
			return true
		}
	}
	paraCadaCodigo(s, func(ini, fim int) {
		if !achou && novo("c:"+strings.ToUpper(s[ini:fim])) {
			achou = true
		}
	})
	if !achou && c.nObj > 0 {
		tokensObj(s, func(a, b int) {
			if v := s[a:b]; !achou && (novo("O:"+v) || novo("o:"+strings.ToLower(v))) {
				achou = true
			}
		})
		if achou {
			return true
		}
	}
	return achou
}

func temDigito(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// ehIdent: caractere que continua um identificador (letra, dígito, _ $ # -).
func ehIdent(b byte) bool { return ehAlnum(b) || b == '_' || b == '$' || b == '#' || b == '-' }

func ehAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// separadores em que uma senha costuma estar grudada; maxCortes limita o trabalho por palavra
const (
	sepSenha  = "=&:/?@|#+"
	maxCortes = 17
)

var (
	reNumTok   = regexp.MustCompile(`\d[\d.\-/]*[\dxX]`)
	reTokenSeg = regexp.MustCompile("[^\\s\"'`,;()\\[\\]{}<>]{8,}")
)

// conhecidos acrescenta a out as ocorrências, em s, de valores já conhecidos.
func (m *Masker) acharConhecidos(s string, numLongo bool, add0 func(ini, fim int, tipo string)) {
	c := m.conh
	add := func(ini, fim int, tipo string) {
		if ehObjeto(tipo) && !m.objPropaga(strings.TrimPrefix(tipo, prefTipoObj)) {
			return // tipo desligado depois de aprendido
		}
		add0(ini, fim, tipo)
	}
	m.acharObjetosConhecidos(s, add)

	// 1) valores em RAM, pelo texto exato (pega qualquer formato já visto)
	c.varrer(s, 0, func(ini, fim int, tipo string) bool { add(ini, fim, tipo); return true })

	// Achado pelo hash: o valor real está aqui no texto, então volta para a memória. Assim
	// a busca pelo texto exato (1) passa a pegá-lo também grudado em outra coisa.
	achei := func(ini, fim int, tp string) {
		add(ini, fim, tp)
		if fim-ini >= 4 {
			c.aprender(tp, s[ini:fim])
		}
	}

	// 2) candidatos pela forma canônica: mesmo número com outra pontuação, e valores
	//    aprendidos antes de um reinício (em disco só há o hash)
	temHash := m.vistos != nil && !m.vistos.Vazio()
	c.mu.RLock()
	temCanon := len(c.canon) > 0
	nSeg := c.nTipos["segredo"]
	c.mu.RUnlock()
	if !temHash && !temCanon {
		return
	}
	olhar := func(chave string) (string, bool) {
		c.mu.RLock()
		tp, ok := c.canon[chave]
		c.mu.RUnlock()
		if ok {
			return tp, true
		}
		if temHash {
			return m.vistos.Tipo(m.p.ID("visto", chave))
		}
		return "", false
	}
	if numLongo || temHash || temCanon {
		for _, ix := range reNumTok.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			// só números longos: um número curto aprendido (agência "4321-0") não pode fazer
			// todo "43210" do texto virar dado sensível. Os curtos ficam com a busca literal.
			if !bordaDig(s, ix[0], ix[1]) || len(soDigitos(v)) < 8 {
				continue
			}
			if tp, ok := olhar("n:" + canonNum(v)); ok {
				achei(ix[0], ix[1], tp)
			}
		}
		for _, ix := range reTel.FindAllStringIndex(s, -1) { // telefones com espaço/parênteses
			if tp, ok := olhar("n:" + canonNum(s[ix[0]:ix[1]])); ok && bordaDig(s, ix[0], ix[1]) {
				achei(ix[0], ix[1], tp)
			}
		}
	}
	if strings.Contains(s, "-") {
		for _, ix := range rePix.FindAllStringIndex(s, -1) {
			if tp, ok := olhar("u:" + strings.ToLower(s[ix[0]:ix[1]])); ok {
				achei(ix[0], ix[1], tp)
			}
		}
	}
	// códigos de usuário e documentos aprendidos (em qualquer caixa, e depois de um reinício)
	c.mu.RLock()
	nCod := c.nTipos["usuario"] + c.nTipos["doc"]
	c.mu.RUnlock()
	if nCod > 0 || (temHash && (m.vistos.Tem("usuario") || m.vistos.Tem("doc"))) {
		paraCadaCodigo(s, func(ini, fim int) {
			v := s[ini:fim]
			if !temDigito(v) || len(v) < 5 {
				return
			}
			if tp, ok := olhar("c:" + strings.ToUpper(v)); ok {
				achei(ini, fim, tp)
			}
		})
	}
	// segredos de antes de um reinício: só o hash existe, então testa cada "palavra" longa
	if temHash && (m.vistos.Tem("segredo") || nSeg > 0) {
		for _, ix := range reTokenSeg.FindAllStringIndex(s, -1) {
			v := s[ix[0]:ix[1]]
			c.mu.RLock()
			_, jaSei := c.tipo[v]
			c.mu.RUnlock()
			if jaSei {
				continue
			}
			if m.vistos.TemTamanho(len(v)) {
				if tp, ok := m.vistos.Tipo(m.p.ID("visto", "s:"+v)); ok {
					achei(ix[0], ix[1], tp)
					continue
				}
			}
			// Senha grudada em outra coisa ("k=Senha@1&b=2", "user:Senha@1@host"): com o hash
			// só dá para perguntar por um pedaço exato, então testa os pedaços entre
			// separadores. A senha pode conter separadores, por isso todos os intervalos
			// (de um corte a outro), e não só os pedaços mínimos.
			cortes := []int{0}
			for i := 0; i < len(v) && len(cortes) < maxCortes; i++ {
				if strings.IndexByte(sepSenha, v[i]) >= 0 {
					cortes = append(cortes, i, i+1) // antes e depois do separador
				}
			}
			if len(cortes) == 1 {
				continue
			}
			cortes = append(cortes, len(v))
		pedacos:
			for a := 0; a < len(cortes); a++ {
				for b := len(cortes) - 1; b > a; b-- {
					ini, fim := cortes[a], cortes[b]
					// só pedaços do tamanho de alguma senha conhecida: evita calcular o hash
					// de quase todos (o cálculo do hash é a parte cara)
					if fim-ini < 8 || (ini == 0 && fim == len(v)) || !m.vistos.TemTamanho(fim-ini) {
						continue
					}
					if tp, ok := m.vistos.Tipo(m.p.ID("visto", "s:"+v[ini:fim])); ok {
						achei(ix[0]+ini, ix[0]+fim, tp)
						break pedacos
					}
				}
			}
		}
	}
}

// aprender registra um valor contextual em RAM e (só o hash) em disco.
func (m *Masker) aprender(tipo, real string) {
	if tipo == "nome" {
		// Nome achado pelo rótulo do campo ("NOM_CLIENTE"): a pessoa entra no registro, como
		// num importar-pessoas, e passa a ser reconhecida em qualquer lugar e grafia.
		if m.pessoas != nil && len(strings.Fields(real)) >= 2 && len(real) <= 80 && m.pessoas.PID(m.p, "nome", NormNome(real)) == "" {
			m.pessoas.Importar(m.p, real, "", "")
			m.conh.aprender("nome", real) // faz os textos já memorizados com esse nome serem refeitos
		}
		return
	}
	if !contextuais[tipo] || len(real) < 4 {
		return
	}
	// código curto ou sem dígito ("jsilva", "ana") só é mascarado junto do rótulo: lembrá-lo
	// faria toda palavra igual virar dado sensível
	if (tipo == "usuario" || tipo == "doc") && (len(real) < 5 || !temDigito(real)) {
		return
	}
	if m.conh.aprender(tipo, real) && m.vistos != nil {
		if tipo == "segredo" {
			m.vistos.MarcarTamanho(len(real))
		}
		m.vistos.Marcar(m.p.ID("visto", chaveCanonica(tipo, real)), tipo)
	}
}
