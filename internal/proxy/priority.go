package proxy

import (
	"regexp"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Priority (see mask/memory_tracing.go): the model only knows the client through what the proxy
// lets through. A word the model wrote (response, tool input) before any data brought it is its own
// knowledge: public vocabulary (the columns of SNOWFLAKE.ACCOUNT_USAGE.TAG_REFERENCES it used in a
// query, the packages of a pip install). What the user writes, the system and the tools bring the
// word just by mentioning it (the name the user typed and the model repeats belongs to the client).
// A tool output (file, command) only brings it if it decided it as a name: a public changelog that
// cites tag_name does not make tag_name the client's. What the output showed without deciding
// already went to the API in the clear when it went out (and what went out is not rewritten:
// enviados.log); the model that repeats the word later reveals nothing new. A word the proxy
// translated from a pseudonym never counts (it came from the client). Output without computed
// decisions (from the internet) counts in full.

var rePrevWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*(?:-[A-Za-z0-9_]+)*`)

type priority struct {
	m          *mask.Masker
	decid      map[string][]string // the names each tool output decided by itself
	data       map[string]bool     // already appeared in data
	model      map[string]bool     // appeared first in a model text
	translates map[string]bool     // the proxy translated it (never the model's)
	inData     bool
	output     bool // inside a tool result (file, command)
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

// all: the strings of v (never the keys, nor the signed reasoning blocks).
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
		// a tool result that comes inside the model message (web search, code execution, API-side
		// MCP): the content is data, not model writing
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

// modelPublics: the words the model wrote before data brought them, in this conversation (decid:
// the names each tool output decided; nil = every output counts in full).
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
