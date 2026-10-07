package mask

import (
	"strings"
	"testing"
)

// O conhecimento acumulado (aprendido em textos anteriores, ou de antes de um reinício) segue
// as regras do rastreamento: palavra comum aprendida não volta em todo texto, nome com cara de
// identificador volta, vocabulário público nunca.
func TestConhecimentoAcumuladoSegueAsRegras(t *testing.T) {
	m := novoTeste(t)
	m.Mascarar("SELECT id FROM erro JOIN tb_pedido_cli ON 1=1 JOIN linhagem ON 1=1;")
	out, _ := m.Mascarar("deu erro na linhagem; olhei a tb_pedido_cli e a SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES")
	for _, w := range []string{"erro", "linhagem", "ACCOUNT_USAGE"} {
		if !strings.Contains(out, w) {
			t.Errorf("%q não devia ser trocado por conhecimento acumulado: %s", w, out)
		}
	}
	if strings.Contains(out, "tb_pedido_cli") {
		t.Errorf("nome com cara de identificador aprendido devia voltar mascarado: %s", out)
	}
}

// SQL cortado sem ";" (saída de grep, head, log) seguido de outra coisa: a instrução acaba
// onde a linha de antes está completa e a seguinte é uma palavra solta; nomes de arquivo de
// uma listagem não viram tabela.coluna. SQL formatado em várias linhas continua sendo lido.
func TestSQLCortadoNaoEngoleListagem(t *testing.T) {
	m := novoTeste(t)
	out, _ := m.Mascarar("SELECT a FROM t WHERE x = 1\nlinhagem.sql\nconfere.py\nrelatorio-novo.html\n")
	for _, w := range []string{"linhagem.sql", "confere.py", "relatorio-novo.html"} {
		if !strings.Contains(out, w) {
			t.Errorf("%s da listagem foi lido como SQL: %s", w, out)
		}
	}
	out, _ = m.Mascarar("SELECT\n  t.col_aa1,\n  t.col_bb2\nFROM\n  dw.tb_pedido_zz t\nWHERE t.col_aa1 = 1\n")
	for _, w := range []string{"col_aa1", "col_bb2", "tb_pedido_zz"} {
		if strings.Contains(out, w) {
			t.Errorf("SQL em várias linhas deixou de ser lido (%s em claro): %s", w, out)
		}
	}
}
