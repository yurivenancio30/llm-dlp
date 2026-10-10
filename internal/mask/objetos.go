package mask

import (
	"crypto/sha256"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Objetos: nomes de recursos internos (servidor, banco, schema, tabela, coluna, procedure,
// índice, usuário, namespace, serviço, bucket, fila), reconhecidos pela estrutura do conteúdo
// (ver docs/pt-BR/estruturas.md). Este arquivo é a base comum: tipos, pseudônimo, aprendizado com
// freios e liga/desliga por tipo. Os leitores de cada formato entregam os achados por
// m.leitores.

// EntObjeto: tipo de entidade -> prefixo do pseudônimo.
var EntObjeto = map[string]string{"servidor": "HOST", "database": "DB", "schema": "SCH", "tabela": "T",
	"coluna": "C", "procedure": "PROC", "indice": "IDX", "usuario": "USR", "namespace": "NS",
	"servico": "SVC", "bucket": "BKT", "fila": "TOP",
	"repositorio": "REPO", "organizacao": "ORG", "pacote": "PKG", "pasta": "DIR", "conta_nuvem": "ACC"}

// propagaPadrao: tipos que, aprendidos, são mascarados também fora da posição estrutural.
// Coluna e índice não: nomes como user_id e created_at existem em todo código.
var propagaPadrao = map[string]bool{"servidor": true, "database": true, "schema": true, "tabela": true,
	"procedure": true, "usuario": true, "namespace": true, "servico": true, "bucket": true, "fila": true,
	"repositorio": true, "organizacao": true, "pacote": true, "pasta": true, "conta_nuvem": true}

const prefTipoObj = "obj." // o tipo interno de um objeto: "obj.tabela"

func ehObjeto(tipo string) bool { return strings.HasPrefix(tipo, prefTipoObj) }

// ObjAchado: um nome de objeto achado por um leitor de estrutura.
type ObjAchado struct {
	Ini, Fim int
	Ent      string // tipo de entidade (chave de EntObjeto)
	Regra    string // nome da regra, para contar e para a evidência "2 regras diferentes"
	Forte    bool   // posição inequívoca: ensina sozinho
}

// Leitor procura nomes de objeto em s, pela estrutura.
type Leitor struct {
	Nome    string
	Achar   func(s string, add func(ObjAchado))
	Publico func(v string) bool // vocabulário do formato: nunca é mascarado nem aprendido
}

// objMascara / objPropaga: liga/desliga por tipo (config "objetos").
func (m *Masker) objMascara(ent string) bool {
	o := m.cfg.Objetos
	if !o.Ligado {
		return false
	}
	if v, ok := o.Mascarar[ent]; ok {
		return v
	}
	return true
}

func (m *Masker) objPropaga(ent string) bool {
	if !m.objMascara(ent) {
		return false
	}
	if v, ok := m.cfg.Objetos.Propagar[ent]; ok {
		return v
	}
	return propagaPadrao[ent]
}

// entSQL: tipos insensíveis à caixa: os de SQL (identificadores sem aspas são insensíveis à
// caixa em todos os dialetos) e servidor (nome DNS não diferencia maiúsculas: "sqlprd01" e
// "SQLPRD01" são a mesma máquina). Nos outros tipos (caminho, bucket, fila...) a grafia vale
// como está: "/dados/Relatorios" e "/dados/relatorios" são duas pastas.
var entSQL = map[string]bool{"database": true, "schema": true, "tabela": true, "coluna": true, "procedure": true,
	"indice": true, "servidor": true}

// semCitacao: o nome sem os colchetes, aspas ou crases em volta.
func semCitacao(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		switch {
		case v[0] == '[' && v[len(v)-1] == ']', v[0] == '"' && v[len(v)-1] == '"', v[0] == '`' && v[len(v)-1] == '`':
			v = v[1 : len(v)-1]
		}
	}
	return v
}

// normObj: a forma do nome que identifica o objeto (sem citação; em minúsculas nos tipos de SQL).
func normObj(ent, v string) string {
	v = semCitacao(v)
	if entSQL[ent] {
		return strings.ToLower(v)
	}
	return v
}

// canonObj: a chave do nome na memória e no vistos.json. "o:" = sem caixa (SQL), "O:" = exata.
func canonObj(ent, v string) string {
	if entSQL[ent] {
		return "o:" + strings.ToLower(semCitacao(v))
	}
	return "O:" + semCitacao(v)
}

// pseudoObjeto: prefixo do tipo + o ID do HMAC da chave (o mesmo tamanho dos outros tipos).
// Estável entre conversas e reinícios. Nome em minúsculas recebe o prefixo em minúsculas
// ("t_..."), para combinar com o estilo do texto; em maiúsculas, o prefixo em maiúsculas. Nome
// de caixa mista (Pedido_Item) recebe o prefixo com só a primeira letra maiúscula e as letras
// do ID na caixa que um hash da própria grafia diz: o mesmo nome de SQL em duas grafias no
// mesmo texto ("ContAB" e "ContAb", que são o mesmo objeto) sai com dois pseudônimos, e a volta
// devolve cada grafia como estava.
func (m *Masker) pseudoObjeto(ent, real string) string {
	pref := EntObjeto[ent]
	if pref == "" {
		pref = "OBJ"
	}
	id := m.p.ID("objeto", ent+"\x00"+normObj(ent, real))
	switch {
	case strings.ToLower(real) == real:
		pref = strings.ToLower(pref)
	case strings.ToUpper(real) == real:
	default:
		pref = pref[:1] + strings.ToLower(pref[1:])
		h := sha256.Sum256([]byte(real))
		b := []byte(id)
		maius := false
		for i := range b {
			if b[i] >= 'a' && b[i] <= 'z' && h[i]&1 == 1 {
				b[i] -= 32
				maius = true
			}
		}
		for i := 0; !maius && i < len(b); i++ { // pelo menos uma: não confunde com a de maiúsculas
			if b[i] >= 'a' && b[i] <= 'z' {
				b[i] -= 32
				maius = true
			}
		}
		id = string(b)
	}
	ps := pref + "_" + id
	registrarPseudo(ps)
	return ps
}

// rePseudoObj: a FORMA de um pseudônimo de objeto. Só a forma não basta para pular um nome
// ("t_customer" é uma tabela real com essa cara): ver ehPseudoObj.
var rePseudoObj = regexp.MustCompile(`^(?i:host|db|sch|t|c|proc|idx|usr|ns|svc|bkt|top|repo|org|pkg|dir|acc|obj)_[a-zA-Z2-7]{8}$`)

// pseudônimos de objeto gerados neste processo (em minúsculas): só esses são pulados.
var (
	pseudosGerados sync.Map
	nPseudos       atomic.Int64
)

const maxPseudosGerados = 500_000

func registrarPseudo(ps string) {
	k := strings.ToLower(ps)
	if _, ok := pseudosGerados.LoadOrStore(k, true); !ok && nPseudos.Add(1) > maxPseudosGerados {
		pseudosGerados.Clear()
		nPseudos.Store(0)
	}
}

// ehPseudoObj: v é um pseudônimo de objeto gerado por nós (em qualquer caixa)?
func ehPseudoObj(v string) bool {
	if !rePseudoObj.MatchString(v) {
		return false
	}
	_, ok := pseudosGerados.Load(strings.ToLower(v))
	return ok
}

// caraDeIdentificador: o freio que separa um nome de recurso de uma palavra comum. Só nomes
// assim são aprendidos e propagados: têm "_", dígito, ponto, hífen entre partes ou mistura de
// caixa ("tb_pedido", "srv01", "app.config", "svc-pedidos", "contaCorrente"). Ficam de fora
// versões, UUIDs e hashes, que têm dígitos mas não são nomes.
func caraDeIdentificador(v string) bool {
	if len(v) < 3 || len(v) > 128 || temDigito(v) && reVersaoOuHash.MatchString(v) {
		return false
	}
	letra := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			letra = true
		}
	}
	if !letra {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '_' || c == '.' || c >= '0' && c <= '9':
			return true
		case c == '-' && i > 0 && i+1 < len(v) && ehAlnum(v[i-1]) && ehAlnum(v[i+1]):
			return true
		case c >= 'A' && c <= 'Z' && i > 0 && v[i-1] >= 'a' && v[i-1] <= 'z':
			return true
		}
	}
	return false
}

