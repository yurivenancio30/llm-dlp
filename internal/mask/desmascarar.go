package mask

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Desmascarar: troca os pseudônimos de volta, inclusive em resposta que chega em pedaços.

// Tabela é o "de:para" pseudônimo -> real de UMA requisição. Vive só em memória:
// é montada enquanto a requisição é mascarada e usada para desmascarar a resposta.
//
// A busca dos pseudônimos num texto usa um índice pelo começo (4 primeiros bytes -> tamanhos
// possíveis) e depois uma consulta direta no mapa. O custo depende do tamanho do texto, não
// de quantos pseudônimos há: com uma regex de alternativas, 5 mil pseudônimos distintos
// (um CSV de e-mails na conversa) já levavam segundos.
type Tabela struct {
	m      map[string]string
	pref   map[uint32][]int32   // 4 primeiros bytes -> tamanhos, do maior para o menor
	par    [1 << 16 / 64]uint64 // filtro: algum pseudônimo começa com estes 2 bytes?
	inicio map[string]bool      // começos de 1 a 3 bytes (para saber se um fim de texto pode ser um pseudônimo cortado)
	curtos []string             // pseudônimos com menos de 4 bytes (não deveria haver)
	maxLen int
	ipPref map[string]string // "240.a.b" -> "10.42.7"
	// Conflitos: pseudônimos que colidiram e por isso não serão desmascarados (para log).
	Conflitos int
}

var reIPFalso = regexp.MustCompile(`\b(2(?:4\d|5[0-5])\.\d{1,3}\.\d{1,3})\.(\d{1,3})\b`)

func NovaTabela(entradas []Entrada) *Tabela {
	t := &Tabela{m: map[string]string{}, pref: map[uint32][]int32{}, inicio: map[string]bool{}, ipPref: map[string]string{}}
	// Colisão (dois valores reais com o mesmo pseudônimo — rara, mas possível, sobretudo na
	// sub-rede falsa de IP): desmascarar escolheria um dos dois e poderia, p.ex., apontar um
	// comando para o host errado. Nesses casos o pseudônimo fica sem desmascarar.
	conflito, conflitoIP := map[string]bool{}, map[string]bool{}
	for _, e := range entradas {
		if r, ok := t.m[e.Pseudo]; ok {
			// o mesmo número com outra pontuação ("12.345.678-9" e "123456789") não é
			// colisão: é o mesmo valor. Fica a primeira grafia vista.
			if r != e.Real && !mesmoValor(e.Tipo, r, e.Real) {
				conflito[e.Pseudo] = true
			}
			continue
		}
		t.m[e.Pseudo] = e.Real
		if reIPFalso.MatchString(e.Pseudo) && strings.Count(e.Pseudo, ".") == 3 && strings.Count(e.Real, ".") == 3 {
			f, r := e.Pseudo[:strings.LastIndex(e.Pseudo, ".")], e.Real[:strings.LastIndex(e.Real, ".")]
			if atual, ok := t.ipPref[f]; ok && atual != r {
				conflitoIP[f] = true
			}
			t.ipPref[f] = r
		}
	}
	for p := range conflito {
		delete(t.m, p)
	}
	for f := range conflitoIP {
		delete(t.ipPref, f)
		for p := range t.m {
			if strings.HasPrefix(p, f+".") {
				delete(t.m, p)
			}
		}
	}
	t.Conflitos = len(conflito) + len(conflitoIP)
	for k := range t.m {
		if len(k) > t.maxLen {
			t.maxLen = len(k)
		}
		if len(k) < 4 {
			t.curtos = append(t.curtos, k)
			continue
		}
		for l := 1; l <= 3; l++ {
			t.inicio[k[:l]] = true
		}
		c := uint32(k[0])<<24 | uint32(k[1])<<16 | uint32(k[2])<<8 | uint32(k[3])
		tem := false
		for _, n := range t.pref[c] {
			if int(n) == len(k) {
				tem = true
				break
			}
		}
		if !tem {
			t.pref[c] = append(t.pref[c], int32(len(k)))
		}
		t.par[c>>22] |= 1 << (c >> 16 & 63)
	}
	for _, ns := range t.pref {
		sort.Slice(ns, func(i, j int) bool { return ns[i] > ns[j] })
	}
	sort.Slice(t.curtos, func(i, j int) bool { return len(t.curtos[i]) > len(t.curtos[j]) })
	return t
}

// casa devolve o fim do pseudônimo mais longo que começa em s[i], ou -1.
func (t *Tabela) casa(s string, i int) int {
	if i+4 <= len(s) {
		k2 := uint32(s[i])<<8 | uint32(s[i+1])
		if t.par[k2>>6]&(1<<(k2&63)) != 0 {
			for _, n := range t.pref[k2<<16|uint32(s[i+2])<<8|uint32(s[i+3])] {
				if fim := i + int(n); fim <= len(s) {
					if _, ok := t.m[s[i:fim]]; ok {
						return fim
					}
				}
			}
		}
	}
	for _, k := range t.curtos {
		if strings.HasPrefix(s[i:], k) {
			return i + len(k)
		}
	}
	return -1
}

// varrer chama fn para cada pseudônimo de s, da esquerda para a direita, sem sobreposição.
func (t *Tabela) varrer(s string, fn func(ini, fim int)) {
	for i := 0; i < len(s); {
		if fim := t.casa(s, i); fim > 0 {
			fn(i, fim)
			i = fim
		} else {
			i++
		}
	}
}

