package proxy

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var rePseTab = regexp.MustCompile(`\bt_[a-z2-7]{8}\b`)

// O que acontece quando o modelo não devolve o pseudônimo exatamente como recebeu: num arquivo
// gerado, nenhuma das formas abaixo deixa o pseudônimo no arquivo. Ele é trocado onde aparece
// inteiro, também dentro de um nome maior (vw_<nome>, <nome>_bkp); com a caixa trocada, volta o
// nome real na caixa em que entrou na conversa.
func TestPseudonimoDerivadoPeloModelo(t *testing.T) {
	const tabela = "pedidos_acme"
	ddl := "CREATE TABLE " + tabela + " (id INT PRIMARY KEY, nota TEXT);\n"
	formas := []struct {
		nome   string
		deriva func(pseudo string) string
		real   string
		volta  bool
	}{
		{"igual", func(p string) string { return p }, tabela, true},
		{"com prefixo", func(p string) string { return "vw_" + p }, "vw_" + tabela, true},
		{"com sufixo", func(p string) string { return p + "_bkp" }, tabela + "_bkp", true},
		{"entre crases", func(p string) string { return "`" + p + "`" }, "`" + tabela + "`", true},
		{"qualificado", func(p string) string { return "dbo." + p + ".id" }, "dbo." + tabela + ".id", true},
		// com a caixa trocada pelo modelo, volta o nome real na caixa em que ele entrou
		{"em maiúsculas", func(p string) string { return strings.ToUpper(p) }, tabela, true},
		{"só a inicial maiúscula", func(p string) string { return strings.ToUpper(p[:1]) + p[1:] }, tabela, true},
	}
	var corpos [][]byte
	px := montarMem(t, t.TempDir(), &corpos, func(corpo []byte, w http.ResponseWriter) {
		// o modelo "escreve" um arquivo usando o pseudônimo da tabela em cada forma
		var req struct {
			Messages []json.RawMessage `json:"messages"`
		}
		json.Unmarshal(corpo, &req)
		m := rePseTab.FindString(string(req.Messages[len(req.Messages)-1]))
		var linhas []string
		for _, f := range formas {
			linhas = append(linhas, "SELECT * FROM "+f.deriva(m)+";")
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t9", "name": "Write",
				"input": map[string]any{"file_path": "/tmp/consultas.sql", "content": strings.Join(linhas, "\n")}}}})
	})
	_, bruto := enviarLer(t, px, false, []any{msg("user", "gere consultas para o esquema"),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "cat schema.sql"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": ddl}})})
	var entrada struct{ Content string }
	if err := json.Unmarshal([]byte(bruto), &entrada); err != nil || entrada.Content == "" {
		t.Fatalf("resposta: %s", bruto)
	}
	linhas := strings.Split(entrada.Content, "\n")
	if len(linhas) != len(formas) {
		t.Fatalf("linhas: %q", linhas)
	}
	for i, f := range formas {
		voltou := strings.TrimSuffix(strings.TrimPrefix(linhas[i], "SELECT * FROM "), ";")
		t.Logf("%-24s volta o real: %-5v -> %s", f.nome, voltou == f.real, voltou)
		if (voltou == f.real) != f.volta {
			t.Errorf("%s: esperava volta=%v, veio %q", f.nome, f.volta, voltou)
		}
	}
}
