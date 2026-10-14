package mask

import (
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Objects: nomes de recursos internos (servidor, banco, schema, tabela, coluna, procedure,
// índice, usuário, namespace, serviço, bucket, fila), reconhecidos pela estrutura do conteúdo
// (ver docs/pt-BR/structures.md). Este arquivo é a base comum: tipos, pseudônimo, aprendizado com
// freios e liga/desliga por tipo. Os leitores de cada formato entregam os achados por
// m.leitores.

// ObjectPrefixes: tipo de entidade -> prefixo do pseudônimo.
var ObjectPrefixes = map[string]string{"servidor": "HOST", "database": "DB", "schema": "SCH", "tabela": "T",
	"coluna": "C", "procedure": "PROC", "indice": "IDX", "usuario": "USR", "namespace": "NS",
	"servico": "SVC", "bucket": "BKT", "fila": "TOP",
	"repositorio": "REPO", "organizacao": "ORG", "pacote": "PKG", "pasta": "DIR", "conta_nuvem": "ACC"}

// defaultPropagates: tipos que, aprendidos, são mascarados também fora da posição estrutural.
// Column e índice não: nomes como user_id e created_at existem em todo código.
var defaultPropagates = map[string]bool{"servidor": true, "database": true, "schema": true, "tabela": true,
	"procedure": true, "usuario": true, "namespace": true, "servico": true, "bucket": true, "fila": true,
	"repositorio": true, "organizacao": true, "pacote": true, "pasta": true, "conta_nuvem": true}

const objTypePrefix = "obj." // o tipo interno de um objeto: "obj.tabela"

func isObject(typ string) bool { return strings.HasPrefix(typ, objTypePrefix) }

// ObjFinding: um nome de objeto achado por um leitor de estrutura.
type ObjFinding struct {
	Start, End int
	Ent        string // tipo de entidade (chave de ObjectPrefixes)
	Rule       string // nome da regra, para contar e para a evidência "2 regras diferentes"
	Strong     bool   // posição inequívoca: ensina sozinho
}

// Reader procura nomes de objeto em s, pela estrutura.
type Reader struct {
	Name   string
	Find   func(s string, add func(ObjFinding))
	Public func(v string) bool // vocabulário do formato: nunca é mascarado nem aprendido
}

// objMask / objPropagates: liga/desliga por tipo (config "objetos").
func (m *Masker) objMask(ent string) bool {
	o := m.cfg.Objects
	if !o.Enabled {
		return false
	}
	if v, ok := o.Mask[ent]; ok {
		return v
	}
	return true
}

func (m *Masker) objPropagates(ent string) bool {
	if !m.objMask(ent) {
		return false
	}
	if v, ok := m.cfg.Objects.Propagate[ent]; ok {
		return v
	}
	return defaultPropagates[ent]
}

// entSQL: tipos insensíveis à caixa: os de SQL (identificadores sem aspas são insensíveis à
// caixa em todos os dialetos) e servidor (nome DNS não diferencia maiúsculas: "sqlprd01" e
// "SQLPRD01" são a mesma máquina). Nos outros tipos (caminho, bucket, fila...) a grafia vale
// como está: "/dados/Relatorios" e "/dados/relatorios" são duas pastas.
var entSQL = map[string]bool{"database": true, "schema": true, "tabela": true, "coluna": true, "procedure": true,
	"indice": true, "servidor": true}

// unquoted: o nome sem os colchetes, aspas ou crases em volta.
func unquoted(v string) string {
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
	v = unquoted(v)
	if entSQL[ent] {
		return strings.ToLower(v)
	}
	return v
}

// canonObj: a chave do nome na memória e no vistos.json. "o:" = sem caixa (SQL), "O:" = exata.
func canonObj(ent, v string) string {
	if entSQL[ent] {
		return "o:" + strings.ToLower(unquoted(v))
	}
	return "O:" + unquoted(v)
}

// pseudoObject: prefixo do tipo + o ID do HMAC da chave (o mesmo tamanho dos outros tipos).
// Estável entre conversas e reinícios. Name em minúsculas recebe o prefixo em minúsculas
// ("t_..."), para combinar com o estilo do texto; em maiúsculas, o prefixo em maiúsculas. Name
// de caixa mista (Pedido_Item) recebe o prefixo com só a primeira letra maiúscula e as letras
// do ID na caixa que o HMAC da própria grafia diz (com a chave, como o resto do pseudônimo: um
// hash sem chave deixaria conferir pela caixa um nome adivinhado): o mesmo nome de SQL em duas
// grafias no mesmo texto ("ContAB" e "ContAb", que são o mesmo objeto) sai com dois pseudônimos, e a volta
// devolve cada grafia como estava.
func (m *Masker) pseudoObject(ent, real string) string {
	pfx := ObjectPrefixes[ent]
	if pfx == "" {
		pfx = "OBJ"
	}
	id := m.p.ID("objeto", ent+"\x00"+normObj(ent, real))
	switch {
	case strings.ToLower(real) == real:
		pfx = strings.ToLower(pfx)
	case strings.ToUpper(real) == real:
	default:
		pfx = pfx[:1] + strings.ToLower(pfx[1:])
		h := m.p.Bits("caixa", real, 8)
		b := []byte(id)
		upper := false
		for i := range b {
			if b[i] >= 'a' && b[i] <= 'z' && h[i]&1 == 1 {
				b[i] -= 32
				upper = true
			}
		}
		for i := 0; !upper && i < len(b); i++ { // pelo menos uma: não confunde com a de maiúsculas
			if b[i] >= 'a' && b[i] <= 'z' {
				b[i] -= 32
				upper = true
			}
		}
		id = string(b)
	}
	ps := pfx + "_" + id
	registerPseudo(ps)
	return ps
}

// rePseudoObj: a FORMA de um pseudônimo de objeto. Só a forma não basta para pular um nome
// ("t_customer" é uma tabela real com essa cara): ver isPseudoObj.
var rePseudoObj = regexp.MustCompile(`^(?i:host|db|sch|t|c|proc|idx|usr|ns|svc|bkt|top|repo|org|pkg|dir|acc|obj)_[a-zA-Z2-7]{8}$`)

// pseudônimos de objeto gerados neste processo (em minúsculas): só esses são pulados.
var (
	generatedPseudos sync.Map
	nPseudos         atomic.Int64
)

const maxGeneratedPseudos = 500_000

func registerPseudo(ps string) {
	k := strings.ToLower(ps)
	if _, ok := generatedPseudos.LoadOrStore(k, true); !ok && nPseudos.Add(1) > maxGeneratedPseudos {
		generatedPseudos.Clear()
		nPseudos.Store(0)
	}
}

// isPseudoObj: v é um pseudônimo de objeto gerado por nós (em qualquer caixa)?
func isPseudoObj(v string) bool {
	if !rePseudoObj.MatchString(v) {
		return false
	}
	_, ok := generatedPseudos.Load(strings.ToLower(v))
	return ok
}

// identifierShape: o freio que separa um nome de recurso de uma palavra comum. Só nomes
// assim são aprendidos e propagados: têm "_", dígito, ponto, hífen entre partes ou mistura de
// caixa ("tb_pedido", "srv01", "app.config", "svc-pedidos", "contaCorrente"). Ficam de fora
// versões, UUIDs e hashes, que têm dígitos mas não são nomes.
func identifierShape(v string) bool {
	if len(v) < 3 || len(v) > 128 || hasDigit(v) && reVersionOrHash.MatchString(v) {
		return false
	}
	letter := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			letter = true
		}
	}
	if !letter {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '_' || c == '.' || c >= '0' && c <= '9':
			return true
		case c == '-' && i > 0 && i+1 < len(v) && isAlnum(v[i-1]) && isAlnum(v[i+1]):
			return true
		case c >= 'A' && c <= 'Z' && i > 0 && v[i-1] >= 'a' && v[i-1] <= 'z':
			return true
		}
	}
	return false
}

