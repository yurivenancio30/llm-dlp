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
