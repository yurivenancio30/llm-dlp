package mask

import (
	"crypto/sha256"
	"sort"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// General rule: every pseudonym comes from the key. The same text masked with two keys gives, for
// each real value, two different pseudonyms; and no pseudonym contains the real value. A new type
// that produces the pseudonym without the key (or with a piece of the real value) breaks this test.
func TestPseudonymsComeFromKey(t *testing.T) {
	text := strings.Join([]string{
		"Falar com João Carlos Silva (joao.silva@empresa-ficticia.com.br, U1001) e com João Silva.",
		"CPF 529.982.247-25, CNPJ 11.222.333/0001-81, matrícula MAT123456, sobre o Projeto Fenix.",
		"O servidor 10.42.7.15 e ip-10-42-7-16.ec2.internal; banco em db01.empresa-ficticia.com.br.",
		"SELECT p.id FROM vendas.PedidoItem p JOIN VENDAS.CLIENTE_ATIVO c ON c.id = p.cliente_id;",
		"export DB_PASSWORD=Xk8pq2vR9zLm4Tw7",
		"rastreio RR473124829BR, atestado com CID F32.1",
	}, "\n")
	pseudos := func(key []byte) map[string]string {
		cfg := config.Default()
		cfg.InternalDomains = []string{"empresa-ficticia"}
		cfg.ExtraPatterns = []config.ExtraPattern{{Label: "matricula", Regex: `\bMAT\d{6}\b`}}
		cfg.Terms = []config.Term{{Label: "projeto", Values: []string{"Projeto Fenix"}}}
		ps, _ := LoadPeople(t.TempDir() + "/p.json")
		ps.Import(NewPseudo(key), "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
		vs, _ := LoadSeen(t.TempDir() + "/v.json")
		m, err := NewMasker(cfg, key, ps, vs)
		if err != nil {
			t.Fatal(err)
		}
		_, ents := m.Mask(text)
		out := map[string]string{}
		for _, e := range ents {
			out[e.Type+"\x00"+e.Real] = e.Pseudo
		}
		return out
	}
	a := pseudos([]byte("0123456789abcdef0123456789abcdef"))
	b := pseudos([]byte("fedcba9876543210fedcba9876543210"))
	types := map[string]bool{}
	for k, pa := range a {
		typ, real, _ := strings.Cut(k, "\x00")
		types[typ] = true
		if pb, ok := b[k]; !ok {
			t.Errorf("%s %q: mascarado só com uma das chaves", typ, real)
		} else if pa == pb {
			t.Errorf("%s %q: o mesmo pseudônimo %q com as duas chaves", typ, real, pa)
		}
		if len(real) >= 4 && strings.Contains(strings.ToLower(pa), strings.ToLower(real)) {
			t.Errorf("%s: o pseudônimo %q contém o valor real", typ, pa)
		}
	}
	ts := make([]string, 0, len(types))
	for tp := range types {
		ts = append(ts, tp)
	}
	sort.Strings(ts)
	if len(ts) < 12 {
		t.Errorf("poucos tipos cobertos: %v", ts)
	}
	t.Logf("tipos: %v", ts)
}

// The case of the ID of a mixed-case object also comes from the key: it does not follow an unkeyed
// hash of the name (with one, whoever guessed the name could check the case of each letter).
func TestObjectCaseComesFromKey(t *testing.T) {
	m, err := NewMasker(config.Default(), []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	letters, equalOnes := 0, 0
	for _, name := range []string{"PedidoItem", "ClienteAtivo", "NotaFiscal", "ContaReceber", "ItemEstoque"} {
		ps := m.pseudoObject("table", name)
		id := ps[strings.IndexByte(ps, '_')+1:]
		h := sha256.Sum256([]byte(name))
		for i := 0; i < len(id); i++ {
			if c := id[i]; c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				letters++
				if (c <= 'Z') == (h[i]&1 == 1) {
					equalOnes++
				}
			}
		}
	}
	if letters < 10 || equalOnes == letters {
		t.Errorf("a caixa de %d de %d letras segue o SHA-256 sem chave do nome", equalOnes, letters)
	}
}
