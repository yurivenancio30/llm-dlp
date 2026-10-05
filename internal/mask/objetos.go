package mask

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// Objetos: nomes de recursos internos (servidor, banco, schema, tabela, coluna, procedure,
// índice, usuário, namespace, serviço, bucket, fila), reconhecidos pela estrutura do conteúdo
// (ver docs/estruturas.md). Este arquivo é a base comum: tipos, pseudônimo, aprendizado com
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

// normObj: o nome sem aspas, colchetes ou crases, em minúsculas (identificadores sem aspas são
// insensíveis à caixa em todos os dialetos de SQL).
func normObj(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		switch {
		case v[0] == '[' && v[len(v)-1] == ']', v[0] == '"' && v[len(v)-1] == '"', v[0] == '`' && v[len(v)-1] == '`':
			v = v[1 : len(v)-1]
		}
	}
	return strings.ToLower(v)
}

// pseudoObjeto: prefixo do tipo + 12 caracteres (60 bits) do HMAC da chave. Estável entre
// conversas e reinícios. Nome em minúsculas recebe o prefixo em minúsculas ("t_..."), para o
// pseudônimo combinar com o estilo do texto; a volta aceita as duas formas.
func (m *Masker) pseudoObjeto(ent, real string) string {
	id := strings.ToLower(m.p.raw("objeto", ent+"\x00"+normObj(real), 8))[:12]
	pref := EntObjeto[ent]
	if pref == "" {
		pref = "OBJ"
	}
	if strings.ToLower(real) == real {
		pref = strings.ToLower(pref)
	}
	return pref + "_" + id
}

// rePseudoObj: um pseudônimo de objeto já pronto (não é mascarado de novo).
var rePseudoObj = regexp.MustCompile(`^(?i:host|db|sch|t|c|proc|idx|usr|ns|svc|bkt|top|repo|org|pkg|dir|acc|obj)_[a-z2-7]{12}$`)

// caraDeIdentificador: o freio que separa um nome de recurso de uma palavra comum. Só nomes
// assim são aprendidos e propagados: têm "_", dígito, ponto, hífen entre partes ou mistura de
// caixa ("tb_pedido", "srv01", "app.config", "svc-pedidos", "contaCorrente"). Ficam de fora
// versões, UUIDs e hashes, que têm dígitos mas não são nomes.
func caraDeIdentificador(v string) bool {
	if len(v) < 3 || len(v) > 128 || reVersaoOuHash.MatchString(v) {
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
	if len(v) < 4 || !m.objPropaga(o.Ent) || !caraDeIdentificador(v) || rePseudoObj.MatchString(v) {
		return
	}
	if publico != nil && publico(v) {
		return
	}
	if !o.Forte && !m.fracos.marcar(normObj(v), o.Regra) {
		return // evidência fraca: mascara no lugar, mas não ensina
	}
	m.conh.aprender(prefTipoObj+o.Ent, v)
	if m.vistos != nil {
		m.vistos.MarcarObj(m.idObj(v), o.Ent, hoje())
	}
}

// idObj: o hash com que o nome fica no vistos.json (65 bits; sem o tipo, que vai no valor).
func (m *Masker) idObj(v string) string {
	return "o:" + strings.ToLower(m.p.raw("visto-objeto", normObj(v), 9))[:13]
}

// hoje: dias desde 1970 (o vistos.json guarda datas assim). Variável para os testes.
var hoje = func() int { return int(time.Now().Unix() / 86400) }

// validadeObj: um nome não visto há mais que isso deixa de ser propagado.
const validadeObj = 90

// acharObjetos roda os leitores em s, devolve os achados (para mascarar) e ensina.
func (m *Masker) acharObjetos(s string, aprende bool, add func(ini, fim int, tipo string)) {
	if !m.cfg.Objetos.Ligado || len(m.leitores) == 0 {
		return
	}
	for _, l := range m.leitores {
		l.Achar(s, func(o ObjAchado) {
			if o.Ini < 0 || o.Fim > len(s) || o.Fim <= o.Ini || !m.objMascara(o.Ent) {
				return
			}
			v := s[o.Ini:o.Fim]
			if rePseudoObj.MatchString(v) || (l.Publico != nil && l.Publico(strings.Trim(v, "[]\"`"))) {
				return
			}
			add(o.Ini, o.Fim, prefTipoObj+o.Ent)
			if aprende {
				m.aprenderObj(o, v, l.Publico)
			}
		})
	}
}

// reTokObj: candidatos a nome aprendido no texto (palavras com "_", "-", "$", "#" no meio).
var reTokObj = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$#]*(?:-[A-Za-z0-9_$#]+)*`)

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
	for _, ix := range reTokObj.FindAllStringIndex(s, -1) {
		v := s[ix[0]:ix[1]]
		if !caraDeIdentificador(v) {
			continue
		}
		if ix[0] > 0 && (s[ix[0]-1] == '-' || ehAlnum(s[ix[0]-1])) {
			continue
		}
		c.mu.RLock()
		tp, ok := c.canon["o:"+strings.ToLower(v)]
		c.mu.RUnlock()
		if ok {
			if ent := strings.TrimPrefix(tp, prefTipoObj); m.objPropaga(ent) {
				add(ix[0], ix[1], tp)
			}
			continue
		}
		if !disco {
			continue
		}
		ent, visto, ok := m.vistos.Obj(m.idObj(v))
		if !ok || d-visto > validadeObj || !m.objPropaga(ent) {
			continue
		}
		add(ix[0], ix[1], prefTipoObj+ent)
		c.aprender(prefTipoObj+ent, v) // volta para a memória
		m.vistos.MarcarObj(m.idObj(v), ent, d)
	}
}

// IdObjeto: o hash com que um nome de objeto fica no vistos.json (para "llm-dlp esquecer").
func (m *Masker) IdObjeto(nome string) string { return m.idObj(nome) }

// UsarLeitores troca os leitores de estrutura (os testes usam leitores próprios).
func (m *Masker) UsarLeitores(ls []Leitor) { m.leitores = ls }

// leitoresPadrao: os leitores de estrutura que vêm ligados.
func leitoresPadrao() []Leitor {
	return []Leitor{
		{Nome: "sql", Achar: acharSQL, Publico: publicoSQL},
		{Nome: "erro", Achar: acharErroObjeto, Publico: publicoSQL},
		{Nome: "conexão", Achar: acharConexoes, Publico: publicoConexao},
		{Nome: "tabela", Achar: acharTabelasObj, Publico: publicoSQL},
	}
}

// LeitoresPadrao: os leitores ligados por padrão (para ferramentas de medição).
func LeitoresPadrao() []Leitor { return leitoresPadrao() }
