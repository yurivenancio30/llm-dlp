package mask

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Unmask: troca os pseudônimos de volta, inclusive em resposta que chega em pedaços.

// Table é o "de:para" pseudônimo -> real de UMA requisição. Vive só em memória:
// é montada enquanto a requisição é mascarada e usada para desmascarar a resposta.
//
// A busca dos pseudônimos num texto usa um índice pelo começo (4 primeiros bytes -> tamanhos
// possíveis) e depois uma consulta direta no mapa. O custo depende do tamanho do texto, não
// de quantos pseudônimos há: com uma regex de alternativas, 5 mil pseudônimos distintos
// (um CSV de e-mails na conversa) já levavam segundos.
type Table struct {
	m        map[string]string
	typ      map[string]string    // pseudônimo -> tipo (quem escreveu: ver decision.go)
	pfx      map[uint32][]int32   // 4 primeiros bytes -> tamanhos, do maior para o menor
	pair     [1 << 16 / 64]uint64 // filtro: algum pseudônimo começa com estes 2 bytes?
	start    map[string]bool      // começos de 1 a 3 bytes (para saber se um fim de texto pode ser um pseudônimo cortado)
	shorts   []string             // pseudônimos com menos de 4 bytes (não deveria haver)
	maxLen   int
	ipPrefix map[string]string // "240.a.b" -> "10.42.7"
	// Conflicts: pseudônimos que colidiram e por isso não serão desmascarados (para log).
	Conflicts int
	collided  []string // os pseudônimos (ou prefixos de IP falso) que colidiram, até 20

	muNotUndone sync.Mutex
	nd          []string // pseudônimos nossos que sobraram na volta (ver NotUndone)
}

