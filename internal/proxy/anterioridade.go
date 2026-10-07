package proxy

import (
	"regexp"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Anterioridade (ver mask/rastreamento.go): o modelo só conhece o cliente pelo que o proxy deixa
// passar. Uma palavra que aparece PRIMEIRO num texto que o modelo escreveu (resposta, entrada de
// ferramenta), antes de qualquer dado (system, ferramentas, mensagem do usuário, saída de
// ferramenta), é conhecimento dele: vocabulário público (as colunas de
// SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES que ele usou numa query, os pacotes de um pip install).
// Palavra que o proxy traduziu de um pseudônimo nunca conta (veio do cliente).

var rePalavraAnt = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*(?:-[A-Za-z0-9_]+)*`)

type anterioridade struct {
	m       *mask.Masker
	dados   map[string]bool // já apareceu num dado
	modelo  map[string]bool // apareceu primeiro num texto do modelo
	traduz  map[string]bool // o proxy traduziu (nunca é do modelo)
	emDados bool
}

func (a *anterioridade) texto(s string) {
	if len(s) < 3 {
		return
	}
	if !a.emDados && a.m != nil {
		for _, t := range a.m.PalavrasTraduzidas(s) {
			a.traduz[strings.ToLower(t.Nome)] = true
		}
	}
	for _, w := range rePalavraAnt.FindAllString(s, -1) {
		if len(w) < 3 {
			continue
		}
		k := strings.ToLower(w)
		if a.emDados {
			a.dados[k] = true
		} else if !a.dados[k] {
			a.modelo[k] = true
		}
	}
}

// tudo: as strings de v (nunca as chaves, nem os blocos assinados de raciocínio).
func (a *anterioridade) tudo(v any) {
	switch x := v.(type) {
	case string:
		a.texto(x)
	case []any:
		for _, e := range x {
			a.tudo(e)
		}
	case map[string]any:
		t, _ := x["type"].(string)
		if t == "thinking" || t == "redacted_thinking" {
			return
		}
		// resultado de ferramenta que vem dentro da mensagem do modelo (busca na web, execução de
		// código, MCP do lado da API): o conteúdo é dado, não escrita do modelo
		if strings.HasSuffix(t, "_tool_result") || t == "container_upload" {
			antes := a.emDados
			a.emDados = true
			defer func() { a.emDados = antes }()
		}
		for k, e := range x {
			if k == "signature" || k == "id" || k == "tool_use_id" || k == "type" {
				continue
			}
			a.tudo(e)
		}
	}
}

// publicosDoModelo: as palavras que o modelo escreveu antes de qualquer dado, nesta conversa.
func publicosDoModelo(req any, m *mask.Masker) map[string]bool {
	r, _ := req.(map[string]any)
	if r == nil || m == nil {
		return nil
	}
	a := &anterioridade{m: m, dados: map[string]bool{}, modelo: map[string]bool{}, traduz: map[string]bool{}}
	a.emDados = true
	a.tudo(r["system"])
	a.tudo(r["tools"])
	msgs, _ := r["messages"].([]any)
	for _, mm := range msgs {
		msg, _ := mm.(map[string]any)
		if msg == nil {
			continue
		}
		a.emDados = msg["role"] != "assistant"
		a.tudo(msg["content"])
	}
	out := map[string]bool{}
	for k := range a.modelo {
		if !a.traduz[k] {
			out[k] = true
		}
	}
	return out
}
