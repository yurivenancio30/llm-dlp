package proxy

import (
	"strings"
	"testing"
)

// The result of a tool is typed by the command of the tool_use with the same id. The same output
// with another command is not typed.
func TestCommandTypesResult(t *testing.T) {
	output := "t_lanc_diario\nt_nota_entrada\nt_saldo_mes\n"
	names := []string{"t_lanc_diario", "t_nota_entrada", "t_saldo_mes"}
	conversation := func(cmd string) []any {
		return []any{
			msg("user", "quais tabelas existem?"),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "b1", "name": "Bash",
				"input": map[string]any{"command": cmd, "description": "lista as tabelas"}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "b1",
				"content": []any{map[string]any{"type": "text", "text": output}}}}),
		}
	}
	var bodies [][]byte
	px := buildDisk(t, t.TempDir(), &bodies)
	send(t, px, conversation(`psql -At -c "select table_name from information_schema.tables"`))
	r := messages(t, bodies[0])
	for _, n := range names {
		if strings.Contains(r[2], n) {
			t.Errorf("%s saiu legível no resultado do SELECT", n)
		}
	}
	var bodies2 [][]byte
	px2 := buildDisk(t, t.TempDir(), &bodies2)
	send(t, px2, conversation("cat notas.txt"))
	r = messages(t, bodies2[0])
	for _, n := range names {
		if !strings.Contains(r[2], n) {
			t.Errorf("%s mascarado sem dica do comando", n)
		}
	}
}
