package mask

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// A chave secreta e os identificadores derivados dela (HMAC).

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// Key carrega a chave de 256 bits de path, criando uma nova (0600) se não existir.
func Key(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		// Grava a chave COMPLETA num arquivo temporário e só então a publica com link(),
		// que é atômico e falha se o arquivo final já existir. Assim, se dois processos
		// criarem ao mesmo tempo, só um vence e o outro lê a chave dele — e ninguém nunca
		// lê uma chave pela metade. Sem isso, um sobrescreveria o outro e os hashes já
		// gravados (pessoas.json) deixariam de bater, sem aviso.
		tmp, err := os.CreateTemp(filepath.Dir(path), ".chave-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		_, err = tmp.WriteString(base64.StdEncoding.EncodeToString(k) + "\n")
		if err == nil {
			err = tmp.Sync()
		}
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(tmp.Name(), 0o600)
		}
		if err != nil {
			return nil, err
		}
		if err := os.Link(tmp.Name(), path); err != nil {
			if errors.Is(err, os.ErrExist) {
				return Key(path) // outro processo venceu: usa a chave dele
			}
			return nil, err
		}
		return k, nil
	}
	if err != nil {
		return nil, err
	}
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(k) != 32 {
		return nil, fmt.Errorf("chave inválida em %s", path)
	}
	return k, nil
}

// Pseudo gera identificadores curtos e estáveis a partir da chave.
type Pseudo struct {
	key []byte
	mu  sync.RWMutex
	ids map[string]string // IDs já calculados (só em RAM): as mesmas palavras se repetem muito
}

const idsMax = 100_000 // passou disso, recomeça (limita a RAM)

func NewPseudo(key []byte) *Pseudo { return &Pseudo{key: key, ids: map[string]string{}} }

// ID devolve 8 caracteres base32 (40 bits) de HMAC(chave, tipo || valor normalizado).
func (p *Pseudo) ID(typ, normValue string) string {
	k := typ + "\x00" + normValue
	p.mu.RLock()
	id, ok := p.ids[k]
	p.mu.RUnlock()
	if ok {
		return id
	}
	id = strings.ToLower(p.raw(typ, normValue, 5))
	p.mu.Lock()
	if len(p.ids) >= idsMax || p.ids == nil {
		p.ids = map[string]string{}
	}
	p.ids[k] = id
	p.mu.Unlock()
	return id
}

// Bits devolve n bytes do HMAC, para derivar números (ex.: prefixo de IP falso).
func (p *Pseudo) Bits(typ, normValue string, n int) []byte {
	m := hmac.New(sha256.New, p.key)
	m.Write([]byte(typ))
	m.Write([]byte{0})
	m.Write([]byte(normValue))
	return m.Sum(nil)[:n]
}

func (p *Pseudo) raw(typ, normValue string, n int) string {
	return b32.EncodeToString(p.Bits(typ, normValue, n))
}
