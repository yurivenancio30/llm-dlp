package mask

import (
	"strings"
	"testing"
)

// Contrato da interface comum (decision.go): as trilhas A–D dependem disto.

func TestContractDecider(t *testing.T) {
	// um decisor de teste: a palavra "zz9" depois de "cofre " é bucket
	name := "teste-contrato"
	registerDecider(Decider{Name: name, Decide: func(c *TextCtx) {
		if c.Ext != "" && c.Ext != "ext-ok" {
			t.Errorf("extensão da dica: %q", c.Ext)
		}
		for i := strings.Index(c.S, "cofre "); i >= 0; i = -1 {
			a := i + len("cofre ")
			b := a
			for b < len(c.S) && isAlnum(c.S[b]) {
				b++
			}
			c.Decide(a, b, "bucket", name)
		}
	}})
	defer func() {
		decidersMu.Lock()
		deciders = deciders[:len(deciders)-1]
		decidersMu.Unlock()
	}()
	m := newTest(t)
	s := "abre o cofre tesouro agora"
	out, ents := m.Mask(s)
	if strings.Contains(out, "tesouro") {
		t.Fatalf("o decisor não mascarou: %s", out)
	}
	if back := NewTable(ents).Unmask(out, false); back != s {
		t.Fatalf("ida e volta: %q", back)
	}
	ds := m.Decided(s, "")
	foundIt := false
	for _, d := range ds {
		if d.Name == "tesouro" && d.Ent == "bucket" && d.Rule == name && !d.Generic {
			foundIt = true
		}
	}
	if !foundIt {
		t.Fatalf("decisão não ficou no memo: %+v", ds)
	}
	// a extensão da dica chega ao decisor, e a dica do leitor de tabela continua legível
	r, _ := m.maskD("abre o cofre tesouro de novo", true, "f||| "+sepExt+"ext-ok")
	if strings.Contains(r.text, "tesouro") || len(r.decidedSet) == 0 {
		t.Fatalf("com dica estendida: %q %+v", r.text, r.decidedSet)
	}
	// palavra comum decidida não vai para o vistos (não tem cara de identificador)
	if _, ok := m.learnedEnt("tesouro"); ok {
		t.Fatalf("decisão de palavra comum foi aprendida")
	}
}

func TestContractReaderDecisions(t *testing.T) {
	m := newTest(t)
	s := "SELECT c_valor FROM sch_fin.t_lanc WHERE id = 1 AND x = 2"
	m.Mask(s)
	ents := map[string]string{}
	for _, d := range m.Decided(s, "") {
		ents[d.Name] = d.Ent
	}
	if ents["t_lanc"] != "tabela" || ents["sch_fin"] != "schema" {
		t.Fatalf("decisões dos leitores: %v", ents)
	}
	if (Decision{Name: "T_LANC", Ent: "tabela"}).Key() != (Decision{Name: "t_lanc", Ent: "tabela"}).Key() {
		t.Fatalf("chave de SQL deveria ser sem caixa")
	}
	if (Decision{Name: "Pagto", Ent: "bucket"}).Key() == (Decision{Name: "pagto", Ent: "bucket"}).Key() {
		t.Fatalf("chave de bucket deveria ser exata")
	}
}

func TestContractGeneric(t *testing.T) {
	prev := publicRef
	defer func() { publicRef = prev }()
	publicRef = map[string]bool{"default": true}
	if !isGeneric("Default") || isGeneric("pagamentos") {
		t.Fatal("ehGenerica")
	}
}

func TestContractWhoWrote(t *testing.T) {
	m := newTest(t)
	_, ents := m.Mask("SELECT a FROM sch_fin.t_lanc WHERE b = 1 AND c = 2")
	tab := NewTable(ents)
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
	des := m.RecordResponse(orig, tab)
	if des != tab.Unmask(orig, false) || !strings.Contains(des, "t_lanc") {
		t.Fatalf("desmascarado: %q", des)
	}
	if o, ok := m.OriginalOf(des); !ok || o != orig {
		t.Fatalf("OriginalDe: %q %v", o, ok)
	}
	tr, ok := m.Translated(des)
	if !ok || len(tr) != 1 || des[tr[0].Start:tr[0].End] != "t_lanc" || tr[0].Pseudo != ps {
		t.Fatalf("Traduzidas: %+v %v", tr, ok)
	}
	if _, ok := m.OriginalOf("texto que ninguém registrou"); ok {
		t.Fatal("sem registro deveria dar false")
	}
}
