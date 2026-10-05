package mask

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Leitor de teste: as regras de verdade chegam nos lotes seguintes. "TABLE x" e "HOST x" são
// posição forte; "tab x" e "ref x" são fracas (cada uma é uma regra); "COLUMN x" é coluna.
var reTesteForte = regexp.MustCompile(`\b(TABLE|HOST|COLUMN) ([\w.\-]+)`)
var reTesteFraco = regexp.MustCompile(`\b(tab|ref) ([\w.\-]+)`)

func leitorTeste() []Leitor {
	ent := map[string]string{"TABLE": "tabela", "HOST": "servidor", "COLUMN": "coluna", "tab": "tabela", "ref": "tabela"}
	return []Leitor{{Nome: "teste", Publico: func(v string) bool { return strings.EqualFold(v, "dual_x1") },
		Achar: func(s string, add func(ObjAchado)) {
			for _, ix := range reTesteForte.FindAllStringSubmatchIndex(s, -1) {
				add(ObjAchado{ix[4], ix[5], ent[s[ix[2]:ix[3]]], "forte", true})
			}
			for _, ix := range reTesteFraco.FindAllStringSubmatchIndex(s, -1) {
				add(ObjAchado{ix[4], ix[5], ent[s[ix[2]:ix[3]]], s[ix[2]:ix[3]], false})
			}
		}}}
}

func comLeitor(t *testing.T) *Masker {
	m := novoTeste(t)
	m.UsarLeitores(leitorTeste())
	return m
}

var rePseudoT = regexp.MustCompile(`\b[Tt]_[a-z2-7]{12}\b`)

func TestPseudonimoDeObjeto(t *testing.T) {
	m := comLeitor(t)
	a := m.Pseudonimo("obj.tabela", "tb_pedido_x9")
	if !regexp.MustCompile(`^t_[a-z2-7]{12}$`).MatchString(a) {
		t.Fatalf("formato: %q", a)
	}
	if b := m.Pseudonimo("obj.tabela", "TB_PEDIDO_X9"); b != "T"+a[1:] {
		t.Errorf("a mesma tabela em maiúsculas deveria ter o mesmo ID: %q %q", a, b)
	}
	if b := m.Pseudonimo("obj.tabela", "[tb_pedido_x9]"); b[2:] != a[2:] {
		t.Errorf("colchetes não deveriam mudar o ID")
	}
	if b := m.Pseudonimo("obj.coluna", "tb_pedido_x9"); b[2:] == a[2:] {
		t.Errorf("tipos diferentes deveriam ter IDs diferentes")
	}
	// estável entre instâncias (reinício) com a mesma chave; outro com outra chave
	m2, _ := NovoMasker(m.cfg, chaveTeste, nil, nil)
	if m2.Pseudonimo("obj.tabela", "tb_pedido_x9") != a {
		t.Errorf("pseudônimo mudou com outra instância da mesma chave")
	}
	m3, _ := NovoMasker(m.cfg, []byte("outra-chave-outra-chave-outra-32"), nil, nil)
	if m3.Pseudonimo("obj.tabela", "tb_pedido_x9") == a {
		t.Errorf("chaves diferentes deram o mesmo pseudônimo")
	}
}

func TestCaraDeIdentificador(t *testing.T) {
	sim := []string{"tb_pedido", "srv01", "app.config", "svc-pedidos", "contaCorrente", "DB_X"}
	nao := []string{"cliente", "Pedido", "SQL", "guarda", "1.2.3", "v2.10.1", "550e8400-e29b-41d4-a716-446655440000",
		"deadbeefdeadbeef", "ab", "2026"}
	for _, v := range sim {
		if !caraDeIdentificador(v) {
			t.Errorf("deveria ter cara de identificador: %q", v)
		}
	}
	for _, v := range nao {
		if caraDeIdentificador(v) {
			t.Errorf("não deveria ter cara de identificador: %q", v)
		}
	}
}

func TestObjetoFortePropaga(t *testing.T) {
	m := comLeitor(t)
	out, _ := m.Mascarar("CREATE TABLE tb_pedido_x9 (id int)")
	if strings.Contains(out, "tb_pedido_x9") || !rePseudoT.MatchString(out) {
		t.Fatalf("na posição: %q", out)
	}
	for _, s := range []string{"a carga da tb_pedido_x9 falhou", "a carga da TB_PEDIDO_X9 falhou", "x: [\"tb_pedido_x9\"]"} {
		if out, _ := m.Mascarar(s); strings.Contains(strings.ToLower(out), "tb_pedido_x9") {
			t.Errorf("não propagou: %q", out)
		}
	}
	// pedaço de outro nome não é o nome
	if out, _ := m.Mascarar("a tb_pedido_x9_hist e a xtb_pedido_x9"); !strings.Contains(out, "tb_pedido_x9_hist") || !strings.Contains(out, "xtb_pedido_x9") {
		t.Errorf("mascarou pedaço de outro nome: %q", out)
	}
}

