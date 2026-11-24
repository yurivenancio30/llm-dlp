package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The name of a declared tool goes in the clear in the tools list. In the same request it stays also
// in a text, even when the memory learned it as an object by mistake (it happened with "WebSearch" as
// a database). A tool_reference (which tool a tool search loads) never changes: with a pseudonym the
// API refuses the request ("Tool reference 'Db_…' not found").
func TestDeclaredToolNameStays(t *testing.T) {
	var bodies [][]byte
	px, m := buildWithMasker(t, &bodies)
	m.Mask("CREATE DATABASE WebSearch;")
	if out, _ := m.Mask("o WebSearch caiu"); strings.Contains(out, "WebSearch") {
		t.Fatalf("the memory did not learn the name; the test proves nothing: %q", out)
	}
	tool := func(name string) map[string]any {
		return map[string]any{"name": name, "description": "x", "input_schema": map[string]any{"type": "object"}}
	}
	post := func(tools []any, text string) string {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10, "tools": tools, "messages": []any{
			msg("user", text),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "s1", "name": "ToolSearch",
				"input": map[string]any{"query": "select:WebSearch"}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "s1",
				"content": []any{map[string]any{"type": "tool_reference", "tool_name": "WebSearch"}}}}),
		}})
		resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return string(bodies[len(bodies)-1])
	}
	const ref = `{"tool_name":"WebSearch","type":"tool_reference"}`

	out := post([]any{tool("ToolSearch"), tool("WebSearch")}, "procure no WebSearch o changelog")
	if !strings.Contains(out, "procure no WebSearch o changelog") {
		t.Errorf("declared tool name masked in the text: %s", out)
	}
	if !strings.Contains(out, ref) {
		t.Errorf("tool_reference changed: %s", out)
	}

	// not declared in the request: the memory applies in the text, but the reference never changes
	out = post([]any{tool("ToolSearch")}, "procure no WebSearch a versão")
	if strings.Contains(out, "procure no WebSearch a versão") {
		t.Errorf("undeclared name should follow the memory: %s", out)
	}
	if !strings.Contains(out, ref) {
		t.Errorf("tool_reference changed: %s", out)
	}
}
