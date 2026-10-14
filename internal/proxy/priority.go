package proxy

import (
	"regexp"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Anterioridade (ver mask/memory_tracing.go): o modelo só conhece o cliente pelo que o proxy deixa
// passar. Uma palavra que o modelo escreveu (resposta, entrada de ferramenta) antes de qualquer
// dado trazê-la é conhecimento dele: vocabulário público (as colunas de
// SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES que ele usou numa query, os pacotes de um pip install).
// O que o usuário escreve, o system e as ferramentas trazem a palavra só de mencioná-la (o nome
// que o usuário digitou e o modelo repete é do cliente). A saída de uma ferramenta (arquivo,
// comando) só a traz se a decidiu como nome: um changelog público que cita tag_name não torna
// tag_name do cliente. O que a saída mostrou sem decidir já foi em claro para a API quando saiu
// (e o que saiu não é reescrito: enviados.log); o modelo que repete a palavra depois não
// revela nada novo. Word que o proxy traduziu de um pseudônimo nunca conta (veio do
// cliente). Saída sem decisões calculadas (da internet) vale por inteiro.

var rePrevWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*(?:-[A-Za-z0-9_]+)*`)

type priority struct {
	m          *mask.Masker
	decid      map[string][]string // os nomes que cada saída de ferramenta decidiu por si
	data       map[string]bool     // já apareceu num dado
	model      map[string]bool     // apareceu primeiro num texto do modelo
	translates map[string]bool     // o proxy traduziu (nunca é do modelo)
	inData     bool
	output     bool // dentro de um resultado de ferramenta (arquivo, comando)
}

func (a *priority) text(s string) {
	if len(s) < 3 {
		return
	}
	if !a.inData && a.m != nil {
		for _, t := range a.m.TranslatedWords(s) {
			a.translates[strings.ToLower(t.Name)] = true
		}
	}
	if ns, ok := a.decid[s]; ok && a.inData && a.output {
		for _, n := range ns {
			for _, w := range rePrevWord.FindAllString(n, -1) {
				a.data[strings.ToLower(w)] = true
			}
		}
		return
	}
	for _, w := range rePrevWord.FindAllString(s, -1) {
		if len(w) < 3 {
			continue
		}
		k := strings.ToLower(w)
		if a.inData {
			a.data[k] = true
		} else if !a.data[k] {
			a.model[k] = true
		}
	}
}

// tudo: as strings de v (nunca as chaves, nem os blocos assinados de raciocínio).
func (a *priority) all(v any) {
	switch x := v.(type) {
	case string:
		a.text(x)
	case []any:
		for _, e := range x {
			a.all(e)
		}
	case map[string]any:
		t, _ := x["type"].(string)
		if t == "thinking" || t == "redacted_thinking" {
			return
		}
		if t == "tool_result" {
			before := a.output
			a.output = true
			defer func() { a.output = before }()
		}
		// resultado de ferramenta que vem dentro da mensagem do modelo (busca na web, execução de
		// código, MCP do lado da API): o conteúdo é dado, não escrita do modelo
		if strings.HasSuffix(t, "_tool_result") || t == "container_upload" {
			before := a.inData
			a.inData = true
			defer func() { a.inData = before }()
		}
		for k, e := range x {
			if k == "signature" || k == "id" || k == "tool_use_id" || k == "type" {
				continue
			}
			a.all(e)
		}
	}
}

// modelPublics: as palavras que o modelo escreveu antes de um dado trazê-las, nesta conversa
// (decid: os nomes que cada saída de ferramenta decidiu; nil = toda saída vale por inteiro).
func modelPublics(req any, m *mask.Masker, decid map[string][]string) map[string]bool {
	r, _ := req.(map[string]any)
	if r == nil || m == nil {
		return nil
	}
	a := &priority{m: m, decid: decid, data: map[string]bool{}, model: map[string]bool{}, translates: map[string]bool{}}
	a.inData = true
	a.all(r["system"])
	a.all(r["tools"])
	msgs, _ := r["messages"].([]any)
	for _, mm := range msgs {
		msg, _ := mm.(map[string]any)
		if msg == nil {
			continue
		}
		a.inData = msg["role"] != "assistant"
		a.all(msg["content"])
	}
	out := map[string]bool{}
	for k := range a.model {
		if !a.translates[k] {
			out[k] = true
		}
	}
	return out
}
