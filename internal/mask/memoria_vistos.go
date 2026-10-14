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

// Seen: os mesmos valores, guardados em disco só como hash, para valer depois de reiniciar.

// Seen guarda em disco, só como hash (HMAC com a chave), os valores contextuais já
// mascarados e o tipo de cada um.
type Seen struct {
	path  string
	mu    sync.RWMutex
	ids   map[string]string // id -> tipo
	size  map[int]bool      // tamanhos das senhas guardadas (filtro: só testa pedaços desses tamanhos)
	n     map[string]int    // quantos de cada tipo
	nObj  int               // quantos nomes de objeto
	dirty bool
	// noSize: há senhas guardadas sem o tamanho (arquivo antigo)
	noSize    bool
	saved     time.Time
	scheduled bool
	// disco: o arquivo como este processo o deixou (leu ou gravou). Se mudou, outro processo
	// gravou (a versão nova durante a troca, ver atualizar; um comando): o que ele aprendeu é
	// juntado antes de gravar, e uma gravação não apaga a outra.
	disk fileFingerprint
}

type fileFingerprint struct {
	mod  time.Time
	size int64
}

func fingerprintOf(path string) fileFingerprint {
	st, err := os.Stat(path)
	if err != nil {
		return fileFingerprint{}
	}
	return fileFingerprint{st.ModTime(), st.Size()}
}

// writeSwapping grava b num arquivo temporário deste processo e troca pelo arquivo (dois
// processos gravando ao mesmo tempo não misturam um temporário com o outro). Devolve a
// impressão do que gravou (a troca de nome não muda data nem tamanho).
func writeSwapping(path string, b []byte) (fileFingerprint, error) {
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		os.Remove(tmp)
		return fileFingerprint{}, err
	}
	imp := fingerprintOf(tmp)
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fileFingerprint{}, err
	}
	return imp, nil
}

// sizePrefix: os tamanhos vão no mesmo arquivo, como chaves "tam:13"
const sizePrefix = "tam:"

var saveInterval = 5 * time.Second

func (v *Seen) HasSize(n int) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.noSize || v.size[n]
}

func (v *Seen) MarkSize(n int) {
	v.mu.Lock()
	if !v.size[n] {
		v.size[n], v.dirty = true, true
		v.ids[sizePrefix+strconv.Itoa(n)] = "tam"
	}
	v.mu.Unlock()
}

