package mask

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Vistos: os mesmos valores, guardados em disco só como hash, para valer depois de reiniciar.

// Vistos guarda em disco, só como hash (HMAC com a chave), os valores contextuais já
// mascarados e o tipo de cada um.
type Vistos struct {
	path string
	mu   sync.RWMutex
	ids  map[string]string // id -> tipo
	tam  map[int]bool      // tamanhos das senhas guardadas (filtro: só testa pedaços desses tamanhos)
	n    map[string]int    // quantos de cada tipo
	nObj int               // quantos nomes de objeto
	sujo bool
	// semTam: há senhas guardadas sem o tamanho (arquivo antigo)
	semTam   bool
	salvo    time.Time
	agendado bool
}

// prefTam: os tamanhos vão no mesmo arquivo, como chaves "tam:13"
const prefTam = "tam:"

var intervaloSalvar = 5 * time.Second

func (v *Vistos) TemTamanho(n int) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.semTam || v.tam[n]
}

func (v *Vistos) MarcarTamanho(n int) {
	v.mu.Lock()
	if !v.tam[n] {
		v.tam[n], v.sujo = true, true
		v.ids[prefTam+strconv.Itoa(n)] = "tam"
	}
	v.mu.Unlock()
}

func CarregarVistos(path string) (*Vistos, error) {
	v := &Vistos{path: path, ids: map[string]string{}, n: map[string]int{}, tam: map[int]bool{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &v.ids) != nil {
		v.ids = map[string]string{} // formato antigo (lista): recomeça; os valores são reaprendidos
	}
	for id, t := range v.ids {
		if n, err := strconv.Atoi(strings.TrimPrefix(id, prefTam)); err == nil && strings.HasPrefix(id, prefTam) {
			v.tam[n] = true
			continue
		}
		v.n[tipoBase(t)]++
		if strings.HasPrefix(t, "obj.") {
			v.nObj++
		}
	}
	// arquivo de antes de os tamanhos serem guardados: sem o filtro (testa todos os pedaços)
	v.semTam = v.n["segredo"] > 0 && len(v.tam) == 0
	return v, nil
}

func (v *Vistos) Vazio() bool { v.mu.RLock(); defer v.mu.RUnlock(); return len(v.ids) == 0 }

func (v *Vistos) Tipo(id string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	t, ok := v.ids[id]
	return t, ok
}

// Tem diz se há algum valor desse tipo guardado.
func (v *Vistos) Tem(tipo string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.n[tipo] > 0
}

// Objetos (ver objetos.go): o valor guarda o tipo e duas datas, em dias desde 1970:
// "obj.tabela|<aprendido>|<visto pela última vez>". O teto é separado, para os nomes de
// objeto não expulsarem senhas e documentos.
const maxObjetos = 500_000

func tipoBase(t string) string {
	if i := strings.IndexByte(t, '|'); i >= 0 {
		return t[:i]
	}
	return t
}

func (v *Vistos) TemObj() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.nObj > 0
}

// Obj devolve o tipo de entidade e o dia em que o nome foi visto pela última vez.
func (v *Vistos) Obj(id string) (ent string, visto int, ok bool) {
	v.mu.RLock()
	t, ok := v.ids[id]
	v.mu.RUnlock()
	if !ok {
		return "", 0, false
	}
	ent, _, visto, ok = lerObj(t)
	return ent, visto, ok
}

func lerObj(t string) (ent string, aprendido, visto int, ok bool) {
	p := strings.Split(t, "|")
	if len(p) != 3 || !strings.HasPrefix(p[0], "obj.") {
		return "", 0, 0, false
	}
	a, e1 := strconv.Atoi(p[1])
	b, e2 := strconv.Atoi(p[2])
	return p[0][4:], a, b, e1 == nil && e2 == nil
}

