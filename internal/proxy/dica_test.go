package proxy

import (
	"strings"
	"testing"
)

// O resultado de uma ferramenta é tipado pelo comando do tool_use do mesmo
// id. A mesma saída com outro comando não é tipada.
func TestComandoTipaResultado(t *testing.T) {
	saida := "t_lanc_diario\nt_nota_entrada\nt_saldo_mes\n"
	nomes := []string{"t_lanc_diario", "t_nota_entrada", "t_saldo_mes"}
	conversa := func(cmd string) []any {
		return []any{
			msg("user", "quais tabelas existem?"),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "b1", "name": "Bash",
				"input": map[string]any{"command": cmd, "description": "lista as tabelas"}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "b1",
				"content": []any{map[string]any{"type": "text", "text": saida}}}}),
		}
	}
	var corpos [][]byte
	px := montarDisco(t, t.TempDir(), &corpos)
	enviar(t, px, conversa(`psql -At -c "select table_name from information_schema.tables"`))
	r := mensagens(t, corpos[0])
	for _, n := range nomes {
		if strings.Contains(r[2], n) {
			t.Errorf("%s saiu legível no resultado do SELECT", n)
		}
	}
	var corpos2 [][]byte
	px2 := montarDisco(t, t.TempDir(), &corpos2)
	enviar(t, px2, conversa("cat notas.txt"))
	r = mensagens(t, corpos2[0])
	for _, n := range nomes {
		if !strings.Contains(r[2], n) {
			t.Errorf("%s mascarado sem dica do comando", n)
		}
	}
}
