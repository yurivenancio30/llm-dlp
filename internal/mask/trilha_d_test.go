package mask

import (
	"strings"
	"testing"
)

// Trilha D da rodada "conhecer o nome": freios (esquema pelo tipo, SQL em prosa) e referência
// pública derivada. Nomes inventados.

// T9: "nome    tipo" decide pelo tipo, com ou sem recuo.
func TestEsquemaPeloTipo(t *testing.T) {
	m := novoTeste(t)
	bloco := "    cod_pessoa_x      object\n    vlr_saldo_dia       float64\n    dta_nsc_cli  datetime64[ns]\n"
	nomes := []string{"cod_pessoa_x", "vlr_saldo_dia", "dta_nsc_cli"}
	if ok, tot := conferirMascarado(t, m, "recuado", bloco, nomes); ok != 3 || tot != 3 {
		out, _ := m.Mascarar(bloco)
		t.Errorf("bloco recuado: %d/%d\n%s", ok, tot, out)
	}
	// tipo de dado em maiúsculas (SQL) também decide; sem recuo, igual
	m = novoTeste(t)
	ddl := "id_conta_x     NUMBER(12,2)\ndt_abertura_x  TIMESTAMP\nnm_titular_x   VARCHAR(80)\n"
	if ok, tot := conferirMascarado(t, m, "ddl", ddl, []string{"id_conta_x", "dt_abertura_x", "nm_titular_x"}); ok != tot {
		out, _ := m.Mascarar(ddl)
		t.Errorf("tipos SQL: %d/%d\n%s", ok, tot, out)
	}
	// negativos: struct Go (tipos de linguagem, ponteiro, lista, qualificado), bloco de
	// variáveis, dataclass Python
	for _, s := range []string{
		"type ContaX struct {\n\tSaldoDia    float64\n\tNomeX       string\n\tAtiva       bool\n\tTotalX      uint64\n\tIdade       int\n\tBuf         strings.Builder\n\tProx        *ContaX\n\tItens       []ItemX\n}\n",
		"type ContaY struct {\n\tsaldo_dia   float64\n\ttotal_x     int64\n}\n",
		"var (\n\tcontador_x  int\n\tnome_x      string\n\tlimite_x    uint64\n)\n",
		"@dataclass\nclass ContaZ:\n    saldo_dia: float\n    nome_x: str\n    ativa_x: bool\n",
	} {
		m := novoTeste(t)
		if out, _ := m.Mascarar(s); out != s {
			t.Errorf("não é esquema: %q -> %q", s, out)
		}
	}
	// o rodapé do pandas decide mesmo só com tipos que também são de linguagem
	m = novoTeste(t)
	dt := "qtd_itens_x      int64\nvlr_total_x    float64\ndtype: object\n"
	if ok, tot := conferirMascarado(t, m, "rodapé", dt, []string{"qtd_itens_x", "vlr_total_x"}); ok != tot {
		t.Errorf("dtypes com rodapé: %d/%d", ok, tot)
	}
}

// D2: SQL citado em prosa e comentário de código não é instrução.
func TestSQLEmProsaECodigo(t *testing.T) {
	for _, s := range []string{
		"// Use merr.Errors to get the list\n\tcount := 1\n",
		"\t\t// io.Copy will use io.WriterTo\n\t\t_, err := io.Copy(io.Discard, dec)\n",
		"        # delete FROM line\n        num_blanks -= 1\n",
		"    '\\x3b'     #  0x3B -> CUSTOMER USE THREE\n    '\\x3c'     #  0x3C -> DEVICE CONTROL FOUR\n",
		"type callData struct {\n\tcall    ast.CallExpr\n\tdesc    string\n}\n",
		"    # in a loop would never call drain(), so it\n",
		"Call data.encode(\"latin-1\") but show a better error message.\n",
		"you can use `termenv.EnvColorProfile` which evaluates the\n",
		"type regs struct {\n\tUse    uint32\n\tFlags  uint32\n}\n",
		"    else:   # proto 0 -- can't use EMPTY_DICT\n        self.write(MARK + DICT)\n",
	} {
		m := novoTeste(t)
		if out, _ := m.Mascarar(s); out != s {
			t.Errorf("não é SQL: %q -> %q", s, out)
		}
	}
	// continuam valendo: instrução de verdade, em maiúsculas ou com forma inequívoca
	for _, c := range []struct{ s, nome string }{
		{"CALL proc_fecha_mes_x(2024);\n", "proc_fecha_mes_x"},
		{"call proc_fecha_mes_x();\n", "proc_fecha_mes_x"},
		{"# delete from tb_log_x where id = 1\n", "tb_log_x"},
		{"-- SELECT id FROM tb_vendas_x WHERE id = 1\n", "tb_vendas_x"},
		{"use db_vendas_x;\n", "db_vendas_x"},
		{"q = \"SELECT id FROM tb_itens_x WHERE id = 1\"  # busca\n", "tb_itens_x"},
	} {
		m := novoTeste(t)
		if out, _ := m.Mascarar(c.s); strings.Contains(out, c.nome) {
			t.Errorf("SQL de verdade: %q -> %q", c.s, out)
		}
	}
}

