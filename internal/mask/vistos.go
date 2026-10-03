package mask

import (
	"encoding/json"
	"errors"
	"os"
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
		v.n[t]++
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

// maxVistos: teto dos hashes guardados (~40 bytes cada). Passou disso, um décimo sai, ao
// acaso; esses valores voltam a depender da palavra ao lado até serem vistos de novo.
const maxVistos = 500_000

func (v *Vistos) Marcar(id, tipo string) {
	v.mu.Lock()
	if _, ok := v.ids[id]; !ok {
		if len(v.ids) >= maxVistos {
			novo, n := make(map[string]string, maxVistos), map[string]int{}
			for k, t := range v.ids { // a ordem de um mapa em Go é aleatória
				if strings.HasPrefix(k, prefTam) {
					novo[k] = t
					continue
				}
				if len(novo) >= maxVistos*9/10 {
					continue
				}
				novo[k] = t
				n[t]++
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