// mesmoValor: a e b são o mesmo valor escrito de dois jeitos ("João Silva" e "JOAO SILVA",
// "12.345.678-9" e "123456789")? Segue a mesma normalização usada para gerar o pseudônimo.
func mesmoValor(tipo, a, b string) bool {
	switch tipo {
	case "segredo", "ip":
		return false
	case "email", "host", "pix", "dominio":
		return strings.EqualFold(a, b)
	case "cpf", "cnpj", "pis", "cnh", "telefone", "cep", "cartao", "rg", "conta":
		return canonNum(a) == canonNum(b)
	}
	return NormNome(a) == NormNome(b)
}

func (t *Tabela) Vazia() bool { return len(t.m) == 0 }

// Desmascarar troca pseudônimos por valores reais. Em json=true, o valor real é
// escapado para caber dentro de uma string JSON (uso em input_json_delta).
func (t *Tabela) Desmascarar(s string, emJSON bool) string {
	if t.Vazia() {
		return s
	}
	esc := func(v string) string {
		if !emJSON {
			return v
		}
		b, _ := json.Marshal(v)
		return string(b[1 : len(b)-1])
	}
	var b strings.Builder
	ult := 0
	t.varrer(s, func(ini, fim int) {
		b.WriteString(s[ult:ini])
		b.WriteString(esc(t.m[s[ini:fim]]))
		ult = fim
	})
	if ult > 0 {
		b.WriteString(s[ult:])
		s = b.String()
	}
	if len(t.ipPref) > 0 {
		// outros endereços na mesma sub-rede falsa (ex.: 240.a.b.0/24 escrito pelo modelo)
		s = reIPFalso.ReplaceAllStringFunc(s, func(ip string) string {
			k := ip[:strings.LastIndex(ip, ".")]
			if r, ok := t.ipPref[k]; ok {
				return r + ip[strings.LastIndex(ip, "."):]
			}
			return ip
		})
	}
	return s
}

// segurar: quantos bytes do fim de s podem ser o começo de um pseudônimo.
func (t *Tabela) segurar(s string) int {
	max := t.maxLen - 1
	if max > len(s) {
		max = len(s)
	}
	for l := max; l > 0; l-- {
		if t.podeSerComeco(s[len(s)-l:]) {
			return l
		}
	}
	if len(t.ipPref) > 0 { // fim do texto parecido com o começo de um IP falso: "24", "240.1", ...
		i := len(s)
		for i > 0 && (ehDig(s[i-1]) || s[i-1] == '.') && len(s)-i < 16 {
			i--
		}
		if i < len(s) && s[i] == '2' && (i == 0 || !ehDig(s[i-1])) {
			return len(s) - i
		}
	}
	return 0
}

// podeSerComeco: c pode ser o começo de um pseudônimo ainda incompleto? Para 4 bytes ou
// mais a resposta é aproximada (olha só o começo e o tamanho): pode segurar um texto que no
// fim não era pseudônimo, e ele é entregue no pedaço seguinte.
func (t *Tabela) podeSerComeco(c string) bool {
	for _, k := range t.curtos {
		if len(c) < len(k) && strings.HasPrefix(k, c) {
			return true
		}
	}
	if len(c) < 4 {
		return t.inicio[c]
	}
	for _, n := range t.pref[uint32(c[0])<<24|uint32(c[1])<<16|uint32(c[2])<<8|uint32(c[3])] {
		if int(n) > len(c) {
			return true
		}
	}
	return false
}

// Fluxo desmascara um texto que chega em pedaços, sem nunca entregar um pseudônimo cortado.
type Fluxo struct {
	t      *Tabela
	emJSON bool
	buf    string
}

func (t *Tabela) NovoFluxo(emJSON bool) *Fluxo { return &Fluxo{t: t, emJSON: emJSON} }

// Empurrar recebe um pedaço e devolve o que já pode ser entregue (desmascarado).
func (f *Fluxo) Empurrar(pedaco string) string {
	if f.t.Vazia() {
		return pedaco
	}
	f.buf += pedaco
	// Só pode segurar DEPOIS do último pseudônimo completo; senão o fim de um pseudônimo
	// completo (ex.: "...x2") pode ser confundido com o começo de outro (ex.: IP "2..")
	// e o pseudônimo completo seria cortado ao meio.
	fimCompleto := 0
	f.t.varrer(f.buf, func(_, fim int) { fimCompleto = fim })
	if len(f.t.ipPref) > 0 {
		if ms := reIPFalso.FindAllStringIndex(f.buf, -1); len(ms) > 0 && ms[len(ms)-1][1] > fimCompleto &&
			ms[len(ms)-1][1] < len(f.buf) { // IP falso terminado (algo depois dele) também é completo
			fimCompleto = ms[len(ms)-1][1]
		}
	}
	k := f.t.segurar(f.buf[fimCompleto:])
	pronto := f.buf[:len(f.buf)-k]
	f.buf = f.buf[len(f.buf)-k:]
	return f.t.Desmascarar(pronto, f.emJSON)
}

// Fechar entrega o que sobrou no fim do bloco.
func (f *Fluxo) Fechar() string {
	r := f.t.Desmascarar(f.buf, f.emJSON)
	f.buf = ""
	return r
}