// D3/D4: a referência pública é gerada (não vazia, com as formas esperadas, de tamanho
// contido); palavra da referência é mascarada onde foi decidida, mas fica marcada como
// genérica (não entra na memória da conversa).
func TestReferenciaPublica(t *testing.T) {
	if len(refPublica) == 0 || len(refPublica) > 2000 {
		t.Fatalf("referência pública: %d palavras", len(refPublica))
	}
	for _, w := range []string{"default", "public", "api", "app"} {
		if !ehGenerica(w) {
			t.Errorf("%q deveria estar na referência pública", w)
		}
	}
	for _, w := range []string{"tb_venda_x", "pagamentos_x", "cofre_central"} {
		if ehGenerica(w) {
			t.Errorf("%q não deveria estar na referência pública", w)
		}
	}
	if !strings.Contains(refPublicaTxt, "# GERADO por TestGerarRefPublica") || !strings.Contains(tiposLinguagemTxt, "# GERADO por TestGerarRefPublica") {
		t.Error("os arquivos de referência precisam do cabeçalho do gerador")
	}
	for _, w := range []string{"string", "int", "bool", "uint64"} {
		if !tiposLinguagem[w] {
			t.Errorf("%q deveria ser tipo de linguagem", w)
		}
	}
	for _, w := range []string{"object", "category", "datetime64", "varchar", "timestamp"} {
		if tiposLinguagem[w] {
			t.Errorf("%q não deveria ser tipo de linguagem", w)
		}
	}
}

func TestGenericaNoInventario(t *testing.T) {
	gen := ""
	for _, w := range []string{"events", "accounts", "messages", "documents"} {
		if refPublica[w] {
			gen = w
			break
		}
	}
	if gen == "" {
		t.Skip("nenhuma palavra de teste na referência gerada")
	}
	m := novoTeste(t)
	s := "SELECT id, valor FROM " + gen + " JOIN tb_venda_x ON tb_venda_x.id = " + gen + ".id WHERE valor > 1;"
	out, _ := m.Mascarar(s)
	if strings.Contains(out, " "+gen+" ") {
		t.Fatalf("palavra da referência no inventário deveria ser mascarada: %s", out)
	}
	var dg, dn *Decisao
	ds := m.Decididos(s, "")
	for i := range ds {
		switch strings.ToLower(ds[i].Nome) {
		case gen:
			dg = &ds[i]
		case "tb_venda_x":
			dn = &ds[i]
		}
	}
	if dg == nil || !dg.Generica {
		t.Fatalf("decisão da palavra genérica: %+v", ds)
	}
	if dn == nil || dn.Generica {
		t.Fatalf("decisão do nome distintivo: %+v", ds)
	}
	// o mesmo pelo decisor (TextoCtx.Decidir)
	// (com uma palavra da referência: "default" é vocabulário de sistema e nunca é decidido)
	c := &TextoCtx{S: "ns " + gen + " x", m: m}
	c.Decidir(3, 3+len(gen), "namespace", "teste")
	if len(c.dec) != 1 || !c.dec[0].Generica || len(c.out) != 1 {
		t.Fatalf("Decidir com palavra genérica: %+v %+v", c.dec, c.out)
	}
}
