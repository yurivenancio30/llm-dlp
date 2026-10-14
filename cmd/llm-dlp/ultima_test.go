package main

import (
	"reflect"
	"testing"
)

// "llm-dlp last" lê a última requisição nos dois formatos: o da Anthropic (messages) e o do
// Gemini (contents, com chamada e resultado de ferramenta).
func TestLastMessages(t *testing.T) {
	cases := []struct {
		name, body string
		wants      []seenMessage
	}{
		{"anthropic", `{"messages":[{"role":"user","content":"oi"},
			{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"a.txt"}}]},
			{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"linha"}]}]}]}`,
			[]seenMessage{{"user", []string{"oi"}},
				{"assistant", []string{`[ferramenta Read] {"file_path":"a.txt"}`}},
				{"user", []string{"[resultado de ferramenta]\nlinha"}}}},
		{"gemini", `{"contents":[{"role":"user","parts":[{"text":"oi"}]},
			{"role":"model","parts":[{"text":"vou ler"},{"functionCall":{"name":"read_file","args":{"path":"a.txt"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"read_file","response":{"output":"linha"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"name":"read","content":"linha"}}},
				{"functionResponse":{"name":"conta","response":{"total":2}}},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`,
			[]seenMessage{{"user", []string{"oi"}},
				{"model", []string{"vou ler", `[ferramenta read_file] {"path":"a.txt"}`}},
				{"user", []string{"[resultado de ferramenta]\nlinha"}},
				{"user", []string{"[resultado de ferramenta]\ncontent: linha\nname: read", "[resultado de ferramenta]\n" + `{"total":2}`, "[arquivo]"}}}},
		{"vazio", `{}`, nil},
		{"não é JSON", `x`, nil},
	}
	for _, c := range cases {
		if came := lastMessages([]byte(c.body)); !reflect.DeepEqual(came, c.wants) {
			t.Errorf("%s:\n veio %q\n quer %q", c.name, came, c.wants)
		}
	}
}

// O "llm-dlp last" também lê o Gemini CLI com a conta Google (a conversa dentro de "request") e
// o Responses do Codex (instruções e itens).
func TestLastMessagesSignatures(t *testing.T) {
	codeAssist := `{"model":"m","project":"p","request":{"contents":[{"role":"user","parts":[{"text":"oi"}]}]}}`
	if came, wants := lastMessages([]byte(codeAssist)), []seenMessage{{"user", []string{"oi"}}}; !reflect.DeepEqual(came, wants) {
		t.Errorf("code assist: veio %q", came)
	}
	responses := `{"instructions":"seja breve","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"oi"}]},
		{"type":"function_call","name":"shell","arguments":"{\"command\":[\"ls\"]}","call_id":"c1"},
		{"type":"function_call_output","call_id":"c1","output":"a.txt"},
		{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch","call_id":"c2"},
		{"type":"reasoning","summary":[]}]}`
	wants := []seenMessage{{"instructions", []string{"seja breve"}}, {"user", []string{"oi"}},
		{"assistant", []string{`[ferramenta shell] {"command":["ls"]}`}}, {"user", []string{"[resultado de ferramenta]\na.txt"}},
		{"assistant", []string{"[ferramenta apply_patch] *** Begin Patch"}}}
	if came := lastMessages([]byte(responses)); !reflect.DeepEqual(came, wants) {
		t.Errorf("responses:\n veio %q\n quer %q", came, wants)
	}
}
