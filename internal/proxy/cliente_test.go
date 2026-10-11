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
// tabela, palavras que ele usa. Nomes de palavra comum, montados por partes.

var (
	tabComum  = "bil" + "ling" // palavra comum do inglês, e nome de tabela na conversa
	tabComum2 = "res" + "ume"
	tabSigla  = "c" + "li"          // não é palavra comum: só a identidade do cliente a protege
	tabNome   = "ped" + "idos_acme" // nome com cara de identificador: continua mascarado em todo lugar
)

// O cabeçalho com que o Claude Code se identifica à API, a frase de identidade, as instruções e
// as descrições das ferramentas não mudam quando a conversa aprende como nome de tabela uma
// palavra que eles usam. Com o cabeçalho trocado, a API deixa de reconhecer o Claude Code e
// responde 429 a toda mensagem daquela conversa.
func TestTextoDoClienteNaoMuda(t *testing.T) {
	var corpos [][]byte
	px := montarDisco(t, t.TempDir(), &corpos)
	cab := "x-anthropic-" + tabComum + "-header: cc_version=2.1.296.c17; cc_entrypoint=" + tabSigla + ";"
	ident := "You are Claude Code, Anthropic's official CLI for Claude."
	instr := "Never show " + tabComum + " data. To continue a session, " + tabComum2 + " it."
	desc := "Reads a file. Pass --" + tabComum2 + " to continue; " + tabComum + " files are skipped."
	ferramenta := map[string]any{"name": "Read", "description": desc,
		"input_schema": map[string]any{"type": "object",
			"properties": map[string]any{tabComum: map[string]any{"type": "string", "description": "the " + tabComum + " file"}},
			"required":   []any{tabComum}}}
	ddl := "CREATE TABLE " + tabComum + " (id INT PRIMARY KEY, valor DECIMAL(10,2));\n" +
		"CREATE TABLE " + tabComum2 + " (id INT PRIMARY KEY, nota TEXT);\n" +
		"CREATE TABLE " + tabSigla + " (id INT PRIMARY KEY, nota TEXT);\n" +
		"CREATE TABLE " + tabNome + " (id INT PRIMARY KEY, nota TEXT);\n"
	corpo := map[string]any{"model": "x", "max_tokens": 10,
		"system": []any{
			map[string]any{"type": "text", "text": cab},
			map[string]any{"type": "text", "text": ident},
			map[string]any{"type": "text", "text": instr},
			map[string]any{"type": "text", "text": "Project notes: orders are kept in " + tabNome + " and nowhere else."},
		},
		"tools": []any{ferramenta},
		"messages": []any{
			msg("user", "quais tabelas existem?"),
			msg("assistant", []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "cat schema.sql"}}}),
			msg("user", []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": ddl}}),
		}}
	original, _ := json.Marshal(corpo)
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	var saiu struct {
		System   []struct{ Text string } `json:"system"`
		Tools    []map[string]any        `json:"tools"`
		Messages []json.RawMessage       `json:"messages"`
	}
	if err := json.Unmarshal(corpos[0], &saiu); err != nil {
		t.Fatal(err)
	}
	// premissa: a conversa aprendeu os nomes (no DDL eles saem mascarados)
	for _, n := range []string{tabComum, tabComum2, tabSigla, tabNome} {
		if strings.Contains(string(saiu.Messages[2]), "TABLE "+n+" ") {
			t.Fatalf("premissa: a tabela %q devia sair mascarada no DDL: %s", n, saiu.Messages[2])
		}
	}
	// o nome do cliente que não é palavra comum continua mascarado no texto do system
	if strings.Contains(saiu.System[3].Text, tabNome) {
		t.Errorf("nome aprendido na conversa vazou no system: %s", saiu.System[3].Text)
	}
	if saiu.System[0].Text != cab {
		t.Errorf("o cabeçalho do Claude Code mudou:\n%s\n%s", cab, saiu.System[0].Text)
	}
	if saiu.System[1].Text != ident {
		t.Errorf("a frase de identidade mudou:\n%s\n%s", ident, saiu.System[1].Text)
	}
	if saiu.System[2].Text != instr {
		t.Errorf("palavra comum trocada nas instruções:\n%s\n%s", instr, saiu.System[2].Text)
	}
	var ferrOriginal map[string]any
	b, _ := json.Marshal(ferramenta)
	json.Unmarshal(b, &ferrOriginal)
	if !reflect.DeepEqual(saiu.Tools[0], ferrOriginal) {
		t.Errorf("a definição da ferramenta mudou:\n%v\n%v", ferrOriginal, saiu.Tools[0])
	}
}

// O cabeçalho só passa intacto com a forma exata dele (nome=valor curtos, sem texto livre) e no
// system; o mesmo texto com um dado dentro, ou na mensagem do usuário, é mascarado como os outros.
func TestCabecalhoDoClienteSoComAForma(t *testing.T) {
	for _, s := range []string{
		"x-anthropic-billing-header: cc_version=2.1.296.c17; cc_entrypoint=claude-vscode;",
		"x-anthropic-billing-header: cc_version=2.1.92.a3f; cc_entrypoint=cli; cch=00000;",
		"You are Claude Code, Anthropic's official CLI for Claude.",
		"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.",
	} {
		if !textoFixoDoCliente(s) {
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
		if textoFixoDoCliente(s) {
			t.Errorf("não devia passar intacto: %q", s)
		}
	}

	var corpos [][]byte
	px := montarDisco(t, t.TempDir(), &corpos)
	cab := "x-anthropic-billing-header: cc_version=2.1.296.c17; cc_entrypoint=cli;"
	corpo, _ := json.Marshal(map[string]any{"model": "x", "max_tokens": 10,
		"system":   []any{map[string]any{"type": "text", "text": cab}, map[string]any{"type": "text", "text": "Usuário logado: " + email}},
		"messages": []any{msg("user", cab+" e o owner "+email)}})
	resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if bytes.Contains(corpos[0], []byte(email)) {
		t.Errorf("dado vazou ao lado do cabeçalho: %s", corpos[0])
	}
	if !bytes.Contains(corpos[0], []byte(cab)) {
		t.Errorf("o cabeçalho devia sair intacto: %s", corpos[0])
	}
}
