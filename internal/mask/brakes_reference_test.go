package mask

import (
	"strings"
	"testing"
)

// Brakes (schema by type, SQL in prose) and derived public reference. Made-up names.

// "name type" decides by the type, with or without indentation.
func TestSchemaByType(t *testing.T) {
	m := newTest(t)
	block := "    cod_pessoa_x      object\n    vlr_saldo_dia       float64\n    dta_nsc_cli  datetime64[ns]\n"
	names := []string{"cod_pessoa_x", "vlr_saldo_dia", "dta_nsc_cli"}
	if ok, tot := checkMasked(t, m, "recuado", block, names); ok != 3 || tot != 3 {
		out, _ := m.Mask(block)
		t.Errorf("bloco recuado: %d/%d\n%s", ok, tot, out)
	}
	// data type in uppercase (SQL) also decides; without indentation, the same
	m = newTest(t)
	ddl := "id_conta_x     NUMBER(12,2)\ndt_abertura_x  TIMESTAMP\nnm_titular_x   VARCHAR(80)\n"
	if ok, tot := checkMasked(t, m, "ddl", ddl, []string{"id_conta_x", "dt_abertura_x", "nm_titular_x"}); ok != tot {
		out, _ := m.Mask(ddl)
		t.Errorf("tipos SQL: %d/%d\n%s", ok, tot, out)
	}
	// negatives: Go struct (language types, pointer, list, qualified), variable block, Python
	// dataclass
	for _, s := range []string{
		"type ContaX struct {\n\tSaldoDia    float64\n\tNomeX       string\n\tAtiva       bool\n\tTotalX      uint64\n\tIdade       int\n\tBuf         strings.Builder\n\tProx        *ContaX\n\tItens       []ItemX\n}\n",
		"type ContaY struct {\n\tsaldo_dia   float64\n\ttotal_x     int64\n}\n",
		"var (\n\tcontador_x  int\n\tnome_x      string\n\tlimite_x    uint64\n)\n",
		"@dataclass\nclass ContaZ:\n    saldo_dia: float\n    nome_x: str\n    ativa_x: bool\n",
	} {
		m := newTest(t)
		if out, _ := m.Mask(s); out != s {
			t.Errorf("não é esquema: %q -> %q", s, out)
		}
	}
	// the pandas footer decides even with only types that are also language types
	m = newTest(t)
	dt := "qtd_itens_x      int64\nvlr_total_x    float64\ndtype: object\n"
	if ok, tot := checkMasked(t, m, "rodapé", dt, []string{"qtd_itens_x", "vlr_total_x"}); ok != tot {
		t.Errorf("dtypes com rodapé: %d/%d", ok, tot)
	}
}

// D2: SQL quoted in prose and in a code comment is not a statement.
func TestSQLInProseAndCode(t *testing.T) {
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
		m := newTest(t)
		if out, _ := m.Mask(s); out != s {
			t.Errorf("não é SQL: %q -> %q", s, out)
		}
	}
	// still apply: a real statement, in uppercase or with an unambiguous shape
	for _, c := range []struct{ s, name string }{
		{"CALL proc_fecha_mes_x(2024);\n", "proc_fecha_mes_x"},
		{"call proc_fecha_mes_x();\n", "proc_fecha_mes_x"},
		{"# delete from tb_log_x where id = 1\n", "tb_log_x"},
		{"-- SELECT id FROM tb_vendas_x WHERE id = 1\n", "tb_vendas_x"},
		{"use db_vendas_x;\n", "db_vendas_x"},
		{"q = \"SELECT id FROM tb_itens_x WHERE id = 1\"  # busca\n", "tb_itens_x"},
	} {
		m := newTest(t)
		if out, _ := m.Mask(c.s); strings.Contains(out, c.name) {
			t.Errorf("SQL de verdade: %q -> %q", c.s, out)
		}
	}
}

// D3/D4: the public reference is generated (not empty, with the expected forms, of contained size);
// a reference word is masked where it was decided, but stays marked as generic (it does not enter
// the conversation memory).
func TestPublicReference(t *testing.T) {
	if len(publicRef) == 0 || len(publicRef) > 2000 {
		t.Fatalf("referência pública: %d palavras", len(publicRef))
	}
	for _, w := range []string{"default", "public", "api", "app"} {
		if !isGeneric(w) {
			t.Errorf("%q deveria estar na referência pública", w)
		}
	}
	for _, w := range []string{"tb_venda_x", "pagamentos_x", "cofre_central"} {
		if isGeneric(w) {
			t.Errorf("%q não deveria estar na referência pública", w)
		}
	}
	if !strings.Contains(publicRefTxt, "# GERADO por TestGerarRefPublica") || !strings.Contains(languageTypesTxt, "# GERADO por TestGerarRefPublica") {
		t.Error("os arquivos de referência precisam do cabeçalho do gerador")
	}
	for _, w := range []string{"string", "int", "bool", "uint64"} {
		if !languageTypes[w] {
			t.Errorf("%q deveria ser tipo de linguagem", w)
		}
	}
	for _, w := range []string{"object", "category", "datetime64", "varchar", "timestamp"} {
		if languageTypes[w] {
			t.Errorf("%q não deveria ser tipo de linguagem", w)
		}
	}
}

func TestGenericInInventory(t *testing.T) {
	gen := ""
	for _, w := range []string{"events", "accounts", "messages", "documents"} {
		if publicRef[w] {
			gen = w
			break
		}
	}
	if gen == "" {
		t.Skip("nenhuma palavra de teste na referência gerada")
	}
	m := newTest(t)
	s := "SELECT id, valor FROM " + gen + " JOIN tb_venda_x ON tb_venda_x.id = " + gen + ".id WHERE valor > 1;"
	out, _ := m.Mask(s)
	if strings.Contains(out, " "+gen+" ") {
		t.Fatalf("palavra da referência no inventário deveria ser mascarada: %s", out)
	}
	var dg, dn *Decision
	ds := m.Decided(s, "")
	for i := range ds {
		switch strings.ToLower(ds[i].Name) {
		case gen:
			dg = &ds[i]
		case "tb_venda_x":
			dn = &ds[i]
		}
	}
	if dg == nil || !dg.Generic {
		t.Fatalf("decisão da palavra genérica: %+v", ds)
	}
	if dn == nil || dn.Generic {
		t.Fatalf("decisão do nome distintivo: %+v", ds)
	}
	// the same through the decider (TextCtx.Decide) (with a reference word: "default" is system
	// vocabulary and is never decided)
	c := &TextCtx{S: "ns " + gen + " x", m: m}
	c.Decide(3, 3+len(gen), "namespace", "teste")
	if len(c.dec) != 1 || !c.dec[0].Generic || len(c.out) != 1 {
		t.Fatalf("Decidir com palavra genérica: %+v %+v", c.dec, c.out)
	}
}