// versão (semver, regex oficial de semver.org sem âncoras internas), UUID e hash hexadecimal
var reVersaoOuHash = regexp.MustCompile(`^(?:v?(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:\.(?:0|[1-9]\d*))?(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]{16,})$`)

// fracos: evidência fraca, só em RAM: nome -> regras diferentes em que foi visto. Duas regras
// diferentes valem como evidência forte.
type fracos struct {
	mu     sync.Mutex
	regras map[string]int // nome da regra -> bit
	vistos map[string]uint64
}

const maxFracos = 200_000

func (f *fracos) marcar(nome, regra string) (duas bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.regras == nil {
		f.regras, f.vistos = map[string]int{}, map[string]uint64{}
	}
	b, ok := f.regras[regra]
	if !ok {
		if len(f.regras) >= 64 {
			b = 63
		} else {
			b = len(f.regras)
			f.regras[regra] = b
		}
	}
	if len(f.vistos) >= maxFracos {
		f.vistos = map[string]uint64{}
	}
	f.vistos[nome] |= 1 << uint(b)
	v := f.vistos[nome]
	return v&(v-1) != 0 // mais de um bit
}

// aprenderObj aplica os freios e, se passar, lembra o nome (RAM e, só o hash, vistos.json).
func (m *Masker) aprenderObj(o ObjAchado, real string, publico func(string) bool) {
	v := strings.Trim(strings.TrimSpace(real), "[]\"`")
	if len(v) < 4 || !m.objPropaga(o.Ent) || !caraDeIdentificador(v) || ehPseudoObj(v) {
		return
	}
	if publico != nil && publico(v) {
		return
	}
	if ent, ok := m.entAprendido(v); ok {
		o.Ent = ent // já aprendido: mantém o tipo (e o pseudônimo) da primeira vez
	}
	if !o.Forte && !m.fracos.marcar(canonObj(o.Ent, v), o.Regra) {
		return // evidência fraca: mascara no lugar, mas não ensina
	}
	m.conh.aprender(prefTipoObj+o.Ent, v)
	if m.vistos != nil {
		m.vistos.MarcarObj(m.idObj(canonObj(o.Ent, v)), o.Ent, hoje())
	}
}