func LoadSeen(path string) (*Seen, error) {
	v := &Seen{path: path, ids: map[string]string{}, n: map[string]int{}, size: map[int]bool{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	v.disk = fingerprintOf(path)
	if json.Unmarshal(b, &v.ids) != nil {
		v.ids = map[string]string{} // formato antigo (lista): recomeça; os valores são reaprendidos
	}
	for id, t := range v.ids {
		v.tally(id, t)
	}
	// arquivo de antes de os tamanhos serem guardados: sem o filtro (testa todos os pedaços)
	v.noSize = v.n["segredo"] > 0 && len(v.size) == 0
	return v, nil
}

// contar põe nos contadores e no filtro de tamanhos uma entrada que já está em v.ids.
func (v *Seen) tally(id, t string) {
	if n, err := strconv.Atoi(strings.TrimPrefix(id, sizePrefix)); err == nil && strings.HasPrefix(id, sizePrefix) {
		v.size[n] = true
		return
	}
	v.n[baseType(t)]++
	if strings.HasPrefix(t, "obj.") {
		v.nObj++
	}
}

// mergeFromDisk (com v.mu travado): se outro processo gravou o arquivo depois deste, traz o
// que só ele aprendeu. Uma entrada que este processo podou (teto) pode voltar assim; a próxima
// poda a tira de novo.
func (v *Seen) mergeFromDisk() {
	if fingerprintOf(v.path) == v.disk {
		return
	}
	b, err := os.ReadFile(v.path)
	if err != nil {
		return
	}
	var other map[string]string
	if json.Unmarshal(b, &other) != nil {
		return
	}
	for id, t := range other {
		if _, ok := v.ids[id]; !ok {
			v.ids[id] = t
			v.tally(id, t)
		}
	}
}

func (v *Seen) IsEmpty() bool { v.mu.RLock(); defer v.mu.RUnlock(); return len(v.ids) == 0 }

func (v *Seen) Type(id string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	t, ok := v.ids[id]
	return t, ok
}

// Has diz se há algum valor desse tipo guardado.
func (v *Seen) Has(typ string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.n[typ] > 0
}

// Objects (ver objetos.go): o valor guarda o tipo e duas datas, em dias desde 1970:
// "obj.tabela|<aprendido>|<visto pela última vez>". O teto é separado, para os nomes de
// objeto não expulsarem senhas e documentos.
const maxObjects = 500_000

func baseType(t string) string {
	if i := strings.IndexByte(t, '|'); i >= 0 {
		return t[:i]
	}
	return t
}

func (v *Seen) HasObj() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.nObj > 0
}

// Obj devolve o tipo de entidade e o dia em que o nome foi visto pela última vez.
func (v *Seen) Obj(id string) (ent string, seen int, ok bool) {
	v.mu.RLock()
	t, ok := v.ids[id]
	v.mu.RUnlock()
	if !ok {
		return "", 0, false
	}
	ent, _, seen, ok = readObj(t)
	return ent, seen, ok
}

func readObj(t string) (ent string, learned, seen int, ok bool) {
	p := strings.Split(t, "|")
	if len(p) != 3 || !strings.HasPrefix(p[0], "obj.") {
		return "", 0, 0, false
	}
	a, e1 := strconv.Atoi(p[1])
	b, e2 := strconv.Atoi(p[2])
	return p[0][4:], a, b, e1 == nil && e2 == nil
}

// MarkObj guarda o nome (ou atualiza o dia em que foi visto, no máximo uma vez por dia).
func (v *Seen) MarkObj(id, ent string, day int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if t, ok := v.ids[id]; ok {
		e, a, seen, ok := readObj(t)
		if !ok || seen >= day {
			return
		}
		v.ids[id], v.dirty = "obj."+e+"|"+strconv.Itoa(a)+"|"+strconv.Itoa(day), true
		return
	}
	if v.nObj >= maxObjects {
		v.pruneObj()
	}
	v.ids[id], v.dirty = "obj."+ent+"|"+strconv.Itoa(day)+"|"+strconv.Itoa(day), true
	v.n["obj."+ent]++
	v.nObj++
}

// pruneObj tira o décimo dos nomes de objeto vistos há mais tempo (com v.mu travado).
func (v *Seen) pruneObj() {
	days := make([]int, 0, v.nObj)
	for _, t := range v.ids {
		if _, _, d, ok := readObj(t); ok {
			days = append(days, d)
		}
	}
	sort.Ints(days)
	cut := days[len(days)/10]
	for id, t := range v.ids {
		if e, _, d, ok := readObj(t); ok && d <= cut {
			delete(v.ids, id)
			v.n["obj."+e]--
			v.nObj--
		}
	}
}

// maxSeen: teto dos hashes guardados (~40 bytes cada). Passou disso, um décimo sai, ao
// acaso; esses valores voltam a depender da palavra ao lado até serem vistos de novo.
const maxSeen = 500_000

func (v *Seen) Mark(id, typ string) {
	v.mu.Lock()
	if _, ok := v.ids[id]; !ok {
		if len(v.ids)-v.nObj >= maxSeen {
			newOne, n := make(map[string]string, maxSeen), map[string]int{}
			for k, t := range v.ids { // a ordem de um mapa em Go é aleatória
				if strings.HasPrefix(k, sizePrefix) || strings.HasPrefix(t, "obj.") {
					newOne[k] = t
					continue
				}
				if len(newOne) >= maxSeen*9/10 {
					continue
				}
				newOne[k] = t
				n[t]++
			}
			for k, c := range v.n {
				if strings.HasPrefix(k, "obj.") {
					n[k] = c
				}
			}
			v.ids, v.n = newOne, n
		}
		v.ids[id], v.dirty = typ, true
		v.n[typ]++
	}
	v.mu.Unlock()
}

// SaveIfDirty grava o arquivo se houve novidade desde a última gravação.
// Com muitos hashes a gravação custa (≈0,3 s com 250 mil), então acontece no máximo uma
// vez a cada saveInterval; o que ficou pendente é gravado por um temporizador.
func (v *Seen) SaveIfDirty() error {
	v.mu.Lock()
	if !v.dirty {
		v.mu.Unlock()
		return nil
	}
	if missingOne := saveInterval - time.Since(v.saved); missingOne > 0 {
		if !v.scheduled {
			v.scheduled = true
			time.AfterFunc(missingOne, func() {
				v.mu.Lock()
				v.scheduled = false
				v.mu.Unlock()
				v.SaveIfDirty()
			})
		}
		v.mu.Unlock()
		return nil
	}
	v.saved = time.Now()
	v.mergeFromDisk()
	b, _ := json.Marshal(v.ids)
	v.dirty = false
	v.mu.Unlock()
	imp, err := writeSwapping(v.path, b)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.disk = imp
	v.mu.Unlock()
	return nil
}

// SaveNow grava já o que estiver pendente, sem esperar o intervalo (na saída do proxy).
func (v *Seen) SaveNow() error {
	v.mu.Lock()
	v.saved = time.Time{}
	v.mu.Unlock()
	return v.SaveIfDirty()
}
