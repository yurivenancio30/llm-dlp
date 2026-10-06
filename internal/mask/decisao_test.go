package mask

import (
	"strings"
	"testing"
)

// Contrato da interface comum (decisao.go): as trilhas A–D dependem disto.

func TestContratoDecisor(t *testing.T) {
	// um decisor de teste: a palavra "zz9" depois de "cofre " é bucket
	nome := "teste-contrato"
	registrarDecisor(Decisor{Nome: nome, Decidir: func(c *TextoCtx) {
		if c.Ext != "" && c.Ext != "ext-ok" {
			t.Errorf("extensão da dica: %q", c.Ext)
		}
		for i := strings.Index(c.S, "cofre "); i >= 0; i = -1 {
			a := i + len("cofre ")
			b := a
			for b < len(c.S) && ehAlnum(c.S[b]) {
				b++
			}
			c.Decidir(a, b, "bucket", nome)
		}
	}})
	defer func() {
		decisoresMu.Lock()
		decisores = decisores[:len(decisores)-1]
		decisoresMu.Unlock()
	}()
	m := novoTeste(t)
	s := "abre o cofre tesouro agora"
	out, ents := m.Mascarar(s)
	if strings.Contains(out, "tesouro") {
		t.Fatalf("o decisor não mascarou: %s", out)
	}
	if volta := NovaTabela(ents).Desmascarar(out, false); volta != s {
		t.Fatalf("ida e volta: %q", volta)
	}
	ds := m.Decididos(s, "")
	achou := false
	for _, d := range ds {
		if d.Nome == "tesouro" && d.Ent == "bucket" && d.Regra == nome && !d.Generica {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("decisão não ficou no memo: %+v", ds)
	}
	// a extensão da dica chega ao decisor, e a dica do leitor de tabela continua legível
	r, _ := m.mascararD("abre o cofre tesouro de novo", true, "f||| "+sepExt+"ext-ok")
	if strings.Contains(r.texto, "tesouro") || len(r.decididos) == 0 {
		t.Fatalf("com dica estendida: %q %+v", r.texto, r.decididos)
	}
	// palavra comum decidida não vai para o vistos (não tem cara de identificador)
	if _, ok := m.entAprendido("tesouro"); ok {
		t.Fatalf("decisão de palavra comum foi aprendida")
	}
}

func TestContratoDecisoesDosLeitores(t *testing.T) {
	m := novoTeste(t)
	s := "SELECT c_valor FROM sch_fin.t_lanc WHERE id = 1 AND x = 2"
	m.Mascarar(s)
	ents := map[string]string{}
	for _, d := range m.Decididos(s, "") {
		ents[d.Nome] = d.Ent
	}
	if ents["t_lanc"] != "tabela" || ents["sch_fin"] != "schema" {
		t.Fatalf("decisões dos leitores: %v", ents)
	}
	if (Decisao{Nome: "T_LANC", Ent: "tabela"}).Chave() != (Decisao{Nome: "t_lanc", Ent: "tabela"}).Chave() {
		t.Fatalf("chave de SQL deveria ser sem caixa")
	}
	if (Decisao{Nome: "Pagto", Ent: "bucket"}).Chave() == (Decisao{Nome: "pagto", Ent: "bucket"}).Chave() {
		t.Fatalf("chave de bucket deveria ser exata")
	}
}

func TestContratoGenerica(t *testing.T) {
	ant := refPublica
	defer func() { refPublica = ant }()
	refPublica = map[string]bool{"default": true}
	if !ehGenerica("Default") || ehGenerica("pagamentos") {
		t.Fatal("ehGenerica")
	}
}

func TestContratoQuemEscreveu(t *testing.T) {
	m := novoTeste(t)
	_, ents := m.Mascarar("SELECT a FROM sch_fin.t_lanc WHERE b = 1 AND c = 2")
	tab := NovaTabela(ents)
	var ps string
	for _, e := range ents {
		if e.Real == "t_lanc" {
			ps = e.Pseudo
		}
	}
	if ps == "" {
		t.Fatal("sem pseudônimo")
	}
	orig := "olhe a tabela " + ps + " e a palavra lanc"
	des := m.RegistrarResposta(orig, tab)
	if des != tab.Desmascarar(orig, false) || !strings.Contains(des, "t_lanc") {
		t.Fatalf("desmascarado: %q", des)
	}
	if o, ok := m.OriginalDe(des); !ok || o != orig {
		t.Fatalf("OriginalDe: %q %v", o, ok)
	}
	tr, ok := m.Traduzidas(des)
	if !ok || len(tr) != 1 || des[tr[0].Ini:tr[0].Fim] != "t_lanc" || tr[0].Pseudo != ps {
		t.Fatalf("Traduzidas: %+v %v", tr, ok)
	}
	if _, ok := m.OriginalDe("texto que ninguém registrou"); ok {
		t.Fatal("sem registro deveria dar false")
	}
}