// versão (semver, regex oficial de semver.org sem âncoras internas), UUID e hash hexadecimal
var reVersionOrHash = regexp.MustCompile(`^(?:v?(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:\.(?:0|[1-9]\d*))?(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]{16,})$`)

// fracos: evidência fraca, só em RAM: nome -> regras diferentes em que foi visto. Duas regras
// diferentes valem como evidência forte.
type weakSet struct {
	mu      sync.Mutex
	rules   map[string]int // nome da regra -> bit
	seenSet map[string]uint64
}

const maxWeak = 200_000

func (f *weakSet) markIt(name, rule string) (twoOf bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rules == nil {
		f.rules, f.seenSet = map[string]int{}, map[string]uint64{}
	}
	b, ok := f.rules[rule]
	if !ok {
		if len(f.rules) >= 64 {
			b = 63
		} else {
			b = len(f.rules)
			f.rules[rule] = b
		}
	}
	if len(f.seenSet) >= maxWeak {
		f.seenSet = map[string]uint64{}
	}
	f.seenSet[name] |= 1 << uint(b)
	v := f.seenSet[name]
	return v&(v-1) != 0 // mais de um bit
}

// learnObj aplica os freios e, se passar, lembra o nome (RAM e, só o hash, vistos.json).
func (m *Masker) learnObj(o ObjFinding, real string, public func(string) bool) {
	v := strings.Trim(strings.TrimSpace(real), "[]\"`")
	if len(v) < 4 || !m.objPropagates(o.Ent) || !identifierShape(v) || isPseudoObj(v) {
		return
	}
	if public != nil && public(v) {
		return
	}
	if ent, ok := m.learnedEnt(v); ok {
		o.Ent = ent // já aprendido: mantém o tipo (e o pseudônimo) da primeira vez
	}
	if !o.Strong && !m.weakSet.markIt(canonObj(o.Ent, v), o.Rule) {
		return // evidência fraca: mascara no lugar, mas não ensina
	}
	m.known.learnIt(objTypePrefix+o.Ent, v)
	if m.seenSet != nil {
		m.seenSet.MarkObj(m.objID(canonObj(o.Ent, v)), o.Ent, today())
	}
}