func TestObjetoFracoSoEnsinaComDuasRegras(t *testing.T) {
	m := comLeitor(t)
	if out, _ := m.Mascarar("veja tab tb_fraca_01 agora"); strings.Contains(out, "tb_fraca_01") {
		t.Fatalf("na posição fraca deveria mascarar: %q", out)
	}
	if out, _ := m.Mascarar("solto: tb_fraca_01"); !strings.Contains(out, "tb_fraca_01") {
		t.Fatalf("evidência fraca não deveria ensinar: %q", out)
	}
	m.Mascarar("veja tab tb_fraca_01 de novo") // mesma regra: continua fraco
	if out, _ := m.Mascarar("solto de novo: tb_fraca_01"); !strings.Contains(out, "tb_fraca_01") {
		t.Fatalf("a mesma regra duas vezes não deveria ensinar: %q", out)
	}
	m.Mascarar("e ref tb_fraca_01") // outra regra
	if out, _ := m.Mascarar("agora solto: tb_fraca_01"); strings.Contains(out, "tb_fraca_01") {
		t.Errorf("duas regras diferentes deveriam ensinar: %q", out)
	}
}

func TestPalavraSimplesEColunaNaoPropagam(t *testing.T) {
	m := comLeitor(t)
	if out, _ := m.Mascarar("TABLE cliente"); strings.Contains(out, "cliente") {
		t.Fatalf("na posição deveria mascarar a palavra simples: %q", out)
	}
	if out, _ := m.Mascarar("o cliente ligou"); out != "o cliente ligou" {
		t.Errorf("palavra simples propagou: %q", out)
	}
	if out, _ := m.Mascarar("COLUMN vl_total_x2"); strings.Contains(out, "vl_total_x2") {
		t.Fatalf("coluna na posição: %q", out)
	}
	if out, _ := m.Mascarar("soma de vl_total_x2"); !strings.Contains(out, "vl_total_x2") {
		t.Errorf("coluna não deveria propagar por padrão: %q", out)
	}
	m.cfg.Objetos.Propagar = map[string]bool{"coluna": true}
	m.Mascarar("COLUMN vl_outro_x3")
	if out, _ := m.Mascarar("soma de vl_outro_x3"); strings.Contains(out, "vl_outro_x3") {
		t.Errorf("com propagar.coluna ligado deveria propagar: %q", out)
	}
}

func TestVocabularioPublicoNaoEAprendido(t *testing.T) {
	m := comLeitor(t)
	if out, _ := m.Mascarar("TABLE dual_x1"); !strings.Contains(out, "dual_x1") {
		t.Errorf("vocabulário público foi mascarado: %q", out)
	}
	if out, _ := m.Mascarar("solto dual_x1"); !strings.Contains(out, "dual_x1") {
		t.Errorf("vocabulário público foi aprendido: %q", out)
	}
}

func TestLigaDesligaPorTipo(t *testing.T) {
	m := comLeitor(t)
	m.Mascarar("HOST srv-exemplo-01")
	m.cfg.Objetos.Mascarar = map[string]bool{"servidor": false}
	if out, _ := m.Mascarar("HOST srv-exemplo-02"); !strings.Contains(out, "srv-exemplo-02") {
		t.Errorf("servidor desligado foi mascarado na posição: %q", out)
	}
	if out, _ := m.Mascarar("ping srv-exemplo-01 agora"); !strings.Contains(out, "srv-exemplo-01") {
		t.Errorf("servidor aprendido antes continuou mascarado depois de desligado: %q", out)
	}
	m.cfg.Objetos.Mascarar = nil
	m.cfg.Objetos.Ligado = false
	if out, _ := m.Mascarar("TABLE tb_desligado_01"); !strings.Contains(out, "tb_desligado_01") {
		t.Errorf("objetos desligados foram mascarados: %q", out)
	}
}

func TestObjetoDaWebNaoEnsina(t *testing.T) {
	m := comLeitor(t)
	if out, _ := m.NovoLote().Mascarar("doc: TABLE tb_web_01", true, posA); strings.Contains(out, "tb_web_01") {
		t.Fatalf("na página deveria mascarar: %q", out)
	}
	if out, _ := m.Mascarar("solto tb_web_01"); !strings.Contains(out, "tb_web_01") {
		t.Errorf("conteúdo da web ensinou: %q", out)
	}
}

