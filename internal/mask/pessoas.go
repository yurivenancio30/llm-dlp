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

// Pessoas: registro (só hashes) de nomes, e-mails e códigos de pessoas conhecidas.

// Pessoas guarda, SÓ COMO HASH (HMAC com a chave), os nomes, e-mails e códigos de
// pessoas conhecidas e a qual pessoa cada um pertence. Serve para (1) achar um nome
// solto no texto e (2) dar o mesmo identificador ao nome e ao e-mail da mesma pessoa.
// Nenhum valor real fica no arquivo.
type Pessoas struct {
	path      string
	mu        sync.RWMutex
	sujo      bool
	salvo     time.Time
	codCache  sync.Map // palavra -> é código de alguém? (só em RAM)
	nCodCache atomic.Int64
	D         struct {
		Nomes   map[string]string `json:"nomes"`   // id(nome) -> pid
		Emails  map[string]string `json:"emails"`  // id(email) -> pid
		Codigos map[string]string `json:"codigos"` // id(código) -> pid
		// id(primeiro nome): filtro barato para achar nomes em qualquer grafia
		// ("joão silva", "JOAO SILVA") sem testar toda sequência de palavras do texto
		Primeiros map[string]bool `json:"primeiros"`
		// tamanhos dos códigos de usuário: filtro barato (só palavras desses tamanhos são
		// conferidas). Vazio em registros antigos: aí todas são conferidas.
		TamCodigos map[int]bool `json:"tam_codigos,omitempty"`
	}
}

func CarregarPessoas(path string) (*Pessoas, error) {
	ps := &Pessoas{path: path}
	ps.D.Nomes, ps.D.Emails, ps.D.Codigos = map[string]string{}, map[string]string{}, map[string]string{}
	ps.D.Primeiros = map[string]bool{}
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
	return ps, nil
}

func (ps *Pessoas) Salvar() error {
	ps.mu.RLock()
	b, _ := json.Marshal(ps.D)
	ps.mu.RUnlock()
	tmp := ps.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ps.path)
}

// SalvarSeSujo grava o registro se entrou alguém novo (no máximo uma vez a cada 5 s).
func (ps *Pessoas) SalvarSeSujo() error {
	ps.mu.Lock()
	if !ps.sujo || time.Since(ps.salvo) < intervaloSalvar {
		ps.mu.Unlock()
		return nil
	}
	ps.sujo, ps.salvo = false, time.Now()
	ps.mu.Unlock()
	return ps.Salvar()
}

// EhCodigo: v é o código de alguém do registro? Guarda a resposta por palavra (as mesmas
// palavras se repetem muito, e a conta do hash é a parte cara).
func (ps *Pessoas) EhCodigo(p *Pseudo, v string) bool {
	// mesma regra da importação, para registros feitos antes dela: código curto ou sem dígito
	// não é procurado no texto
	if len(v) < 4 || !temDigito(v) {
		return false
	}
	ps.mu.RLock()
	fora := len(ps.D.TamCodigos) > 0 && !ps.D.TamCodigos[len(v)]
	ps.mu.RUnlock()
	if fora {
		return false
	}
	if r, ok := ps.codCache.Load(v); ok {
		return r.(bool)
	}
	r := ps.PID(p, "codigo", NormNome(v)) != ""
	if ps.nCodCache.Add(1) > 200_000 {
		ps.codCache.Clear()
		ps.nCodCache.Store(0)
	}
	ps.codCache.Store(v, r)
	return r
}

func (ps *Pessoas) TemCodigos() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.D.Codigos) > 0
}

func (ps *Pessoas) Total() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.D.Nomes)
}