// entAprendido: o tipo com que o nome v já foi aprendido (em RAM ou no vistos.json), se foi.
func (m *Masker) entAprendido(v string) (string, bool) {
	v = semCitacao(v)
	low := strings.ToLower(v)
	c := m.conh
	c.mu.RLock()
	tp, ok := c.canon["O:"+v]
	if !ok {
		tp, ok = c.canon["o:"+low]
		ok = ok && entSQL[strings.TrimPrefix(tp, prefTipoObj)]
	}
	c.mu.RUnlock()
	if ok && ehObjeto(tp) {
		return strings.TrimPrefix(tp, prefTipoObj), true
	}
	if m.vistos == nil || !m.vistos.TemObj() {
		return "", false
	}
	ent, visto, ok := m.vistos.Obj(m.idObj("O:" + v))
	if !ok {
		ent, visto, ok = m.vistos.Obj(m.idObj("o:" + low))
		ok = ok && entSQL[ent]
	}
	return ent, ok && hoje()-visto <= validadeObj
}

// unificarObjetos: o mesmo nome real fica com UM tipo (e, portanto, um pseudônimo) no texto,
// mesmo quando regras diferentes o acham com tipos diferentes ("dir_..." numa linha e
// "svc_..." noutra para a mesma pasta). Vale o tipo com que o nome já foi aprendido; se não
// foi, o da primeira ocorrência no texto (no mesmo trecho, a ordem do tipo, como em aplicarT).
func (m *Masker) unificarObjetos(out []Achado) {
	var ix []int
	for i, a := range out {
		if ehObjeto(a.Tipo) {
			ix = append(ix, i)
		}
	}
	if len(ix) < 2 && (len(ix) == 0 || m.conh.geracao() == 0 && (m.vistos == nil || !m.vistos.TemObj())) {
		return
	}
	sort.SliceStable(ix, func(i, j int) bool {
		a, b := out[ix[i]], out[ix[j]]
		if a.Ini != b.Ini {
			return a.Ini < b.Ini
		}
		return a.Tipo < b.Tipo
	})
	escolha := map[string]string{}
	for _, i := range ix {
		k := strings.ToLower(semCitacao(out[i].Real))
		t, ok := escolha[k]
		if !ok {
			t = out[i].Tipo
			if ent, ok := m.entAprendido(out[i].Real); ok && m.objMascara(ent) {
				t = prefTipoObj + ent
			}
			escolha[k] = t
		}
		out[i].Tipo = t
	}
}

// idObj: o hash com que o nome fica no vistos.json (o tipo vai no valor).
func (m *Masker) idObj(chave string) string { return m.p.ID("visto", chave) }

