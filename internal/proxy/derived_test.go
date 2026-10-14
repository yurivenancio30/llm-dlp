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
func TestPseudonymDerivedByModel(t *testing.T) {
	const table = "pedidos_acme"
	ddl := "CREATE TABLE " + table + " (id INT PRIMARY KEY, nota TEXT);\n"
	shapes := []struct {
		name    string
		derives func(pseudo string) string
		real    string
		back    bool
	}{
		{"igual", func(p string) string { return p }, table, true},
		{"com prefixo", func(p string) string { return "vw_" + p }, "vw_" + table, true},
		{"com sufixo", func(p string) string { return p + "_bkp" }, table + "_bkp", true},
		{"entre crases", func(p string) string { return "`" + p + "`" }, "`" + table + "`", true},
		{"qualificado", func(p string) string { return "dbo." + p + ".id" }, "dbo." + table + ".id", true},
		// com a caixa trocada pelo modelo, volta o nome real na caixa em que ele entrou
		{"em maiúsculas", func(p string) string { return strings.ToUpper(p) }, table, true},
		{"só a inicial maiúscula", func(p string) string { return strings.ToUpper(p[:1]) + p[1:] }, table, true},
	}
	var bodies [][]byte
	px := buildMem(t, t.TempDir(), &bodies, func(body []byte, w http.ResponseWriter) {
		// o modelo "escreve" um arquivo usando o pseudônimo da tabela em cada forma
		var req struct {
			Messages []json.RawMessage `json:"messages"`
		}
		json.Unmarshal(body, &req)
		m := rePseTab.FindString(string(req.Messages[len(req.Messages)-1]))
		var lines []string
		for _, f := range shapes {
			lines = append(lines, "SELECT * FROM "+f.derives(m)+";")
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"type": "message", "role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t9", "name": "Write",
				"input": map[string]any{"file_path": "/tmp/consultas.sql", "content": strings.Join(lines, "\n")}}}})
	})
	_, rawText := sendRead(t, px, false, []any{msg("user", "gere consultas para o esquema"),
		msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "cat schema.sql"}}}),
		msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": ddl}})})
	var entry struct{ Content string }
	if err := json.Unmarshal([]byte(rawText), &entry); err != nil || entry.Content == "" {
		t.Fatalf("resposta: %s", rawText)
	}
	lines := strings.Split(entry.Content, "\n")
	if len(lines) != len(shapes) {
		t.Fatalf("linhas: %q", lines)
	}
	for i, f := range shapes {
		cameBack := strings.TrimSuffix(strings.TrimPrefix(lines[i], "SELECT * FROM "), ";")
		t.Logf("%-24s volta o real: %-5v -> %s", f.name, cameBack == f.real, cameBack)
		if (cameBack == f.real) != f.back {
			t.Errorf("%s: esperava volta=%v, veio %q", f.name, f.back, cameBack)
		}
	}
}
