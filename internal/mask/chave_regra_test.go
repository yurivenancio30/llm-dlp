package mask

import (
	"crypto/sha256"
	"sort"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Regra geral: todo pseudônimo vem da chave. O mesmo texto mascarado com duas chaves dá, para
// cada valor real, dois pseudônimos diferentes; e nenhum pseudônimo contém o valor real. Um
// tipo novo que gere o pseudônimo sem a chave (ou com um pedaço do real) quebra este teste.
func TestPseudonimosVemDaChave(t *testing.T) {
	texto := strings.Join([]string{
		"Falar com João Carlos Silva (joao.silva@empresa-ficticia.com.br, U1001) e com João Silva.",
		"CPF 529.982.247-25, CNPJ 11.222.333/0001-81, matrícula MAT123456, sobre o Projeto Fenix.",
		"O servidor 10.42.7.15 e ip-10-42-7-16.ec2.internal; banco em db01.empresa-ficticia.com.br.",
		"SELECT p.id FROM vendas.PedidoItem p JOIN VENDAS.CLIENTE_ATIVO c ON c.id = p.cliente_id;",
		"export DB_PASSWORD=Xk8pq2vR9zLm4Tw7",
		"rastreio RR473124829BR, atestado com CID F32.1",
	}, "\n")
	pseudos := func(chave []byte) map[string]string {
		cfg := config.Padrao()
		cfg.DominiosInternos = []string{"empresa-ficticia"}
		cfg.PadroesExtras = []config.PadraoExtra{{Rotulo: "matricula", Regex: `\bMAT\d{6}\b`}}
		cfg.Termos = []config.Termo{{Rotulo: "projeto", Valores: []string{"Projeto Fenix"}}}
		ps, _ := CarregarPessoas(t.TempDir() + "/p.json")
		ps.Importar(NovoPseudo(chave), "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
		vs, _ := CarregarVistos(t.TempDir() + "/v.json")
		m, err := NovoMasker(cfg, chave, ps, vs)
		if err != nil {
			t.Fatal(err)
		}
		_, ents := m.Mascarar(texto)
		out := map[string]string{}
		for _, e := range ents {
			out[e.Tipo+"\x00"+e.Real] = e.Pseudo
		}
		return out
	}
	a := pseudos([]byte("0123456789abcdef0123456789abcdef"))
	b := pseudos([]byte("fedcba9876543210fedcba9876543210"))
	tipos := map[string]bool{}
	for k, pa := range a {
		tipo, real, _ := strings.Cut(k, "\x00")
		tipos[tipo] = true
		if pb, ok := b[k]; !ok {
			t.Errorf("%s %q: mascarado só com uma das chaves", tipo, real)
		} else if pa == pb {
			t.Errorf("%s %q: o mesmo pseudônimo %q com as duas chaves", tipo, real, pa)
		}
		if len(real) >= 4 && strings.Contains(strings.ToLower(pa), strings.ToLower(real)) {
			t.Errorf("%s: o pseudônimo %q contém o valor real", tipo, pa)
		}
	}
	ts := make([]string, 0, len(tipos))
	for tp := range tipos {
		ts = append(ts, tp)
	}
	sort.Strings(ts)
	if len(ts) < 12 {
		t.Errorf("poucos tipos cobertos: %v", ts)
	}
	t.Logf("tipos: %v", ts)
}

// A caixa do ID de um objeto de caixa mista também vem da chave: não segue um hash sem chave do
// nome (com ele, quem adivinhasse o nome conferiria a caixa de cada letra).
func TestCaixaDoObjetoVemDaChave(t *testing.T) {
	m, err := NovoMasker(config.Padrao(), []byte("0123456789abcdef0123456789abcdef"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	letras, iguais := 0, 0
	for _, nome := range []string{"PedidoItem", "ClienteAtivo", "NotaFiscal", "ContaReceber", "ItemEstoque"} {
		ps := m.pseudoObjeto("tabela", nome)
		id := ps[strings.IndexByte(ps, '_')+1:]
		h := sha256.Sum256([]byte(nome))
		for i := 0; i < len(id); i++ {
			if c := id[i]; c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				letras++
				if (c <= 'Z') == (h[i]&1 == 1) {
					iguais++
				}
			}
		}
	}
	if letras < 10 || iguais == letras {
		t.Errorf("a caixa de %d de %d letras segue o SHA-256 sem chave do nome", iguais, letras)
	}
}
