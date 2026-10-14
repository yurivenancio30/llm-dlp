package mask

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// People: registro (só hashes) de nomes, e-mails e códigos de pessoas conhecidas.

// People guarda, SÓ COMO HASH (HMAC com a chave), os nomes, e-mails e códigos de
// pessoas conhecidas e a qual pessoa cada um pertence. Serve para (1) achar um nome
// solto no texto e (2) dar o mesmo identificador ao nome e ao e-mail da mesma pessoa.
// Nenhum valor real fica no arquivo.
type People struct {
	path       string
	mu         sync.RWMutex
	dirty      bool
	saved      time.Time
	codeCache  sync.Map // palavra -> é código de alguém? (só em RAM)
	nCodeCache atomic.Int64
	disk       fileFingerprint // o arquivo como este processo o deixou (ver Seen.disco)
	D          struct {
		Names  map[string]string `json:"nomes"`   // id(nome) -> pid
		Emails map[string]string `json:"emails"`  // id(email) -> pid
		Codes  map[string]string `json:"codigos"` // id(código) -> pid
		// id(primeiro nome): filtro barato para achar nomes em qualquer grafia
		// ("joão silva", "JOAO SILVA") sem testar toda sequência de palavras do texto
		FirstNames map[string]bool `json:"primeiros"`
		// tamanhos dos códigos de usuário: filtro barato (só palavras desses tamanhos são
		// conferidas). IsEmpty em registros antigos: aí todas são conferidas.
		CodeLengths map[int]bool `json:"tam_codigos,omitempty"`
	}
}

func LoadPeople(path string) (*People, error) {
	ps := &People{path: path}
	ps.D.Names, ps.D.Emails, ps.D.Codes = map[string]string{}, map[string]string{}, map[string]string{}
	ps.D.FirstNames = map[string]bool{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ps, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &ps.D); err != nil {
		return nil, err
	}
	ps.disk = fingerprintOf(path)
	return ps, nil
}

// Save grava o registro. Se outro processo gravou depois deste (o importar-pessoas com o
// proxy no ar; a versão nova durante a troca), junta antes o que só o outro tem: o registro só
// cresce, e uma gravação não apaga a outra.
func (ps *People) Save() error {
	ps.mu.Lock()
	ps.mergeFromDisk()
	b, _ := json.Marshal(ps.D)
	ps.mu.Unlock()
	imp, err := writeSwapping(ps.path, b)
	if err != nil {
		return err
	}
	ps.mu.Lock()
	ps.disk = imp
	ps.mu.Unlock()
	return nil
}

// mergeFromDisk: com ps.mu travado. Um id que os dois têm fica com a pessoa deste processo.
func (ps *People) mergeFromDisk() {
	if fingerprintOf(ps.path) == ps.disk {
		return
	}
	b, err := os.ReadFile(ps.path)
	if err != nil {
		return
	}
	var other People
	if json.Unmarshal(b, &other.D) != nil {
		return
	}
	// sem tamanhos = registro antigo, que confere toda palavra: se um dos lados é assim, o
	// resultado fica sem o filtro (conferir a mais nunca deixa um código passar)
	noFilter := len(ps.D.Codes) > 0 && len(ps.D.CodeLengths) == 0 ||
		len(other.D.Codes) > 0 && len(other.D.CodeLengths) == 0
	join := func(mine *map[string]string, its map[string]string) {
		for id, pid := range its {
			if *mine == nil {
				*mine = map[string]string{}
			}
			if _, ok := (*mine)[id]; !ok {
				(*mine)[id] = pid
			}
		}
	}
	join(&ps.D.Names, other.D.Names)
	join(&ps.D.Emails, other.D.Emails)
	join(&ps.D.Codes, other.D.Codes)
	for id := range other.D.FirstNames {
		if ps.D.FirstNames == nil {
			ps.D.FirstNames = map[string]bool{}
		}
		ps.D.FirstNames[id] = true
	}
	if noFilter {
		ps.D.CodeLengths = nil
	} else {
		for n := range other.D.CodeLengths {
			if ps.D.CodeLengths == nil {
				ps.D.CodeLengths = map[int]bool{}
			}
			ps.D.CodeLengths[n] = true
		}
	}
	ps.codeCache.Clear() // códigos novos: "não é código" guardado antes pode ter deixado de valer
	ps.nCodeCache.Store(0)
}

// SaveNow grava já o que estiver pendente, sem esperar o intervalo (na saída do proxy).
func (ps *People) SaveNow() error {
	ps.mu.Lock()
	dirty := ps.dirty
	ps.dirty = false
	ps.mu.Unlock()
	if !dirty {
		return nil
	}
	return ps.Save()
}

// SaveIfDirty grava o registro se entrou alguém novo (no máximo uma vez a cada 5 s).
func (ps *People) SaveIfDirty() error {
	ps.mu.Lock()
	if !ps.dirty || time.Since(ps.saved) < saveInterval {
		ps.mu.Unlock()
		return nil
	}
	ps.dirty, ps.saved = false, time.Now()
	ps.mu.Unlock()
	return ps.Save()
}

// IsCode: v é o código de alguém do registro? Guarda a resposta por palavra (as mesmas
// palavras se repetem muito, e a conta do hash é a parte cara).
func (ps *People) IsCode(p *Pseudo, v string) bool {
	// mesma regra da importação, para registros feitos antes dela: código curto ou sem dígito
	// não é procurado no texto
	if len(v) < 4 || !hasDigit(v) {
		return false
	}
	ps.mu.RLock()
	outside := len(ps.D.CodeLengths) > 0 && !ps.D.CodeLengths[len(v)]
	ps.mu.RUnlock()
	if outside {
		return false
	}
	if r, ok := ps.codeCache.Load(v); ok {
		return r.(bool)
	}
	r := ps.PID(p, "codigo", NormName(v)) != ""
	if ps.nCodeCache.Add(1) > 200_000 {
		ps.codeCache.Clear()
		ps.nCodeCache.Store(0)
	}
	ps.codeCache.Store(v, r)
	return r
}