// hoje: dias desde 1970 (o vistos.json guarda datas assim). Variável para os testes.
var hoje = func() int { return int(time.Now().Unix() / 86400) }

// validadeObj: um nome não visto há mais que isso deixa de ser propagado.
const validadeObj = 90

// acharObjetos roda os leitores em s, devolve os achados (para mascarar) e ensina.
func (m *Masker) acharObjetos(s string, aprende bool, add func(ini, fim int, tipo string)) {
	if !m.cfg.Objetos.Ligado || len(m.leitores) == 0 {
		return
	}
	ct := contextoTexto(s)
	if len(s) < paraleloLeitores || runtime.GOMAXPROCS(0) < 2 {
		for _, l := range m.leitores {
			l.Achar(s, func(o ObjAchado) { m.aplicarAchado(l, s, ct, o, aprende, add) })
		}
		return
	}
	// texto grande: os leitores (que só leem s) rodam em paralelo; os achados são aplicados e
	// aprendidos depois, na ordem dos leitores, como se tivessem rodado um depois do outro
	res := make([][]ObjAchado, len(m.leitores))
	var prox atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < min(runtime.GOMAXPROCS(0), len(m.leitores)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(prox.Add(1)) - 1
				if i >= len(m.leitores) {
					return
				}
				m.leitores[i].Achar(s, func(o ObjAchado) { res[i] = append(res[i], o) })
			}
		}()
	}
	wg.Wait()
	for i, l := range m.leitores {
		for _, o := range res[i] {
			m.aplicarAchado(l, s, ct, o, aprende, add)
		}
	}
}

// paraleloLeitores: a partir deste tamanho, os leitores rodam em paralelo (variável para os testes).
var paraleloLeitores = 64 << 10

// rodarLeitor: um leitor em s, com os freios comuns (tipo desligado, pseudônimo, vocabulário).
func (m *Masker) rodarLeitor(l Leitor, s string, aprende bool, add func(ini, fim int, tipo string)) {
	ct := contextoTexto(s)
	l.Achar(s, func(o ObjAchado) { m.aplicarAchado(l, s, ct, o, aprende, add) })
}

// ctxTexto: o que vale para o texto inteiro, medido uma vez antes dos leitores: o software
// público que ele roda como imagem (softwareDoTexto).
type ctxTexto struct {
	software map[string]bool
}

func contextoTexto(s string) ctxTexto {
	return ctxTexto{software: softwareDoTexto(s)}
}

// aplicarAchado: um achado do leitor l, com os freios comuns. ct: o contexto do texto.
func (m *Masker) aplicarAchado(l Leitor, s string, ct ctxTexto, o ObjAchado, aprende bool, add func(ini, fim int, tipo string)) {
	if o.Ini < 0 || o.Fim > len(s) || o.Fim <= o.Ini || !m.objMascara(o.Ent) {
		return
	}
	v := s[o.Ini:o.Fim]
	if ehPseudoObj(v) || (l.Publico != nil && l.Publico(strings.Trim(v, "[]\"`"))) || achadoDeVocabulario(l, o, strings.Trim(v, "[]\"`'"), ct.software) {
		return
	}
	add(o.Ini, o.Fim, prefTipoObj+o.Ent)
	if aprende {
		m.aprenderObj(o, v, l.Publico)
	}
	if ganchoAchado != nil {
		ganchoAchado(l.Nome, o, v)
	}
}

// ganchoAchado: só para as medições (medicao_test.go): cada achado de cada leitor.
var ganchoAchado func(leitor string, o ObjAchado, v string)

// tokensObj chama fn para cada candidato a nome aprendido em s: [A-Za-z_][A-Za-z0-9_$#]*
// (com letras acentuadas) e pedaços "-..." no meio (o mesmo que a regex
// `[\p{L}_][\p{L}\d_$#]*(?:-[\p{L}\d_$#]+)*`, sem o custo de uma regex no texto inteiro).
func tokensObj(s string, fn func(a, b int)) {
	ident := func(c byte) bool { return ehAlnum(c) || c == '_' || c == '$' || c == '#' }
	for i := 0; i < len(s); {
		c := s[i]
		n0 := 1
		if c >= 0x80 {
			if n0 = letraUTF8(s, i); n0 == 0 { // letra acentuada também começa um nome
				i++
				continue
			}
		} else if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
			i++
			continue
		}
		j := i + n0
		for {
			for j < len(s) {
				if ident(s[j]) {
					j++
				} else if n := letraUTF8(s, j); n > 0 {
					j += n
				} else {
					break
				}
			}
			if j+1 < len(s) && s[j] == '-' && ident(s[j+1]) {
				j++
				continue
			}
			break
		}
		fn(i, j)
		if escapeEm(s, i) && j > i+1 && (ehAlnum(s[i+1]) && !(s[i+1] >= '0' && s[i+1] <= '9') || s[i+1] == '_') {
			fn(i+1, j) // "\nt_x" em texto escapado: o nome pode começar depois do escape
		}
		i = j
	}
}