// learnedEnt: o tipo com que o nome v já foi aprendido (em RAM ou no vistos.json), se foi.
func (m *Masker) learnedEnt(v string) (string, bool) {
	v = unquoted(v)
	low := strings.ToLower(v)
	c := m.known
	c.mu.RLock()
	tp, ok := c.canon["O:"+v]
	if !ok {
		tp, ok = c.canon["o:"+low]
		ok = ok && entSQL[strings.TrimPrefix(tp, objTypePrefix)]
	}
	c.mu.RUnlock()
	if ok && isObject(tp) {
		return strings.TrimPrefix(tp, objTypePrefix), true
	}
	if m.seenSet == nil || !m.seenSet.HasObj() {
		return "", false
	}
	ent, seen, ok := m.seenSet.Obj(m.objID("O:" + v))
	if !ok {
		ent, seen, ok = m.seenSet.Obj(m.objID("o:" + low))
		ok = ok && entSQL[ent]
	}
	return ent, ok && today()-seen <= objValidity
}

// serversAsHost: um servidor achado pela estrutura (o host de uma string de conexão, de um
// YAML) que é um hostname do domínio interno passa a ter o tipo "host", o mesmo que o detector de
// hostname dá a ele fora da estrutura. Assim o mesmo servidor tem UM pseudônimo em qualquer lugar
// (antes: host_x7k2... dentro da URL e x7k2....invalid solto no texto, e o modelo via dois).
func (m *Masker) serversAsHost(out []Finding) {
	if len(m.cfg.InternalDomains) == 0 {
		return
	}
	for i, a := range out {
		if a.Type != objTypePrefix+"servidor" {
			continue
		}
		h := strings.ToLower(a.Real)
		if ix := reHost.FindStringIndex(h); ix != nil && ix[0] == 0 && ix[1] == len(h) && m.internal(h) && !strings.HasSuffix(h, ".invalid") {
			out[i].Type = "host"
		}
	}
}

// unifyObjects: o mesmo nome real fica com UM tipo (e, portanto, um pseudônimo) no texto,
// mesmo quando regras diferentes o acham com tipos diferentes ("dir_..." numa linha e
// "svc_..." noutra para a mesma pasta). Vale o tipo com que o nome já foi aprendido; se não
// foi, o da primeira ocorrência no texto (no mesmo trecho, a ordem do tipo, como em applyT).
func (m *Masker) unifyObjects(out []Finding) {
	m.serversAsHost(out)
	var ix []int
	for i, a := range out {
		if isObject(a.Type) {
			ix = append(ix, i)
		}
	}
	if len(ix) < 2 && (len(ix) == 0 || m.known.generation() == 0 && (m.seenSet == nil || !m.seenSet.HasObj())) {
		return
	}
	sort.SliceStable(ix, func(i, j int) bool {
		a, b := out[ix[i]], out[ix[j]]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.Type < b.Type
	})
	choice := map[string]string{}
	for _, i := range ix {
		k := strings.ToLower(unquoted(out[i].Real))
		t, ok := choice[k]
		if !ok {
			t = out[i].Type
			if ent, ok := m.learnedEnt(out[i].Real); ok && m.objMask(ent) {
				t = objTypePrefix + ent
			}
			choice[k] = t
		}
		out[i].Type = t
	}
}

