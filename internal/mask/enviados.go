package mask

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
)

// Enviados: o que cada texto já enviado à API levou no lugar do dado real, para que ele saia
// igual nos reenvios, inclusive depois de um reinício.
//
// O Claude Code reenvia a conversa inteira a cada mensagem, e a API só aproveita o cache se
// o começo da requisição for idêntico ao da anterior. Se um valor aprendido depois mudasse
// um texto antigo, a conversa inteira seria regravada no cache, sem proteger nada (o texto
// antigo já tinha saído com o valor em claro). Por isso, texto que saiu fica congelado.
//
// Em disco fica só: o HMAC do texto (com a chave) e, para cada trecho trocado, a posição, o
// tipo e o pseudônimo (o que a API recebeu). Nenhum valor real.

type trecho struct {
	Ini, Fim     int
	Tipo, Pseudo string
}

// Enviados guarda os registros em RAM e acrescenta os novos ao arquivo (uma linha JSON cada).
type Enviados struct {
	path      string
	impressao string // config + versão: se mudar, os registros não valem mais
	mu        sync.Mutex
	reg       map[string]*regEnviado
	uso       int64
	pend      [][]byte // linhas ainda não gravadas
	linhas    int      // linhas no arquivo (registros repetidos contam)
}

type regEnviado struct {
	ts  []trecho
	uso int64
}

type linhaEnviado struct {
	ID string  `json:"i,omitempty"`
	T  [][]any `json:"t,omitempty"`
	// só na primeira linha
	Impressao string `json:"impressao,omitempty"`
}

// maxEnviados: teto de textos guardados. Passou disso, sai o décimo usado há mais tempo.
var maxEnviados = 400_000

// CarregarEnviados lê o arquivo. Se ele for de outra configuração ou de outra versão do
// llm-dlp, recomeça vazio: o texto passa a ser mascarado com as regras atuais (a conversa
// é regravada no cache uma vez).
func CarregarEnviados(path, impressao string) (*Enviados, error) {
	e := &Enviados{path: path, impressao: impressao, reg: map[string]*regEnviado{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return e, e.reescrever()
	}
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	primeira, valido := true, false
	for sc.Scan() {
		var l linhaEnviado
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // linha quebrada (gravação interrompida): o texto é mascarado de novo
		}
		if primeira {
			primeira, valido = false, l.Impressao == impressao
			if !valido {
				break
			}
			continue
		}
		if ts, ok := trechosDeLinha(l.T); ok && l.ID != "" {
			e.uso++
			e.reg[l.ID] = &regEnviado{ts, e.uso}
			e.linhas++
		}
	}
	f.Close()
	if err := sc.Err(); err != nil {
		valido = false
	}
	if !valido {
		e.reg, e.uso = map[string]*regEnviado{}, 0
	}
	if !valido || len(e.reg) > maxEnviados || e.linhas > 2*len(e.reg)+1000 {
		e.podar()
		return e, e.reescrever()
	}
	return e, nil
}

func trechosDeLinha(t [][]any) ([]trecho, bool) {
	out := make([]trecho, 0, len(t))
	for _, x := range t {
		if len(x) != 4 {
			return nil, false
		}
		ini, ok1 := x[0].(float64)
		fim, ok2 := x[1].(float64)
		tp, ok3 := x[2].(string)
		ps, ok4 := x[3].(string)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, false
		}
		out = append(out, trecho{int(ini), int(fim), tp, ps})
	}
	return out, true
}

// Buscar devolve os trechos com que o texto (pelo HMAC) foi enviado.
func (e *Enviados) Buscar(id string) ([]trecho, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.reg[id]
	if !ok {
		return nil, false
	}
	e.uso++
	r.uso = e.uso
	return r.ts, true
}

// Gravar registra um texto que acabou de sair (vai para o arquivo no próximo Salvar).
func (e *Enviados) Gravar(id string, ts []trecho) {
	t := make([][]any, len(ts))
	for i, x := range ts {
		t[i] = []any{x.Ini, x.Fim, x.Tipo, x.Pseudo}
	}
	b, _ := json.Marshal(linhaEnviado{ID: id, T: t})
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.reg[id]; ok {
		return
	}
	e.uso++
	e.reg[id] = &regEnviado{ts, e.uso}
	e.pend = append(e.pend, b)
}

// Salvar acrescenta ao arquivo o que foi registrado desde a última vez. Passou do teto,
// tira os usados há mais tempo e reescreve o arquivo.
func (e *Enviados) Salvar() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.reg) > maxEnviados {
		e.podar()
		return e.reescrever()
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
	e.linhas += len(e.pend)
	e.pend = nil
	return err
}

// podar deixa no máximo 90% do teto, ficando os usados mais recentemente (com e.mu travado
// ou antes de o Enviados ser compartilhado).
func (e *Enviados) podar() {
	if len(e.reg) <= maxEnviados {
		return
	}
	ids := make([]string, 0, len(e.reg))
	for id := range e.reg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return e.reg[ids[i]].uso > e.reg[ids[j]].uso })
	for _, id := range ids[maxEnviados*9/10:] {
		delete(e.reg, id)
	}
}

// reescrever grava o arquivo inteiro (cabeçalho + registros, do mais antigo ao mais recente).
func (e *Enviados) reescrever() error {
	ids := make([]string, 0, len(e.reg))
	for id := range e.reg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return e.reg[ids[i]].uso < e.reg[ids[j]].uso })
	tmp := e.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	b, _ := json.Marshal(linhaEnviado{Impressao: e.impressao})
	w.Write(append(b, '\n'))
	for _, id := range ids {
		r := e.reg[id]
		t := make([][]any, len(r.ts))
		for i, x := range r.ts {
			t[i] = []any{x.Ini, x.Fim, x.Tipo, x.Pseudo}
		}
		b, _ := json.Marshal(linhaEnviado{ID: id, T: t})
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
	e.linhas, e.pend = len(ids), nil
	return os.Rename(tmp, e.path)
}
