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

// The secret key and the identifiers derived from it (HMAC).

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// Key loads the 256-bit key from path, creating a new one (0600) if it does not exist.
func Key(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		// Writes the COMPLETE key to a temporary file and only then publishes it with link(), which
		// is atomic and fails if the final file already exists. So, if two processes create it at
		// the same time, only one wins and the other reads its key — and nobody ever reads a
		// half-written key. Without this, one would overwrite the other and the hashes already
		// written (pessoas.json) would stop matching, without warning.
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

// Pseudo generates short, stable identifiers from the key.
type Pseudo struct {
	key []byte
	mu  sync.RWMutex
	ids map[string]string // IDs already computed (RAM only): the same words repeat a lot
}

const idsMax = 100_000 // past this, start over (limits RAM)

func NewPseudo(key []byte) *Pseudo { return &Pseudo{key: key, ids: map[string]string{}} }

// ID returns 8 base32 characters (40 bits) of HMAC(key, type || normalized value).
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

// Bits returns n bytes of the HMAC, to derive numbers (e.g. the fake IP prefix).
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
