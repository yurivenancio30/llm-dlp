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

// O "llm-dlp ultima" também lê o Gemini CLI com a conta Google (a conversa dentro de "request") e
// o Responses do Codex (instruções e itens).
func TestMensagensDaUltimaAssinaturas(t *testing.T) {
	codeAssist := `{"model":"m","project":"p","request":{"contents":[{"role":"user","parts":[{"text":"oi"}]}]}}`
	if veio, quer := mensagensDaUltima([]byte(codeAssist)), []mensagemVista{{"user", []string{"oi"}}}; !reflect.DeepEqual(veio, quer) {
		t.Errorf("code assist: veio %q", veio)
	}
	responses := `{"instructions":"seja breve","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"oi"}]},
		{"type":"function_call","name":"shell","arguments":"{\"command\":[\"ls\"]}","call_id":"c1"},
		{"type":"function_call_output","call_id":"c1","output":"a.txt"},
		{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch","call_id":"c2"},
		{"type":"reasoning","summary":[]}]}`
	quer := []mensagemVista{{"instructions", []string{"seja breve"}}, {"user", []string{"oi"}},
		{"assistant", []string{`[ferramenta shell] {"command":["ls"]}`}}, {"user", []string{"[resultado de ferramenta]\na.txt"}},
		{"assistant", []string{"[ferramenta apply_patch] *** Begin Patch"}}}
	if veio := mensagensDaUltima([]byte(responses)); !reflect.DeepEqual(veio, quer) {
		t.Errorf("responses:\n veio %q\n quer %q", veio, quer)
	}
}