// escapeEm: s[i] é a letra de um escape \n, \t ou \r (texto de JSON escapado)?
func escapeEm(s string, i int) bool {
	return i > 0 && s[i-1] == '\\' && (s[i] == 'n' || s[i] == 't' || s[i] == 'r') && (i < 2 || s[i-2] != '\\')
}

// acharObjetosConhecidos: nomes aprendidos (em RAM ou, só o hash, no vistos.json) que
// aparecem em s, em qualquer caixa e fora de qualquer estrutura.
func (m *Masker) acharObjetosConhecidos(s string, add func(ini, fim int, tipo string)) {
	c := m.conh
	c.mu.RLock()
	nRAM := c.nObj
	c.mu.RUnlock()
	disco := m.vistos != nil && m.vistos.TemObj()
	if nRAM == 0 && !disco {
		return
	}
	d := hoje()
	tokensObj(s, func(a0, b0 int) {
		if a0 > 0 && (s[a0-1] == '-' || ehAlnum(s[a0-1])) && !escapeEm(s, a0-1) {
			return
		}
		// nome com ponto ("top_x.eventos", "app.config"): o token para no ponto, então tenta
		// também as formas com os pedaços seguintes, da mais longa para a mais curta
		fins := formasComPonto(s, a0, b0)
		for k := len(fins) - 1; k >= 0; k-- {
			if m.objConhecidoEm(s, a0, fins[k], c, disco, d, add) {
				return
			}
		}
	})
}

// formasComPonto: os fins possíveis de um nome que começa em s[a:b] e continua com ".pedaço"
// (até 3 pedaços a mais): [b, fim com 1 pedaço, fim com 2...].
func formasComPonto(s string, a, b int) []int {
	fins := []int{b}
	ident := func(c byte) bool { return ehAlnum(c) || c == '_' || c == '$' || c == '#' }
	for n := 0; n < 3 && b+1 < len(s) && s[b] == '.' && ident(s[b+1]); n++ {
		e := b + 1
		for e < len(s) && (ident(s[e]) || s[e] == '-' && e+1 < len(s) && ident(s[e+1])) {
			e++
		}
		fins = append(fins, e)
		b = e
	}
	return fins
}

// objConhecidoEm: s[a:b] é um nome aprendido (RAM ou vistos.json)? Se for, entrega o achado.
func (m *Masker) objConhecidoEm(s string, a, b int, c *conhecidos, disco bool, d int, add func(ini, fim int, tipo string)) bool {
	ix := [2]int{a, b}
	v := s[ix[0]:ix[1]]
	if !caraDeIdentificador(v) {
		return false
	}
	c.mu.RLock()
	tp, ok := c.canon["O:"+v] // grafia exata
	if !ok {
		tp, ok = c.canon["o:"+strings.ToLower(v)] // nome de SQL, em qualquer caixa
		ok = ok && entSQL[strings.TrimPrefix(tp, prefTipoObj)]
	}
	c.mu.RUnlock()
	if ok {
		if ent := strings.TrimPrefix(tp, prefTipoObj); m.objPropaga(ent) {
			add(ix[0], ix[1], tp)
		}
		return true
	}
	if !disco {
		return false
	}
	chave := "O:" + v
	ent, visto, ok := m.vistos.Obj(m.idObj(chave))
	if !ok {
		chave = "o:" + strings.ToLower(v)
		ent, visto, ok = m.vistos.Obj(m.idObj(chave))
		ok = ok && entSQL[ent]
	}
	if !ok || d-visto > validadeObj || !m.objPropaga(ent) {
		return false
	}
	add(ix[0], ix[1], prefTipoObj+ent)
	c.aprender(prefTipoObj+ent, v) // volta para a memória
	m.vistos.MarcarObj(m.idObj(chave), ent, d)
	return true
}

// UsarLeitores troca os leitores de estrutura (os testes usam leitores próprios).
func (m *Masker) UsarLeitores(ls []Leitor) { m.leitores = ls }
