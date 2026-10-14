package mask

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
)

// Sent: o que cada texto já enviado à API levou no lugar do dado real, para que ele saia
// igual nos reenvios, inclusive depois de um reinício.
//
// O Claude Code reenvia a conversa inteira a cada mensagem, e a API só aproveita o cache se
// o começo da requisição for idêntico ao da anterior. Se um valor aprendido depois mudasse
// um texto antigo, a conversa inteira seria regravada no cache, sem proteger nada (o texto
// antigo já tinha saído com o valor em claro). Por isso, texto que saiu fica congelado.
//
// O registro é por posição na conversa (ver Position): só o reenvio exato fica congelado.
//
// Em disco fica só: o HMAC (com a chave) da posição mais o texto e, para cada trecho trocado,
// onde ele está no texto, o tipo e o pseudônimo (o que a API recebeu). Nenhum valor real.

type snippet struct {
	Start, End   int
	Type, Pseudo string
}

// Sent guarda os registros em RAM e acrescenta os novos ao arquivo (uma linha JSON cada).
type Sent struct {
	path        string
	fingerprint string // config + versão: se mudar, os registros não valem mais
	mu          sync.Mutex
	reg         map[string]*sentRecord
	usage       int64
	pend        [][]byte // linhas ainda não gravadas
	lines       int      // linhas no arquivo (registros repetidos contam)
}

type sentRecord struct {
	ts    []snippet
	usage int64
}

type sentLine struct {
	ID string  `json:"i,omitempty"`
	T  [][]any `json:"t,omitempty"`
	// só na primeira linha
	Fingerprint string `json:"impressao,omitempty"`
}

// maxSent: teto de textos guardados. Passou disso, sai o décimo usado há mais tempo.
var maxSent = 400_000

// LoadSent lê o arquivo. Se ele for de outra configuração ou de outra versão do
// llm-dlp, recomeça vazio: o texto passa a ser mascarado com as regras atuais (a conversa
// é regravada no cache uma vez).
func LoadSent(path, fingerprint string) (*Sent, error) {
	e := &Sent{path: path, fingerprint: fingerprint, reg: map[string]*sentRecord{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return e, e.rewrite()
	}
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	firstOne, valid := true, false
	for sc.Scan() {
		var l sentLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // linha quebrada (gravação interrompida): o texto é mascarado de novo
		}
		if firstOne {
			firstOne, valid = false, l.Fingerprint == fingerprint
			if !valid {
				break
			}
			continue
		}
		if ts, ok := lineSnippets(l.T); ok && l.ID != "" {
			e.usage++
			e.reg[l.ID] = &sentRecord{ts, e.usage}
			e.lines++
		}
	}
	f.Close()
	if err := sc.Err(); err != nil {
		valid = false
	}
	if !valid {
		e.reg, e.usage = map[string]*sentRecord{}, 0
	}
	if !valid || len(e.reg) > maxSent || e.lines > 2*len(e.reg)+1000 {
		e.prune()
		return e, e.rewrite()
	}
	return e, nil
}

func lineSnippets(t [][]any) ([]snippet, bool) {
	out := make([]snippet, 0, len(t))
	for _, x := range t {
		if len(x) != 4 {
			return nil, false
		}
		ini, ok1 := x[0].(float64)
		end, ok2 := x[1].(float64)
		tp, ok3 := x[2].(string)
		ps, ok4 := x[3].(string)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, false
		}
		out = append(out, snippet{int(ini), int(end), tp, ps})
	}
	return out, true
}

// Lookup devolve os trechos com que o texto foi enviado naquela posição (pelo HMAC).
func (e *Sent) Lookup(id string) ([]snippet, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.reg[id]
	if !ok {
		return nil, false
	}
	e.usage++
	r.usage = e.usage
	return r.ts, true
}

// Record registra um texto que acabou de sair (vai para o arquivo no próximo Save).
func (e *Sent) Record(id string, ts []snippet) {
	t := make([][]any, len(ts))
	for i, x := range ts {
		t[i] = []any{x.Start, x.End, x.Type, x.Pseudo}
	}
	b, _ := json.Marshal(sentLine{ID: id, T: t})
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.reg[id]; ok {
		return
	}
	e.usage++
	e.reg[id] = &sentRecord{ts, e.usage}
	e.pend = append(e.pend, b)
}

// Save acrescenta ao arquivo o que foi registrado desde a última vez. Passou do teto,
// tira os usados há mais tempo e reescreve o arquivo.
func (e *Sent) Save() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.reg) > maxSent {
		e.prune()
		return e.rewrite()
	}
	if len(e.pend) == 0 {
		return nil
	}
	f, err := os.OpenFile(e.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, l := range e.pend {
		w.Write(l)
		w.WriteByte('\n')
	}
	err = w.Flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	e.lines += len(e.pend)
	e.pend = nil
	return err
}

// podar deixa no máximo 90% do teto, ficando os usados mais recentemente (com e.mu travado
// ou antes de o Sent ser compartilhado).
func (e *Sent) prune() {
	if len(e.reg) <= maxSent {
		return
	}
	ids := make([]string, 0, len(e.reg))
	for id := range e.reg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return e.reg[ids[i]].usage > e.reg[ids[j]].usage })
	for _, id := range ids[maxSent*9/10:] {
		delete(e.reg, id)
	}
}

// reescrever grava o arquivo inteiro (cabeçalho + registros, do mais antigo ao mais recente).
func (e *Sent) rewrite() error {
	ids := make([]string, 0, len(e.reg))
	for id := range e.reg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return e.reg[ids[i]].usage < e.reg[ids[j]].usage })
	tmp := e.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	b, _ := json.Marshal(sentLine{Fingerprint: e.fingerprint})
	w.Write(append(b, '\n'))
	for _, id := range ids {
		r := e.reg[id]
		t := make([][]any, len(r.ts))
		for i, x := range r.ts {
			t[i] = []any{x.Start, x.End, x.Type, x.Pseudo}
		}
		b, _ := json.Marshal(sentLine{ID: id, T: t})
		w.Write(append(b, '\n'))
	}
	err = w.Flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	e.lines, e.pend = len(ids), nil
	return os.Rename(tmp, e.path)
}
