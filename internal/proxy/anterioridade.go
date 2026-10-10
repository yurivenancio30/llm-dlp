package proxy

import (
	"regexp"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Anterioridade (ver mask/memoria_rastreamento.go): o modelo só conhece o cliente pelo que o proxy deixa
// passar. Uma palavra que o modelo escreveu (resposta, entrada de ferramenta) antes de qualquer
// dado trazê-la é conhecimento dele: vocabulário público (as colunas de
// SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES que ele usou numa query, os pacotes de um pip install).
// O que o usuário escreve, o system e as ferramentas trazem a palavra só de mencioná-la (o nome
// que o usuário digitou e o modelo repete é do cliente). A saída de uma ferramenta (arquivo,
// comando) só a traz se a decidiu como nome: um changelog público que cita tag_name não torna
// tag_name do cliente. O que a saída mostrou sem decidir já foi em claro para a API quando saiu
// (e o que saiu não é reescrito: enviados.log); o modelo que repete a palavra depois não
// revela nada novo. Palavra que o proxy traduziu de um pseudônimo nunca conta (veio do
// cliente). Saída sem decisões calculadas (da internet) vale por inteiro.

var rePalavraAnt = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*(?:-[A-Za-z0-9_]+)*`)

type anterioridade struct {
	m       *mask.Masker
	decid   map[string][]string // os nomes que cada saída de ferramenta decidiu por si
	dados   map[string]bool     // já apareceu num dado
	modelo  map[string]bool     // apareceu primeiro num texto do modelo
	traduz  map[string]bool     // o proxy traduziu (nunca é do modelo)
	emDados bool
	saida   bool // dentro de um resultado de ferramenta (arquivo, comando)
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
	if ns, ok := a.decid[s]; ok && a.emDados && a.saida {
		for _, n := range ns {
			for _, w := range rePalavraAnt.FindAllString(n, -1) {
				a.dados[strings.ToLower(w)] = true
			}
		}
		return
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
		if t == "tool_result" {
			antes := a.saida
			a.saida = true
			defer func() { a.saida = antes }()
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

// publicosDoModelo: as palavras que o modelo escreveu antes de um dado trazê-las, nesta conversa
// (decid: os nomes que cada saída de ferramenta decidiu; nil = toda saída vale por inteiro).
func publicosDoModelo(req any, m *mask.Masker, decid map[string][]string) map[string]bool {
	r, _ := req.(map[string]any)
	if r == nil || m == nil {
		return nil
	}
	a := &anterioridade{m: m, decid: decid, dados: map[string]bool{}, modelo: map[string]bool{}, traduz: map[string]bool{}}
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