// Importar registra uma pessoa. Valores vazios são ignorados. Nomes com uma palavra só
// não entram na detecção (costumam ser times ou sistemas, não pessoas).
func (ps *Pessoas) Importar(p *Pseudo, nome, email, codigo string) string {
	nome, email, codigo = NormNome(nome), strings.ToLower(strings.TrimSpace(email)), NormNome(codigo)
	// código curto ou sem dígito ("TBD", "N/A", "SIM") não identifica ninguém e, procurado no
	// texto, mascararia palavras comuns
	if len(codigo) < 4 || !temDigito(codigo) {
		codigo = ""
	}
	if codigo != "" {
		ps.codCache.Clear()
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
	if codigo != "" {
		if ps.D.TamCodigos == nil && len(ps.D.Codigos) == 0 {
			ps.D.TamCodigos = map[int]bool{}
		}
		if ps.D.TamCodigos != nil && !ps.D.TamCodigos[len(codigo)] {
			ps.D.TamCodigos[len(codigo)], ps.sujo = true, true
		}
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Codigos, p.ID("codigo", codigo)})
	}
	var variantes []string
	if partes := strings.Fields(nome); len(partes) >= 2 {
		variantes = append(variantes, nome)
		if len(partes) >= 3 {
			variantes = append(variantes, partes[0]+" "+partes[len(partes)-1])
		}
	}
	for _, v := range variantes {
		ids = append(ids, struct {
			m  map[string]string
			id string
		}{ps.D.Nomes, p.ID("nome", v)})
		if ps.D.Primeiros == nil {
			ps.D.Primeiros = map[string]bool{}
		}
		ps.D.Primeiros[p.ID("primeiro", strings.Fields(v)[0])] = true
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
			ps.sujo = true
		}
	}
	return pid
}

// PID devolve o identificador da pessoa dona do valor, ou "" se desconhecida.
func (ps *Pessoas) PID(p *Pseudo, tipo, valorNorm string) string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	switch tipo {
	case "email":
		return ps.D.Emails[p.ID("email", valorNorm)]
	case "nome":
		return ps.D.Nomes[p.ID("nome", valorNorm)]
	case "codigo":
		return ps.D.Codigos[p.ID("codigo", valorNorm)]
	}
	return ""
}

var conectores = map[string]bool{"da": true, "de": true, "do": true, "das": true, "dos": true, "e": true}

type palavra struct {
	ini, fim int
	maiusc   bool
	conector bool
}

// AcharNomes procura sequências de 2 a 6 palavras com cara de nome ("João da Silva",
// "JOAO SILVA") e confere o hash de cada uma no registro.
func (ps *Pessoas) AcharNomes(s string, p *Pseudo) []Achado {
	if ps.Total() == 0 {
		return nil
	}
	var ws []palavra
	ini := -1
	for i, r := range s + " " {
		letra := unicode.IsLetter(r) || ((r == '\'' || r == '-') && ini >= 0)
		if letra && ini < 0 {
			ini = i
		} else if !letra && ini >= 0 {
			w := s[ini:i]
			r0 := []rune(w)[0]
			ws = append(ws, palavra{ini, i, unicode.IsUpper(r0), conectores[w]})
			ini = -1
		}
	}
	ps.mu.RLock()
	porPrimeiro := len(ps.D.Primeiros) > 0
	ps.mu.RUnlock()
	inicio := func(i int) bool {
		if !porPrimeiro { // registro antigo, sem o filtro: só sequências com iniciais maiúsculas
			return ws[i].maiusc
		}
		if ws[i].fim-ws[i].ini < 2 {
			return false
		}
		ps.mu.RLock()
		defer ps.mu.RUnlock()
		return ps.D.Primeiros[p.ID("primeiro", NormNome(s[ws[i].ini:ws[i].fim]))]
	}
	var out []Achado
	for i := 0; i < len(ws); i++ {
		if !inicio(i) {
			continue
		}
		achou := 0
		for n := 6; n >= 2 && achou == 0; n-- {
			j := i + n - 1
			if j >= len(ws) || (!porPrimeiro && !ws[j].maiusc) || !mesmaLinha(s, ws, i, j) {
				continue
			}
			ok := true
			for k := i + 1; k < j && !porPrimeiro; k++ {
				if !ws[k].maiusc && !ws[k].conector {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			if ps.PID(p, "nome", NormNome(s[ws[i].ini:ws[j].fim])) != "" {
				out = append(out, Achado{ws[i].ini, ws[j].fim, "nome", s[ws[i].ini:ws[j].fim]})
				achou = n
			}
		}
		if achou > 0 {
			i += achou - 1
		}
	}
	return out
}

// mesmaLinha: entre as palavras i..j só há espaços simples (não quebra linha nem pontuação).
func mesmaLinha(s string, ws []palavra, i, j int) bool {
	for k := i; k < j; k++ {
		gap := s[ws[k].fim:ws[k+1].ini]
		if strings.TrimLeft(gap, " \t") != "" {
			return false
		}
	}
	return true
}
