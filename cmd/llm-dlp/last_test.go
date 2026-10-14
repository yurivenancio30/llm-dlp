package main

import (
	"reflect"
	"testing"
)

// "llm-dlp last" reads the last request in both formats: Anthropic's (messages) and Gemini's
// (contents, with tool call and tool result).
func TestLastMessages(t *testing.T) {
	cases := []struct {
		name, body string
		wants      []seenMessage
	}{
		{"anthropic", `{"messages":[{"role":"user","content":"oi"},
			{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"a.txt"}}]},
			{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"linha"}]}]}]}`,
			[]seenMessage{{"user", []string{"oi"}},
				{"assistant", []string{`[tool Read] {"file_path":"a.txt"}`}},
				{"user", []string{"[tool result]\nlinha"}}}},
		{"gemini", `{"contents":[{"role":"user","parts":[{"text":"oi"}]},
			{"role":"model","parts":[{"text":"vou ler"},{"functionCall":{"name":"read_file","args":{"path":"a.txt"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"read_file","response":{"output":"linha"}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"name":"read","content":"linha"}}},
				{"functionResponse":{"name":"conta","response":{"total":2}}},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`,
			[]seenMessage{{"user", []string{"oi"}},
				{"model", []string{"vou ler", `[tool read_file] {"path":"a.txt"}`}},
				{"user", []string{"[tool result]\nlinha"}},
				{"user", []string{"[tool result]\ncontent: linha\nname: read", "[tool result]\n" + `{"total":2}`, "[file]"}}}},
		{"vazio", `{}`, nil},
		{"não é JSON", `x`, nil},
	}
	for _, c := range cases {
		if came := lastMessages([]byte(c.body)); !reflect.DeepEqual(came, c.wants) {
			t.Errorf("%s:\n got  %q\n want %q", c.name, came, c.wants)
		}
	}
}

// "llm-dlp last" also reads Gemini CLI with the Google account (the conversation inside "request")
// and Codex's Responses (instructions and items).
func TestLastMessagesSignatures(t *testing.T) {
	codeAssist := `{"model":"m","project":"p","request":{"contents":[{"role":"user","parts":[{"text":"oi"}]}]}}`
	if came, wants := lastMessages([]byte(codeAssist)), []seenMessage{{"user", []string{"oi"}}}; !reflect.DeepEqual(came, wants) {
		t.Errorf("code assist: got %q", came)
	}
	responses := `{"instructions":"seja breve","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"oi"}]},
		{"type":"function_call","name":"shell","arguments":"{\"command\":[\"ls\"]}","call_id":"c1"},
		{"type":"function_call_output","call_id":"c1","output":"a.txt"},
		{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch","call_id":"c2"},
		{"type":"reasoning","summary":[]}]}`
	wants := []seenMessage{{"instructions", []string{"seja breve"}}, {"user", []string{"oi"}},
		{"assistant", []string{`[tool shell] {"command":["ls"]}`}}, {"user", []string{"[tool result]\na.txt"}},
		{"assistant", []string{"[tool apply_patch] *** Begin Patch"}}}
	if came := lastMessages([]byte(responses)); !reflect.DeepEqual(came, wants) {
		t.Errorf("responses:\n got  %q\n want %q", came, wants)
	}
}