func TestObjetoAposReinicioEValidade(t *testing.T) {
	dir := t.TempDir()
	vs, _ := CarregarVistos(dir + "/vistos.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	m.UsarLeitores(leitorTeste())
	m.Mascarar("TABLE tb_disco_01")
	vs.Salvar()
	if b, _ := os.ReadFile(dir + "/vistos.json"); strings.Contains(string(b), "tb_disco") {
		t.Fatalf("nome em claro no vistos.json")
	}
	novo := func() *Masker {
		v, _ := CarregarVistos(dir + "/vistos.json")
		m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, v)
		return m
	}
	if out, _ := novo().Mascarar("depois do reinício: TB_DISCO_01"); strings.Contains(strings.ToLower(out), "tb_disco_01") {
		t.Errorf("após reinício não mascarou: %q", out)
	}
	antes := hoje
	defer func() { hoje = antes }()
	hoje = func() int { return antes() + validadeObj + 5 }
	if out, _ := novo().Mascarar("bem depois: tb_disco_01"); !strings.Contains(out, "tb_disco_01") {
		t.Errorf("nome não visto há mais de %d dias continuou propagando: %q", validadeObj, out)
	}
}

func TestEsquecer(t *testing.T) {
	dir := t.TempDir()
	vs, _ := CarregarVistos(dir + "/vistos.json")
	m, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, vs)
	m.UsarLeitores(leitorTeste())
	m.Mascarar("TABLE tb_esq_01 HOST srv-esq-01 TABLE tb_esq_02")
	if n := vs.Esquecer(m.IdObjeto("TB_ESQ_01"), func(string, int, int) bool { return true }); n != 1 {
		t.Fatalf("por nome: %d", n)
	}
	if n := vs.Esquecer("", func(e string, _, _ int) bool { return e == "servidor" }); n != 1 {
		t.Fatalf("por tipo: %d", n)
	}
	d := hoje()
	if n := vs.Esquecer("", func(_ string, a, _ int) bool { return a >= d+1 }); n != 0 {
		t.Fatalf("por data (futura): %d", n)
	}
	vs.Salvar()
	v2, _ := CarregarVistos(dir + "/vistos.json")
	m2, _ := NovoMasker(novoTeste(t).cfg, chaveTeste, nil, v2)
	out, _ := m2.Mascarar("tb_esq_01 srv-esq-01 tb_esq_02")
	if !strings.Contains(out, "tb_esq_01") || !strings.Contains(out, "srv-esq-01") || strings.Contains(out, "tb_esq_02") {
		t.Errorf("depois de esquecer: %q", out)
	}
}

// Ida e volta: o pseudônimo que o modelo escreve, em qualquer das duas caixas do prefixo,
// volta ao nome real.
func TestIdaEVoltaObjeto(t *testing.T) {
	m := comLeitor(t)
	out, ents := m.Mascarar("SELECT * FROM x; TABLE tb_ida_01")
	p := rePseudoT.FindString(out)
	if p == "" {
		t.Fatalf("sem pseudônimo: %q", out)
	}
	tab := NovaTabela(ents)
	for _, escrito := range []string{p, "T" + p[1:], "t" + p[1:]} {
		if got := tab.Desmascarar("rode `SELECT 1 FROM "+escrito+"`;", false); got != "rode `SELECT 1 FROM tb_ida_01`;" {
			t.Errorf("volta de %q: %q", escrito, got)
		}
	}
	if out2, _ := m.Mascarar("o modelo escreveu " + p); out2 != "o modelo escreveu "+p {
		t.Errorf("pseudônimo pronto foi mascarado de novo: %q", out2)
	}
}

// Nome aprendido depois não muda o texto que já saiu (congelamento), mas vale para texto novo.
func TestObjetoAprendidoNaoMudaOQueJaSaiu(t *testing.T) {
	m := comLeitor(t)
	antigo := "a carga da tb_cong_01 falhou"
	l := m.NovoLote()
	enviado, _ := l.Mascarar(antigo, false, posA)
	l.Congelar()
	m.Mascarar("TABLE tb_cong_01")
	if out, _ := m.NovoLote().Mascarar(antigo, false, posA); out != enviado {
		t.Errorf("o texto que já saiu mudou")
	}
	if out, _ := m.NovoLote().Mascarar(antigo, false, posB); strings.Contains(out, "tb_cong_01") {
		t.Errorf("texto novo não usou o nome aprendido: %q", out)
	}
}