// objID: o hash com que o nome fica no vistos.json (o tipo vai no valor).
func (m *Masker) objID(key string) string { return m.p.ID("visto", key) }

// hoje: dias desde 1970 (o vistos.json guarda datas assim). Variável para os testes.
var today = func() int { return int(time.Now().Unix() / 86400) }

// objValidity: um nome não visto há mais que isso deixa de ser propagado.
const objValidity = 90

// findObjects roda os leitores em s, devolve os achados (para mascarar) e ensina.
func (m *Masker) findObjects(s string, learn bool, add func(ini, end int, typ string)) {
	if !m.cfg.Objects.Enabled || len(m.readers) == 0 {
		return
	}
	ct := textContext(s)
	if len(s) < parallelReaders || runtime.GOMAXPROCS(0) < 2 {
		for _, l := range m.readers {
			l.Find(s, func(o ObjFinding) { m.applyFinding(l, s, ct, o, learn, add) })
		}
		return
	}
	// texto grande: os leitores (que só leem s) rodam em paralelo; os achados são aplicados e
	// aprendidos depois, na ordem dos leitores, como se tivessem rodado um depois do outro
	res := make([][]ObjFinding, len(m.readers))
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < min(runtime.GOMAXPROCS(0), len(m.readers)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(m.readers) {
					return
				}
				m.readers[i].Find(s, func(o ObjFinding) { res[i] = append(res[i], o) })
			}
		}()
	}
	wg.Wait()
	for i, l := range m.readers {
		for _, o := range res[i] {
			m.applyFinding(l, s, ct, o, learn, add)
		}
	}
}

// parallelReaders: a partir deste tamanho, os leitores rodam em paralelo (variável para os testes).
var parallelReaders = 64 << 10

// runReader: um leitor em s, com os freios comuns (tipo desligado, pseudônimo, vocabulário).
func (m *Masker) runReader(l Reader, s string, learn bool, add func(ini, end int, typ string)) {
	ct := textContext(s)
	l.Find(s, func(o ObjFinding) { m.applyFinding(l, s, ct, o, learn, add) })
}

// ctxText: o que vale para o texto inteiro, medido uma vez antes dos leitores: o software
// público que ele roda como imagem (textSoftware).
type ctxText struct {
	software map[string]bool
}

func textContext(s string) ctxText {
	return ctxText{software: textSoftware(s)}
}

// applyFinding: um achado do leitor l, com os freios comuns. ct: o contexto do texto.
func (m *Masker) applyFinding(l Reader, s string, ct ctxText, o ObjFinding, learn bool, add func(ini, end int, typ string)) {
	if o.Start < 0 || o.End > len(s) || o.End <= o.Start || !m.objMask(o.Ent) {
		return
	}
	v := s[o.Start:o.End]
	if isPseudoObj(v) || (l.Public != nil && l.Public(strings.Trim(v, "[]\"`"))) || vocabularyFinding(l, o, strings.Trim(v, "[]\"`'"), ct.software) {
		return
	}
	add(o.Start, o.End, objTypePrefix+o.Ent)
	if learn {
		m.learnObj(o, v, l.Public)
	}
	if hookFound != nil {
		hookFound(l.Name, o, v)
	}
}

// hookFound: só para as medições (measurement_test.go): cada achado de cada leitor.
var hookFound func(reader string, o ObjFinding, v string)

