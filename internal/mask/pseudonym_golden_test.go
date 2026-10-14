package mask

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// The pseudonyms of a fixed key, recorded in testdata/pseudonyms.golden. Whoever already has a key
// has conversations, a people registry and a seen-values file built with these pseudonyms and
// hashes: a refactor that changes one of them (a renamed type that also renamed the HMAC domain,
// a new normalization) breaks the continuity of every conversation. This test catches it. To
// record the file again, on purpose: LLM_DLP_GOLDEN=1 go test ./internal/mask -run TestPseudonymGolden
func TestPseudonymGolden(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	cfg := config.Default()
	cfg.InternalDomains = []string{"empresa-ficticia"}
	cfg.ExtraPatterns = []config.ExtraPattern{{Label: "matricula", Regex: `\bMAT\d{6}\b`}}
	cfg.Terms = []config.Term{{Label: "projeto", Values: []string{"Projeto Fenix"}}}
	dir := t.TempDir()
	ps, _ := LoadPeople(dir + "/p.json")
	ps.Import(NewPseudo(key), "João Carlos Silva", "joao.silva@empresa-ficticia.com.br", "U1001")
	vs, _ := LoadSeen(dir + "/v.json")
	m, err := NewMasker(cfg, key, ps, vs)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	add := func(typ, real, ps string) { lines = append(lines, typ+"\t"+real+"\t"+ps) }

	// each type, directly
	for _, c := range [][2]string{
		{"secret", "Xk8pq2vR9zLm4Tw7"}, {"email", "maria.souza@empresa-ficticia.com.br"},
		{"email", "joao.silva@empresa-ficticia.com.br"}, {"ip", "10.42.7.15"},
		{"host", "db01.empresa-ficticia.com.br"}, {"host", "mysql.empresa-ficticia.com.br"},
		{"cpf", "529.982.247-25"}, {"cnpj", "11.222.333/0001-81"}, {"cnpj", "12.ABC.345/01DE-35"},
		{"pis", "120.5643.678-0"}, {"cnh", "02650306461"}, {"rg", "12.345.678-9"}, {"phone", "(11) 98765-4321"},
		{"cep", "01310-100"}, {"card", "4111 1111 1111 1111"}, {"account", "12345-6"}, {"pix", "maria@pix.com.br"},
		{"address", "Rua das Flores, 123"}, {"birthdate", "12/03/1985"}, {"name", "João Carlos Silva"},
		{"name", "JOÃO SILVA"}, {"name", "maria fernanda souza"}, {"name", "Maria Fernanda Souza"},
		{"user", "U1001"}, {"user", "jsilva"}, {"doc", "AB1234567"}, {"iban", "DE89370400440532013000"},
		{"lawsuit", "0001234-56.2024.8.26.0100"}, {"mac", "00:1a:2b:3c:4d:5e"},
		{"nfe", "35240111222333000181550010000012341000012345"}, {"boleto", "23790.12345 60000.123456 78901.234567 8 91230000010000"},
		{"tracking", "RR473124829BR"}, {"certificate", "104539 01 55 2013 1 00012 021 0000123-45"},
		{"sensitive", "F32.1"}, {"sensitive", "evangélico"}, {"quasi", "feminino"},
		{"x:matricula", "MAT123456"}, {"t:projeto", "Projeto Fenix"}, {"t:projeto", "PROJETO FENIX"},
	} {
		add(c[0], c[1], m.Pseudonym(c[0], c[1]))
	}
	// each object entity, in the three cases
	ents := make([]string, 0, len(ObjectPrefixes)+1)
	for e := range ObjectPrefixes {
		ents = append(ents, e)
	}
	ents = append(ents, genericEnt)
	sort.Strings(ents)
	for _, e := range ents {
		for _, v := range []string{"pedido_item", "PEDIDO_ITEM", "PedidoItem"} {
			add(objTypePrefix+e, v, m.Pseudonym(objTypePrefix+e, v))
		}
	}
	// a text, as the proxy masks it (detectors, readers, people, learned values)
	_, ents2 := m.Mask(strings.Join([]string{
		"Falar com João Carlos Silva (joao.silva@empresa-ficticia.com.br, U1001) e com João Silva.",
		"CPF 529.982.247-25, CNPJ 11.222.333/0001-81, matrícula MAT123456, sobre o Projeto Fenix.",
		"O servidor 10.42.7.15 e ip-10-42-7-16.ec2.internal; banco em db01.empresa-ficticia.com.br.",
		"SELECT p.id FROM vendas.PedidoItem p JOIN VENDAS.CLIENTE_ATIVO c ON c.id = p.cliente_id;",
		"export DB_PASSWORD=Xk8pq2vR9zLm4Tw7",
		"rastreio RR473124829BR, atestado com CID F32.1, tel (11) 98765-4321",
		"kubectl get pods -n pagamentos-prod; bucket s3://vendas-relatorios-hx/2024/",
	}, "\n"))
	for _, e := range ents2 {
		add("texto:"+e.Type, e.Real, e.Pseudo)
	}
	// what goes to disk: the seen-values ids and the people registry hashes
	vs.SaveNow()
	var seen map[string]any
	b, _ := os.ReadFile(dir + "/v.json")
	json.Unmarshal(b, &seen)
	for k, v := range seen {
		add("visto", k, toJSON(v))
	}
	ps.SaveNow()
	b, _ = os.ReadFile(dir + "/p.json")
	add("pessoas", "", string(b))

	sort.Strings(lines)
	lines = dedup(lines)
	got := strings.Join(lines, "\n") + "\n"
	const golden = "testdata/pseudonyms.golden"
	if os.Getenv("LLM_DLP_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		ws := map[string]bool{}
		for _, l := range wl {
			ws[l] = true
		}
		gs := map[string]bool{}
		for _, l := range gl {
			gs[l] = true
			if !ws[l] {
				t.Errorf("new:     %s", l)
			}
		}
		for _, l := range wl {
			if !gs[l] {
				t.Errorf("missing: %s", l)
			}
		}
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func dedup(ls []string) []string {
	out := ls[:0]
	for i, l := range ls {
		if i == 0 || l != ls[i-1] {
			out = append(out, l)
		}
	}
	return out
}