// MarcarObj guarda o nome (ou atualiza o dia em que foi visto, no máximo uma vez por dia).
func (v *Vistos) MarcarObj(id, ent string, dia int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if t, ok := v.ids[id]; ok {
		e, a, visto, ok := lerObj(t)
		if !ok || visto >= dia {
			return
		}
		v.ids[id], v.sujo = "obj."+e+"|"+strconv.Itoa(a)+"|"+strconv.Itoa(dia), true
		return
	}
	if v.nObj >= maxObjetos {
		v.podarObj()
	}
	v.ids[id], v.sujo = "obj."+ent+"|"+strconv.Itoa(dia)+"|"+strconv.Itoa(dia), true
	v.n["obj."+ent]++
	v.nObj++
}

// podarObj tira o décimo dos nomes de objeto vistos há mais tempo (com v.mu travado).
func (v *Vistos) podarObj() {
	dias := make([]int, 0, v.nObj)
	for _, t := range v.ids {
		if _, _, d, ok := lerObj(t); ok {
			dias = append(dias, d)
		}
	}
	sort.Ints(dias)
	corte := dias[len(dias)/10]
	for id, t := range v.ids {
		if e, _, d, ok := lerObj(t); ok && d <= corte {
			delete(v.ids, id)
			v.n["obj."+e]--
			v.nObj--
		}
	}
}

// Esquecer apaga os nomes de objeto que satisfazem f (tipo, aprendido, visto) e devolve
// quantos. id != "" apaga só esse.
func (v *Vistos) Esquecer(id string, f func(ent string, aprendido, visto int) bool) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for k, t := range v.ids {
		if id != "" && k != id {
			continue
		}
		if e, a, d, ok := lerObj(t); ok && f(e, a, d) {
			delete(v.ids, k)
			v.n["obj."+e]--
			v.nObj--
			n++
		}
	}
	if n > 0 {
		v.sujo = true
	}
	return n
}

// Objetos: para cada nome de objeto guardado, chama f com o tipo e as datas.
func (v *Vistos) Objetos(f func(ent string, aprendido, visto int)) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	for _, t := range v.ids {
		if e, a, d, ok := lerObj(t); ok {
			f(e, a, d)
		}
	}
}

// Salvar grava agora (comandos de linha; o proxy usa SalvarSeSujo).
func (v *Vistos) Salvar() error {
	v.mu.Lock()
	v.salvo = time.Time{}
	v.mu.Unlock()
	return v.SalvarSeSujo()
}

// maxVistos: teto dos hashes guardados (~40 bytes cada). Passou disso, um décimo sai, ao
// acaso; esses valores voltam a depender da palavra ao lado até serem vistos de novo.
const maxVistos = 500_000

func (v *Vistos) Marcar(id, tipo string) {
	v.mu.Lock()
	if _, ok := v.ids[id]; !ok {
		if len(v.ids)-v.nObj >= maxVistos {
			novo, n := make(map[string]string, maxVistos), map[string]int{}
			for k, t := range v.ids { // a ordem de um mapa em Go é aleatória
				if strings.HasPrefix(k, prefTam) || strings.HasPrefix(t, "obj.") {
					novo[k] = t
					continue
				}
				if len(novo) >= maxVistos*9/10 {
					continue
				}
				novo[k] = t
				n[t]++
			}
			for k, c := range v.n {
				if strings.HasPrefix(k, "obj.") {
					n[k] = c
				}
			}
			v.ids, v.n = novo, n
		}
		v.ids[id], v.sujo = tipo, true
		v.n[tipo]++
	}
	v.mu.Unlock()
}

// SalvarSeSujo grava o arquivo se houve novidade desde a última gravação.
// Com muitos hashes a gravação custa (≈0,3 s com 250 mil), então acontece no máximo uma
// vez a cada intervaloSalvar; o que ficou pendente é gravado por um temporizador.
func (v *Vistos) SalvarSeSujo() error {
	v.mu.Lock()
	if !v.sujo {
		v.mu.Unlock()
		return nil
	}
	if falta := intervaloSalvar - time.Since(v.salvo); falta > 0 {
		if !v.agendado {
			v.agendado = true
			time.AfterFunc(falta, func() {
				v.mu.Lock()
				v.agendado = false
				v.mu.Unlock()
				v.SalvarSeSujo()
			})
		}
		v.mu.Unlock()
		return nil
	}
	v.salvo = time.Now()
	b, _ := json.Marshal(v.ids)
	v.sujo = false
	v.mu.Unlock()
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}