// objTokens chama fn para cada candidato a nome aprendido em s: [A-Za-z_][A-Za-z0-9_$#]*
// (com letras acentuadas) e pedaços "-..." no meio (o mesmo que a regex
// `[\p{L}_][\p{L}\d_$#]*(?:-[\p{L}\d_$#]+)*`, sem o custo de uma regex no texto inteiro).
func objTokens(s string, fn func(a, b int)) {
	ident := func(c byte) bool { return isAlnum(c) || c == '_' || c == '$' || c == '#' }
	for i := 0; i < len(s); {
		c := s[i]
		n0 := 1
		if c >= 0x80 {
			if n0 = utf8Letter(s, i); n0 == 0 { // letra acentuada também começa um nome
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
				} else if n := utf8Letter(s, j); n > 0 {
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
		if escapeIn(s, i) && j > i+1 && (isAlnum(s[i+1]) && !(s[i+1] >= '0' && s[i+1] <= '9') || s[i+1] == '_') {
			fn(i+1, j) // "\nt_x" em texto escapado: o nome pode começar depois do escape
		}
		i = j
	}
}

// escapeIn: s[i] é a letra de um escape \n, \t ou \r (texto de JSON escapado)?
func escapeIn(s string, i int) bool {
	return i > 0 && s[i-1] == '\\' && (s[i] == 'n' || s[i] == 't' || s[i] == 'r') && (i < 2 || s[i-2] != '\\')
}

// findKnownObjects: nomes aprendidos (em RAM ou, só o hash, no vistos.json) que
// aparecem em s, em qualquer caixa e fora de qualquer estrutura.
func (m *Masker) findKnownObjects(s string, add func(ini, end int, typ string)) {
	c := m.known
	c.mu.RLock()
	nRAM := c.nObj
	c.mu.RUnlock()
	disk := m.seenSet != nil && m.seenSet.HasObj()
	if nRAM == 0 && !disk {
		return
	}
	d := today()
	objTokens(s, func(a0, b0 int) {
		if a0 > 0 && (s[a0-1] == '-' || isAlnum(s[a0-1])) && !escapeIn(s, a0-1) {
			return
		}
		// nome com ponto ("top_x.eventos", "app.config"): o token para no ponto, então tenta
		// também as formas com os pedaços seguintes, da mais longa para a mais curta
		ends := dottedShapes(s, a0, b0)
		for k := len(ends) - 1; k >= 0; k-- {
			if m.knownObjIn(s, a0, ends[k], c, disk, d, add) {
				return
			}
		}
	})
}

// dottedShapes: os fins possíveis de um nome que começa em s[a:b] e continua com ".pedaço"
// (até 3 pedaços a mais): [b, fim com 1 pedaço, fim com 2...].
func dottedShapes(s string, a, b int) []int {
	ends := []int{b}
	ident := func(c byte) bool { return isAlnum(c) || c == '_' || c == '$' || c == '#' }
	for n := 0; n < 3 && b+1 < len(s) && s[b] == '.' && ident(s[b+1]); n++ {
		e := b + 1
		for e < len(s) && (ident(s[e]) || s[e] == '-' && e+1 < len(s) && ident(s[e+1])) {
			e++
		}
		ends = append(ends, e)
		b = e
	}
	return ends
}

// knownObjIn: s[a:b] é um nome aprendido (RAM ou vistos.json)? Se for, entrega o achado.
func (m *Masker) knownObjIn(s string, a, b int, c *knownSet, disk bool, d int, add func(ini, end int, typ string)) bool {
	ix := [2]int{a, b}
	v := s[ix[0]:ix[1]]
	if !identifierShape(v) {
		return false
	}
	c.mu.RLock()
	tp, ok := c.canon["O:"+v] // grafia exata
	if !ok {
		tp, ok = c.canon["o:"+strings.ToLower(v)] // nome de SQL, em qualquer caixa
		ok = ok && entSQL[strings.TrimPrefix(tp, objTypePrefix)]
	}
	c.mu.RUnlock()
	if ok {
		if ent := strings.TrimPrefix(tp, objTypePrefix); m.objPropagates(ent) {
			add(ix[0], ix[1], tp)
		}
		return true
	}
	if !disk {
		return false
	}
	key := "O:" + v
	ent, seen, ok := m.seenSet.Obj(m.objID(key))
	if !ok {
		key = "o:" + strings.ToLower(v)
		ent, seen, ok = m.seenSet.Obj(m.objID(key))
		ok = ok && entSQL[ent]
	}
	if !ok || d-seen > objValidity || !m.objPropagates(ent) {
		return false
	}
	add(ix[0], ix[1], objTypePrefix+ent)
	c.learnIt(objTypePrefix+ent, v) // volta para a memória
	m.seenSet.MarkObj(m.objID(key), ent, d)
	return true
}

// UseReaders troca os leitores de estrutura (os testes usam leitores próprios).
func (m *Masker) UseReaders(ls []Reader) { m.readers = ls }