// reLeftover: as formas de pseudônimo, em qualquer caixa, que podem sobrar na volta: rótulo
// ("CPF-x", "Sensivel-x"), pessoa, objeto ("t_x"), e-mail e host (.invalid) e IP falso.
var reLeftover = regexp.MustCompile(`[A-Za-z][A-Za-z:]*-[A-Za-z2-7]{8}|(?i:pessoa) [A-Za-z2-7]{8}|[A-Za-z]+_[A-Za-z2-7]{8}|[\w.+-]+\.invalid|\b2(?:4\d|5[0-5])\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)

const maxNotUndone = 20

// noteLeftovers: os pseudônimos gerados pelo llm-dlp que ficaram em s depois de desmascarar (o
// modelo trocou a caixa de um jeito que nenhuma variante cobre, cortou ou juntou um pseudônimo,
// ou citou um de outra conversa). Um IP da faixa falsa também conta: nenhum dado real usa
// 240.0.0.0/4.
func (t *Table) noteLeftovers(s string) {
	if !strings.ContainsAny(s, "-_.") && !strings.Contains(s, "essoa") && !strings.Contains(s, "ESSOA") {
		return
	}
	for _, v := range reLeftover.FindAllString(s, -1) {
		ours := false
		if a, err := netip.ParseAddr(v); err == nil {
			ours = fakeNet.Contains(a)
		} else {
			_, ours = generatedPseudos.Load(strings.ToLower(v))
		}
		if !ours {
			continue
		}
		t.muNotUndone.Lock()
		if len(t.nd) < maxNotUndone && !slices.Contains(t.nd, v) {
			t.nd = append(t.nd, v)
		}
		t.muNotUndone.Unlock()
	}
}

// NotUndone: os pseudônimos que sobraram nas respostas desmascaradas com esta tabela (até 20).
// Pseudônimo não é dado real: pode ir para o log.
func (t *Table) NotUndone() []string {
	t.muNotUndone.Lock()
	defer t.muNotUndone.Unlock()
	return slices.Clone(t.nd)
}

// Collided: os pseudônimos que dois valores reais diferentes receberam nesta tabela (até 20; um
// IP falso aparece como "240.a.b.x", a rede /24 inteira). Pseudônimo não é dado real: pode ir
// para o log, e a forma dele diz o tipo.
func (t *Table) Collided() []string { return t.collided }

var reFakeIP = regexp.MustCompile(`\b(2(?:4\d|5[0-5])\.\d{1,3}\.\d{1,3})\.(\d{1,3})\b`)

func NewTable(entries []Entry) *Table {
	t := &Table{m: map[string]string{}, typ: map[string]string{}, pfx: map[uint32][]int32{}, start: map[string]bool{}, ipPrefix: map[string]string{}}
	// Colisão (dois valores reais com o mesmo pseudônimo — rara, mas possível, sobretudo na
	// sub-rede falsa de IP): desmascarar escolheria um dos dois e poderia, p.ex., apontar um
	// comando para o host errado. Nesses casos o pseudônimo fica sem desmascarar.
	conflict, conflictIP := map[string]bool{}, map[string]bool{}
	// primeiro os pseudônimos como saíram, depois os que o modelo escreveu noutra caixa (um texto
	// do assistente reenviado como a API o mandou): a variante de uma grafia nunca toma o lugar
	// do pseudônimo de outra
	ord := make([]Entry, 0, len(entries))
	var others []Entry
	for _, e := range entries {
		if primaryCase(e.Pseudo, e.Real, e.Type) {
			ord = append(ord, e)
		} else {
			others = append(others, e)
		}
	}
	nPrimary := len(ord)
	ord = append(ord, others...)
	var base []Entry // um por pseudônimo, para as variantes de caixa
	for k, e := range ord {
		if _, ok := t.m[e.Pseudo]; ok && k >= nPrimary {
			continue // variante de caixa que coincide com um pseudônimo já registrado
		}
		if r, ok := t.m[e.Pseudo]; ok {
			// o mesmo número com outra pontuação ("12.345.678-9" e "123456789") não é
			// colisão: é o mesmo valor. Fica a primeira grafia vista. Também não é colisão o mesmo
			// pseudônimo de pessoa para dois nomes: o registro de pessoas dá o id da pessoa ao nome
			// completo e ao primeiro + último ("Maria Fernanda Silva" e "Maria Silva"), e as duas
			// grafias são a mesma pessoa.
			if r != e.Real && e.Type != "nome" && !sameValue(e.Type, r, e.Real) {
				conflict[e.Pseudo] = true
			}
			continue
		}
		t.m[e.Pseudo] = e.Real
		t.typ[e.Pseudo] = e.Type
		if e.Type == "nome" || pseudoWithCase(e.Type) {
			base = append(base, e)
		}
		if reFakeIP.MatchString(e.Pseudo) && strings.Count(e.Pseudo, ".") == 3 && strings.Count(e.Real, ".") == 3 {
			f, r := e.Pseudo[:strings.LastIndex(e.Pseudo, ".")], e.Real[:strings.LastIndex(e.Real, ".")]
			if current, ok := t.ipPrefix[f]; ok && current != r {
				conflictIP[f] = true
			}
			t.ipPrefix[f] = r
		}
	}
	// as outras caixas do pseudônimo (de uma pessoa, de um objeto, de um tipo com rótulo), depois
	// de todas as grafias que apareceram: a variante nunca toma o lugar de uma grafia real
	// ("Maria da Silva" e "propostas" ficam como estavam)
	for _, e := range base {
		if conflict[e.Pseudo] {
			continue
		}
		var vs []Entry
		if e.Type == "nome" {
			vs = nameVariants(e.Pseudo, e.Real)
		} else {
			vs = caseVariants(e.Pseudo, e.Real, e.Type, t.m)
		}
		for _, v := range vs {
			if _, ok := t.m[v.Pseudo]; !ok {
				t.m[v.Pseudo], t.typ[v.Pseudo] = v.Real, v.Type
			}
		}
	}
	for p := range conflict {
		delete(t.m, p)
	}
	for f := range conflictIP {
		delete(t.ipPrefix, f)
		for p := range t.m {
			if strings.HasPrefix(p, f+".") {
				delete(t.m, p)
			}
		}
	}
	t.Conflicts = len(conflict) + len(conflictIP)
	for p := range conflict {
		t.collided = append(t.collided, p)
	}
	for f := range conflictIP {
		t.collided = append(t.collided, f+".x")
	}
	sort.Strings(t.collided)
	if len(t.collided) > maxNotUndone {
		t.collided = t.collided[:maxNotUndone]
	}
	for k := range t.m {
		if len(k) > t.maxLen {
			t.maxLen = len(k)
		}
		if len(k) < 4 {
			t.shorts = append(t.shorts, k)
			continue
		}
		for l := 1; l <= 3; l++ {
			t.start[k[:l]] = true
		}
		c := uint32(k[0])<<24 | uint32(k[1])<<16 | uint32(k[2])<<8 | uint32(k[3])
		has := false
		for _, n := range t.pfx[c] {
			if int(n) == len(k) {
				has = true
				break
			}
		}
		if !has {
			t.pfx[c] = append(t.pfx[c], int32(len(k)))
		}
		t.pair[c>>22] |= 1 << (c >> 16 & 63)
	}
	for _, ns := range t.pfx {
		sort.Slice(ns, func(i, j int) bool { return ns[i] > ns[j] })
	}
	sort.Slice(t.shorts, func(i, j int) bool { return len(t.shorts[i]) > len(t.shorts[j]) })
	return t
}

// primaryCase: o pseudônimo tem a caixa com que é gerado para esta grafia (e não é uma
// variante de caixa). Objeto (pseudoObject): minúsculo para nome minúsculo, prefixo em maiúsculas
// e ID minúsculo para nome em maiúsculas, prefixo com a primeira maiúscula para caixa mista. Type
// com rótulo: a forma de pseudoLabel. Os outros tipos não têm variante.
func primaryCase(ps, real, typ string) bool {
	if !pseudoWithCase(typ) {
		return true
	}
	i := strings.LastIndexByte(ps, pseudoSep(typ))
	if i <= 0 {
		return true
	}
	pfx, id := ps[:i], ps[i+1:]
	if !isObject(typ) {
		return primaryLabel(pfx, id, real)
	}
	switch {
	case strings.ToLower(real) == real:
		return ps == strings.ToLower(ps)
	case strings.ToUpper(real) == real:
		return pfx == strings.ToUpper(pfx) && id == strings.ToLower(id)
	}
	return pfx == pfx[:1]+strings.ToLower(pfx[1:]) && pfx[:1] == strings.ToUpper(pfx[:1]) && id != strings.ToLower(id)
}

// casa devolve o fim do pseudônimo mais longo que começa em s[i], ou -1.
func (t *Table) matched(s string, i int) int {
	if i+4 <= len(s) {
		k2 := uint32(s[i])<<8 | uint32(s[i+1])
		if t.pair[k2>>6]&(1<<(k2&63)) != 0 {
			for _, n := range t.pfx[k2<<16|uint32(s[i+2])<<8|uint32(s[i+3])] {
				if end := i + int(n); end <= len(s) {
					if _, ok := t.m[s[i:end]]; ok {
						return end
					}
				}
			}
		}
	}
	for _, k := range t.shorts {
		if strings.HasPrefix(s[i:], k) {
			return i + len(k)
		}
	}
	return -1
}

// varrer chama fn para cada pseudônimo de s, da esquerda para a direita, sem sobreposição.
func (t *Table) scan(s string, fn func(ini, end int)) {
	for i := 0; i < len(s); {
		if end := t.matched(s, i); end > 0 {
			fn(i, end)
			i = end
		} else {
			i++
		}
	}
}

// sameValue: a e b são o mesmo valor escrito de dois jeitos ("João Silva" e "JOAO SILVA",
// "12.345.678-9" e "123456789")? Segue a mesma normalização usada para gerar o pseudônimo.
func sameValue(typ, a, b string) bool {
	if isObject(typ) {
		ent := strings.TrimPrefix(typ, objTypePrefix)
		return normObj(ent, a) == normObj(ent, b)
	}
	switch typ {
	case "segredo", "ip":
		return false
	case "email", "host", "pix", "dominio":
		return strings.EqualFold(a, b)
	case "cpf", "cnpj", "pis", "cnh", "telefone", "cep", "cartao", "rg", "conta":
		return canonNum(a) == canonNum(b)
	}
	return NormName(a) == NormName(b)
}

func (t *Table) Empty() bool { return len(t.m) == 0 }

// Unmask troca pseudônimos por valores reais. Em json=true, o valor real é
// escapado para caber dentro de uma string JSON (uso em input_json_delta).
func (t *Table) Unmask(s string, inJSON bool) string {
	if t.Empty() {
		return s
	}
	esc := func(v string) string {
		if !inJSON {
			return v
		}
		b, _ := json.Marshal(v)
		return string(b[1 : len(b)-1])
	}
	var b strings.Builder
	lastPos := 0
	t.scan(s, func(ini, end int) {
		b.WriteString(s[lastPos:ini])
		b.WriteString(esc(t.m[s[ini:end]]))
		lastPos = end
	})
	if lastPos > 0 {
		b.WriteString(s[lastPos:])
		s = b.String()
	}
	if len(t.ipPrefix) > 0 {
		// outros endereços na mesma sub-rede falsa (ex.: 240.a.b.0/24 escrito pelo modelo)
		s = reFakeIP.ReplaceAllStringFunc(s, func(ip string) string {
			k := ip[:strings.LastIndex(ip, ".")]
			if r, ok := t.ipPrefix[k]; ok {
				return r + ip[strings.LastIndex(ip, "."):]
			}
			return ip
		})
	}
	t.noteLeftovers(s)
	return s
}

// segurar: quantos bytes do fim de s podem ser o começo de um pseudônimo.
func (t *Table) hold(s string) int {
	max := t.maxLen - 1
	if max > len(s) {
		max = len(s)
	}
	for l := max; l > 0; l-- {
		if t.canBeStart(s[len(s)-l:]) {
			return l
		}
	}
	if len(t.ipPrefix) > 0 { // fim do texto parecido com o começo de um IP falso: "24", "240.1", ...
		i := len(s)
		for i > 0 && (isDig(s[i-1]) || s[i-1] == '.') && len(s)-i < 16 {
			i--
		}
		if i < len(s) && s[i] == '2' && (i == 0 || !isDig(s[i-1])) {
			return len(s) - i
		}
	}
	return 0
}

// canBeStart: c pode ser o começo de um pseudônimo ainda incompleto? Para 4 bytes ou
// mais a resposta é aproximada (olha só o começo e o tamanho): pode segurar um texto que no
// fim não era pseudônimo, e ele é entregue no pedaço seguinte.
func (t *Table) canBeStart(c string) bool {
	for _, k := range t.shorts {
		if len(c) < len(k) && strings.HasPrefix(k, c) {
			return true
		}
	}
	if len(c) < 4 {
		return t.start[c]
	}
	for _, n := range t.pfx[uint32(c[0])<<24|uint32(c[1])<<16|uint32(c[2])<<8|uint32(c[3])] {
		if int(n) > len(c) {
			return true
		}
	}
	return false
}

// Stream desmascara um texto que chega em pedaços, sem nunca entregar um pseudônimo cortado.
type Stream struct {
	t      *Table
	inJSON bool
	buf    string
}

func (t *Table) NewStream(inJSON bool) *Stream { return &Stream{t: t, inJSON: inJSON} }

// Push recebe um pedaço e devolve o que já pode ser entregue (desmascarado).
func (f *Stream) Push(piece string) string {
	if f.t.Empty() {
		return piece
	}
	f.buf += piece
	// Só pode segurar DEPOIS do último pseudônimo completo; senão o fim de um pseudônimo
	// completo (ex.: "...x2") pode ser confundido com o começo de outro (ex.: IP "2..")
	// e o pseudônimo completo seria cortado ao meio.
	fullEnd := 0
	f.t.scan(f.buf, func(_, end int) { fullEnd = end })
	if len(f.t.ipPrefix) > 0 {
		if ms := reFakeIP.FindAllStringIndex(f.buf, -1); len(ms) > 0 && ms[len(ms)-1][1] > fullEnd &&
			ms[len(ms)-1][1] < len(f.buf) { // IP falso terminado (algo depois dele) também é completo
			fullEnd = ms[len(ms)-1][1]
		}
	}
	k := f.t.hold(f.buf[fullEnd:])
	ready := f.buf[:len(f.buf)-k]
	f.buf = f.buf[len(f.buf)-k:]
	return f.t.Unmask(ready, f.inJSON)
}

// Close entrega o que sobrou no fim do bloco.
func (f *Stream) Close() string {
	r := f.t.Unmask(f.buf, f.inJSON)
	f.buf = ""
	return r
}
