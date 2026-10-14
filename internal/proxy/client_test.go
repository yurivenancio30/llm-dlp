package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// O texto do próprio Claude Code (system e ferramentas) quando a conversa aprende, como nome de
// tabela, palavras que ele usa. Names de palavra comum, montados por partes.

var (
	tabCommon  = "bil" + "ling" // palavra comum do inglês, e nome de tabela na conversa
	tabCommon2 = "res" + "ume"
	tabAcronym = "c" + "li"          // não é palavra comum: só a identidade do cliente a protege
	tabName    = "ped" + "idos_acme" // nome com cara de identificador: continua mascarado em todo lugar
)

// O cabeçalho com que o Claude Code se identifica à API, a frase de identidade, as instruções e
// as descrições das ferramentas não mudam quando a conversa aprende como nome de tabela uma
// palavra que eles usam. Com o cabeçalho trocado, a API deixa de reconhecer o Claude Code e
// responde 429 a toda mensagem daquela conversa.
func TestClientTextUnchanged(t *testing.T) {
	var bodies [][]byte
	px := buildDisk(t, t.TempDir(), &bodies)
	hdr := "x-anthropic-" + tabCommon + "-header: cc_version=2.1.296.c17; cc_entrypoint=" + tabAcronym + ";"
	ident := "You are Claude Code, Anthropic's official CLI for Claude."
	instr := "Never show " + tabCommon + " data. To continue a session, " + tabCommon2 + " it."
	desc := "Reads a file. Pass --" + tabCommon2 + " to continue; " + tabCommon + " files are skipped."
	tool := map[string]any{"name": "Read", "description": desc,
		"input_schema": map[string]any{"type": "object",
			"properties": map[string]any{tabCommon: map[string]any{"type": "string", "description": "the " + tabCommon + " file"}},
			"required":   []any{tabCommon}}}
	ddl := "CREATE TABLE " + tabCommon + " (id INT PRIMARY KEY, valor DECIMAL(10,2));\n" +
		"CREATE TABLE " + tabCommon2 + " (id INT PRIMARY KEY, nota TEXT);\n" +
		"CREATE TABLE " + tabAcronym + " (id INT PRIMARY KEY, nota TEXT);\n" +
		"CREATE TABLE " + tabName + " (id INT PRIMARY KEY, nota TEXT);\n"
	body := map[string]any{"model": "x", "max_tokens": 10,
		"system": []any{
			map[string]any{"type": "text", "text": hdr},
			map[string]any{"type": "text", "text": ident},
			map[string]any{"type": "text", "text": instr},
			map[string]any{"type": "text", "text": "Project notes: orders are kept in " + tabName + " and nowhere else."},
		},
		"tools": []any{tool},
		"messages": []any{
			msg("user", "quais tabelas existem?"),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "cat schema.sql"}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": ddl}}),
		}}
	original, _ := json.Marshal(body)
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	var wentOut struct {
		System   []struct{ Text string } `json:"system"`
		Tools    []map[string]any        `json:"tools"`
		Messages []json.RawMessage       `json:"messages"`
	}
	if err := json.Unmarshal(bodies[0], &wentOut); err != nil {
		t.Fatal(err)
	}
	// premissa: a conversa aprendeu os nomes (no DDL eles saem mascarados)
	for _, n := range []string{tabCommon, tabCommon2, tabAcronym, tabName} {
		if strings.Contains(string(wentOut.Messages[2]), "TABLE "+n+" ") {
			t.Fatalf("premissa: a tabela %q devia sair mascarada no DDL: %s", n, wentOut.Messages[2])
		}
	}
	// o nome do cliente que não é palavra comum continua mascarado no texto do system
	if strings.Contains(wentOut.System[3].Text, tabName) {
		t.Errorf("nome aprendido na conversa vazou no system: %s", wentOut.System[3].Text)
	}
	if wentOut.System[0].Text != hdr {
		t.Errorf("o cabeçalho do Claude Code mudou:\n%s\n%s", hdr, wentOut.System[0].Text)
	}
	if wentOut.System[1].Text != ident {
		t.Errorf("a frase de identidade mudou:\n%s\n%s", ident, wentOut.System[1].Text)
	}
	if wentOut.System[2].Text != instr {
		t.Errorf("palavra comum trocada nas instruções:\n%s\n%s", instr, wentOut.System[2].Text)
	}
	var originalTools map[string]any
	b, _ := json.Marshal(tool)
	json.Unmarshal(b, &originalTools)
	if !reflect.DeepEqual(wentOut.Tools[0], originalTools) {
		t.Errorf("a definição da ferramenta mudou:\n%v\n%v", originalTools, wentOut.Tools[0])
	}
}

// O cabeçalho só passa intacto com a forma exata dele (nome=valor curtos, sem texto livre) e no
// system; o mesmo texto com um dado dentro, ou na mensagem do usuário, é mascarado como os outros.
func TestClientHeaderShapeOnly(t *testing.T) {
	for _, s := range []string{
		"x-anthropic-billing-header: cc_version=2.1.296.c17; cc_entrypoint=claude-vscode;",
		"x-anthropic-billing-header: cc_version=2.1.92.a3f; cc_entrypoint=cli; cch=00000;",
		"You are Claude Code, Anthropic's official CLI for Claude.",
		"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.",
	} {
		if !clientFixedText(s) {
			t.Errorf("devia passar intacto: %q", s)
		}
	}
	for _, s := range []string{
		"x-anthropic-billing-header: cc_version=2.1.296.c17; contato " + email + ";",
		"x-anthropic-billing-header: cc_version=2.1.296.c17;\nUsuário logado: " + email,
		"x-anthropic-billing-header: nota=o owner e " + email + ";",
		"x-anthropic-billing-header:",
		"You are Claude Code, Anthropic's official CLI for Claude. Usuário logado: " + email,
		"",
	} {
		if clientFixedText(s) {
			t.Errorf("não devia passar intacto: %q", s)
		}
	}

	var bodies [][]byte
	px := buildDisk(t, t.TempDir(), &bodies)
	hdr := "x-anthropic-billing-header: cc_version=2.1.296.c17; cc_entrypoint=cli;"
	body, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10,
		"system":   []any{map[string]any{"type": "text", "text": hdr}, map[string]any{"type": "text", "text": "Usuário logado: " + email}},
		"messages": []any{msg("user", hdr+" e o owner "+email)}})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if bytes.Contains(bodies[0], []byte(email)) {
		t.Errorf("dado vazou ao lado do cabeçalho: %s", bodies[0])
	}
	if !bytes.Contains(bodies[0], []byte(hdr)) {
		t.Errorf("o cabeçalho devia sair intacto: %s", bodies[0])
	}
}