func (ps *People) HasCodes() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.D.Codes) > 0
}

func (ps *People) Total() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.D.Names)
}

// Import registra uma pessoa. Values vazios são ignorados. Names com uma palavra só
// não entram na detecção (costumam ser times ou sistemas, não pessoas).
func (ps *People) Import(p *Pseudo, name, email, codeValue string) string {
	name, email, codeValue = NormName(name), strings.ToLower(strings.TrimSpace(email)), NormName(codeValue)
	// código curto ou sem dígito ("TBD", "N/A", "SIM") não identifica ninguém e, procurado no
	// texto, mascararia palavras comuns
	if len(codeValue) < 4 || !hasDigit(codeValue) {
		codeValue = ""
	}
	if codeValue != "" {
		ps.codeCache.Clear()
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	var ids []struct {
		m  map[string]string
		id string
	}
	if email != "" {
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Emails, p.ID("email", email)})
	}
	if codeValue != "" {
		if ps.D.CodeLengths == nil && len(ps.D.Codes) == 0 {
			ps.D.CodeLengths = map[int]bool{}
		}
		if ps.D.CodeLengths != nil && !ps.D.CodeLengths[len(codeValue)] {
			ps.D.CodeLengths[len(codeValue)], ps.dirty = true, true
		}
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Codes, p.ID("codigo", codeValue)})
	}
	var variants []string
	if parts := strings.Fields(name); len(parts) >= 2 {
		variants = append(variants, name)
		if len(parts) >= 3 {
			variants = append(variants, parts[0]+" "+parts[len(parts)-1])
		}
	}
	for _, v := range variants {
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Names, p.ID("nome", v)})
		if ps.D.FirstNames == nil {
			ps.D.FirstNames = map[string]bool{}
		}
		ps.D.FirstNames[p.ID("primeiro", strings.Fields(v)[0])] = true
	}
	if len(ids) == 0 {
		return ""
	}
	pid := ""
	for _, x := range ids {
		if v, ok := x.m[x.id]; ok {
			pid = v
			break
		}
	}
	if pid == "" {
		pid = ids[0].id
	}
	for _, x := range ids {
		if _, ok := x.m[x.id]; !ok {
			x.m[x.id] = pid
			ps.dirty = true
		}
	}
	return pid
}

// PID devolve o identificador da pessoa dona do valor, ou "" se desconhecida.
func (ps *People) PID(p *Pseudo, typ, normValue string) string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	switch typ {
	case "email":
		return ps.D.Emails[p.ID("email", normValue)]
	case "nome":
		return ps.D.Names[p.ID("nome", normValue)]
	case "codigo":
		return ps.D.Codes[p.ID("codigo", normValue)]
	}
	return ""
}

var connectors = map[string]bool{"da": true, "de": true, "do": true, "das": true, "dos": true, "e": true}

type word struct {
	ini, end  int
	upperCase bool
	connector bool
}

// FindNames procura sequências de 2 a 6 palavras com cara de nome ("João da Silva",
// "JOAO SILVA") e confere o hash de cada uma no registro.
func (ps *People) FindNames(s string, p *Pseudo) []Finding {
	if ps.Total() == 0 {
		return nil
	}
	var ws []word
	ini := -1
	for i, r := range s + " " {
		letter := unicode.IsLetter(r) || ((r == '\'' || r == '-') && ini >= 0)
		if letter && ini < 0 {
			ini = i
		} else if !letter && ini >= 0 {
			w := s[ini:i]
			r0 := []rune(w)[0]
			ws = append(ws, word{ini, i, unicode.IsUpper(r0), connectors[w]})
			ini = -1
		}
	}
	ps.mu.RLock()
	byFirst := len(ps.D.FirstNames) > 0
	ps.mu.RUnlock()
	start := func(i int) bool {
		if !byFirst { // registro antigo, sem o filtro: só sequências com iniciais maiúsculas
			return ws[i].upperCase
		}
		if ws[i].end-ws[i].ini < 2 {
			return false
		}
		ps.mu.RLock()
		defer ps.mu.RUnlock()
		return ps.D.FirstNames[p.ID("primeiro", NormName(s[ws[i].ini:ws[i].end]))]
	}
	var out []Finding
	for i := 0; i < len(ws); i++ {
		if !start(i) {
			continue
		}
		foundIt := 0
		for n := 6; n >= 2 && foundIt == 0; n-- {
			j := i + n - 1
			if j >= len(ws) || (!byFirst && !ws[j].upperCase) || !sameLine(s, ws, i, j) {
				continue
			}
			ok := true
			for k := i + 1; k < j && !byFirst; k++ {
				if !ws[k].upperCase && !ws[k].connector {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			if ps.PID(p, "nome", NormName(s[ws[i].ini:ws[j].end])) != "" {
				out = append(out, Finding{ws[i].ini, ws[j].end, "nome", s[ws[i].ini:ws[j].end]})
				foundIt = n
			}
		}
		if foundIt > 0 {
			i += foundIt - 1
		}
	}
	return out
}

// sameLine: entre as palavras i..j só há espaços simples (não quebra linha nem pontuação).
func sameLine(s string, ws []word, i, j int) bool {
	for k := i; k < j; k++ {
		gap := s[ws[k].end:ws[k+1].ini]
		if strings.TrimLeft(gap, " \t") != "" {
			return false
		}
	}
	return true
}
