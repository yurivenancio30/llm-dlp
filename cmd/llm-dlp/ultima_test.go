package main

import (
	"reflect"
	"testing"
)

// "llm-dlp ultima" lê a última requisição nos dois formatos: o da Anthropic (messages) e o do
// Gemini (contents, com chamada e resultado de ferramenta).
func TestMensagensDaUltima(t *testing.T) {
	casos := []struct {
		nome, corpo string
		quer        []mensagemVista
	}{
		{"anthropic", `{"messages":[{"role":"user","content":"oi"},
			{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"a.txt"}}]},
			{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"linha"}]}]}]}`,
			[]mensagemVista{{"user", []string{"oi"}},
				{"assistant", []string{`[ferramenta Read] {"file_path":"a.txt"}`}},
				{"user", []string{"[resultado de ferramenta]\nlinha"}}}},
		{"gemini", `{"contents":[{"role":"user","parts":[{"text":"oi"}]},
			{"role":"model","parts":[{"text":"vou ler"},{"functionCall":{"name":"read_file","args":{"path":"a.txt"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"read_file","response":{"output":"linha"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"name":"read","content":"linha"}}},
				{"functionResponse":{"name":"conta","response":{"total":2}}},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`,
			[]mensagemVista{{"user", []string{"oi"}},
				{"model", []string{"vou ler", `[ferramenta read_file] {"path":"a.txt"}`}},
				{"user", []string{"[resultado de ferramenta]\nlinha"}},
				{"user", []string{"[resultado de ferramenta]\ncontent: linha\nname: read", "[resultado de ferramenta]\n" + `{"total":2}`, "[arquivo]"}}}},
		{"vazio", `{}`, nil},
		{"não é JSON", `x`, nil},
	}
	for _, c := range casos {
		if veio := mensagensDaUltima([]byte(c.corpo)); !reflect.DeepEqual(veio, c.quer) {
			t.Errorf("%s:\n veio %q\n quer %q", c.nome, veio, c.quer)
		}
	}
}
