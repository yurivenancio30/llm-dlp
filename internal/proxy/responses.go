package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Format Responses da OpenAI (/v1/responses): o do Codex CLI, com a chave de API da OpenAI e com
// a assinatura do ChatGPT (chatgpt.com/backend-api/codex/responses). Vale para todo caminho
// terminado em /responses, de qualquer provedor.
//
// Como no Gemini e no Chat Completions, a ida é vista na forma da Anthropic (responsesBridge) e
// passa pelo mesmo caminho de /v1/messages. O que muda de nome:
//
//	instructions e mensagens "system"/"developer"     -> system
//	tools[] (function, custom)                         -> tools (descrição e schema; o formato
//	                                                      de uma ferramenta custom não muda)
//	input[] "message" (texto, imagem, arquivo)         -> blocos de texto, imagem e PDF
//	input[] "function_call" (arguments é um JSON)      -> tool_use
//	input[] "custom_tool_call" (input é texto livre)   -> tool_use (apply_patch do Codex)
//	input[] "local_shell_call" (action.command)        -> tool_use
//	input[] "function_call_output", "custom_..._output"-> tool_result
//	input[] "reasoning", "web_search_call" e afins     -> não mudam (o modelo só viu pseudônimos)

// parâmetros do Responses que não levam dado do usuário (ou que não podem mudar)
var responsesParams = map[string]bool{"model": true, "input": true, "instructions": true, "tools": true,
	"tool_choice": true, "parallel_tool_calls": true, "reasoning": true, "store": true, "stream": true,
	"include": true, "prompt_cache_key": true, "text": true, "service_tier": true, "max_output_tokens": true,
	"temperature": true, "top_p": true, "truncation": true, "previous_response_id": true, "background": true,
	"top_logprobs": true, "safety_identifier": true, "prompt_cache_retention": true}

// itens que já estão do lado da API, ou que o modelo escreveu a partir de pseudônimos: não mudam
var intactResponsesItems = map[string]bool{"reasoning": true, "web_search_call": true, "image_generation_call": true,
	"compaction": true, "compaction_summary": true, "context_compaction": true, "compaction_trigger": true,
	"item_reference": true, "configuration_update": true, "tool_search_call": true, "tool_search_output": true,
	"file_search_call": true, "code_interpreter_call": true, "mcp_list_tools": true}

// responsesBlocks: o conteúdo de uma mensagem (texto ou lista de partes) como blocos da Anthropic.
func responsesBlocks(content any) (blocks []any, parts []map[string]any) {
	blocks = []any{}
	switch c := content.(type) {
	case string:
		return []any{map[string]any{"type": "text", "text": c}}, nil
	case []any:
		parts = []map[string]any{}
		for _, x := range c {
			pm, _ := x.(map[string]any)
			if pm == nil {
				continue
			}
			b := map[string]any{partMark: len(parts)}
			parts = append(parts, pm)
			media := func(u string) {
				typ, data, ok := dataURL(u)
				switch {
				case !ok:
					b["type"] = "server_tool_use" // endereço ou arquivo já do lado da API: não muda
					return
				case typ == "application/pdf":
					b["type"] = "document"
				case strings.HasPrefix(typ, "image/"):
					b["type"] = "image"
				default:
					delete(b, partMark)
					b["type"], b["text"] = "text", openAIFileNote
					return
				}
				b["source"] = map[string]any{"type": "base64", "media_type": typ, "data": data}
			}
			switch pm["type"] {
			case "input_text", "output_text", "text", "summary_text":
				b["type"], b["text"] = "text", pm["text"]
			case "input_image":
				u, _ := pm["image_url"].(string)
				media(u)
			case "input_file":
				u, _ := pm["file_data"].(string)
				media(u)
			case "input_audio":
				delete(b, partMark) // áudio: não há como verificar; não sai (falha fechada)
				b["type"], b["text"] = "text", openAIFileNote
			case "refusal":
				b["type"] = "server_tool_use" // recusa do próprio modelo: não muda
			default:
				b["type"], b["parte"] = "parte desconhecida", pm // mascara todo texto dela, por precaução
			}
			blocks = append(blocks, b)
		}
	}
	return blocks, parts
}

// responsesContent: os blocos já mascarados, de volta como o conteúdo de uma mensagem do
// Responses. Com partes == nil o conteúdo era um texto.
func responsesContent(blocks any, parts []map[string]any) any {
	bs, _ := blocks.([]any)
	if parts == nil {
		var ts []string
		for _, x := range bs {
			if b, _ := x.(map[string]any); b != nil && b["type"] == "text" {
				t, _ := b["text"].(string)
				ts = append(ts, t)
			}
		}
		return strings.Join(ts, "\n\n")
	}
	out := make([]any, 0, len(bs))
	for _, x := range bs {
		b, _ := x.(map[string]any)
		if b == nil {
			continue
		}
		var pm map[string]any
		if i, ok := b[partMark].(int); ok && i < len(parts) {
			pm = parts[i]
		}
		src, _ := b["source"].(map[string]any)
		switch b["type"] {
		case "text":
			if pm == nil {
				pm = map[string]any{"type": "input_text"}
			}
			pm["text"] = b["text"]
		case "image", "document":
			if src["type"] != "base64" { // PDF que virou texto mascarado
				t, _ := src["data"].(string)
				if title, _ := b["title"].(string); title != "" {
					t = title + "\n\n" + t
				}
				pm = map[string]any{"type": "input_text", "text": t}
				break
			}
			u := fmt.Sprintf("data:%v;base64,%v", src["media_type"], src["data"])
			switch {
			case pm == nil:
				pm = map[string]any{"type": "input_image", "image_url": u}
			case pm["type"] == "input_file":
				pm["file_data"] = u
			default:
				pm["image_url"] = u
			}
		}
		if pm != nil {
			out = append(out, pm)
		}
	}
	return out
}

// responsesBridge devolve a requisição do Responses na forma da Anthropic e a função que, depois de
// mascarada, copia o resultado de volta. ok = false: não é uma requisição com "input" (cai no modo
// genérico, que mascara todo texto).
func responsesBridge(v any) (map[string]any, func(), bool) {
	req, _ := v.(map[string]any)
	entry, ok := req["input"]
	if !ok {
		return nil, nil, false
	}
	anth := map[string]any{}
	var returns []func()

	if ts, _ := req["tools"].([]any); ts != nil {
		tools := []any{}
		for _, t := range ts {
			if tm, ok := t.(map[string]any); ok {
				tools = append(tools, tm) // o mesmo mapa: descrição e schema são mascarados no lugar
			}
		}
		anth["tools"] = tools
	}

	system := []any{}
	if s, ok := req["instructions"].(string); ok {
		b := map[string]any{"type": "text", "text": s}
		system = append(system, b)
		returns = append(returns, func() { req["instructions"] = b["text"] })
	}

	var items []any
	switch x := entry.(type) {
	case string:
		m := map[string]any{"type": "message", "role": "user", "content": x}
		items = []any{m}
		returns = append(returns, func() { req["input"] = m["content"] })
	case []any:
		items = x
	}

	msgs := []any{}
	for _, x := range items {
		it, _ := x.(map[string]any)
		if it == nil {
			continue
		}
		typ, _ := it["type"].(string)
		if typ == "" && it["role"] != nil {
			typ = "message"
		}
		switch {
		case intactResponsesItems[typ]:
			continue
		case typ == "message":
			hostRole, _ := it["role"].(string)
			blocks, parts := responsesBlocks(it["content"])
			if hostRole == "system" || hostRole == "developer" {
				// instruções do cliente: cada texto é mascarado no lugar
				for _, bx := range blocks {
					b, _ := bx.(map[string]any)
					if b == nil || b["type"] != "text" {
						continue
					}
					system = append(system, b)
					if parts == nil {
						returns = append(returns, func() { it["content"] = b["text"] })
					} else if i, ok := b[partMark].(int); ok {
						pm := parts[i]
						returns = append(returns, func() { pm["text"] = b["text"] })
					}
				}
				continue
			}
			am := map[string]any{"role": "user", "content": blocks}
			if hostRole == "assistant" {
				am["role"] = "assistant"
			}
			msgs = append(msgs, am)
			returns = append(returns, func() { it["content"] = responsesContent(am["content"], parts) })
		case typ == "function_call":
			args, _ := it["arguments"].(string)
			var entry any = map[string]any{}
			if strings.TrimSpace(args) != "" {
				if dec, ok := decode([]byte(args)); ok {
					entry = dec
				} else {
					entry = map[string]any{"\x00cru": args} // não é JSON: mascara o texto como veio
				}
			}
			before := encode(entry)
			b := map[string]any{"type": "tool_use", "id": it["call_id"], "name": it["name"], "input": entry}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{b}})
			returns = append(returns, func() {
				// só reescreve os argumentos que mudaram (o texto original fica como o cliente mandou)
				if newOne := encode(b["input"]); string(newOne) != string(before) {
					if m, _ := b["input"].(map[string]any); m != nil && len(m) == 1 && m["\x00cru"] != nil {
						it["arguments"] = m["\x00cru"]
					} else {
						it["arguments"] = string(newOne)
					}
				}
			})
		case typ == "custom_tool_call":
			b := map[string]any{"type": "tool_use", "id": it["call_id"], "name": it["name"], "input": map[string]any{"input": it["input"]}}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{b}})
			returns = append(returns, func() {
				if m, _ := b["input"].(map[string]any); m != nil {
					it["input"] = m["input"]
				}
			})
		case typ == "local_shell_call":
			b := map[string]any{"type": "tool_use", "id": it["call_id"], "name": "local_shell", "input": it["action"]}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{b}})
			returns = append(returns, func() { it["action"] = b["input"] })
		case typ == "function_call_output" || typ == "custom_tool_call_output" || typ == "local_shell_call_output":
			blocks, parts := responsesBlocks(it["output"])
			res := map[string]any{"type": "tool_result", "tool_use_id": it["call_id"], "content": blocks}
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{res}})
			if it["output"] != nil {
				returns = append(returns, func() { it["output"] = responsesContent(res["content"], parts) })
			}
		default:
			// item que não conheço: mascara todo texto dele, por precaução
			b := map[string]any{"type": "parte desconhecida", "parte": it}
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{b}})
		}
	}
	if len(system) > 0 {
		anth["system"] = system
	}
	anth["messages"] = msgs

	for _, k := range keys(req) {
		if !responsesParams[k] { // campo que não conheço: mascara por precaução, como na Anthropic
			k := k
			anth["responses:"+k] = req[k]
			returns = append(returns, func() { req[k] = anth["responses:"+k] })
		}
	}
	return anth, func() {
		for _, f := range returns {
			f()
		}
	}, true
}

// ---- volta --------------------------------------------------------------------------------

// responsesReturn desmascara a resposta do Responses. Em streaming, o texto, os argumentos de uma
// chamada (pedaços de um JSON) e a entrada de uma ferramenta custom (o patch do apply_patch)
// chegam em pedaços e passam cada um por um Stream, que segura o pseudônimo cortado entre dois
// pedaços. Os eventos que trazem o item inteiro (*.done, output_item.done, response.completed)
// são desmascarados inteiros: o cliente pode usar tanto os pedaços quanto o item.
type responsesReturn struct {
	p       *Proxy
	tab     *mask.Table
	streams map[string]*responsesStream // por item (e parte, no texto)
}

type responsesStream struct {
	f        *mask.Stream
	event    string         // o tipo do evento de pedaço, para o pedaço a mais no fim
	fields   map[string]any // item_id, output_index, content_index
	field    string         // "delta"
	rawValue bool           // ferramenta que vai para a internet: segue com os pseudônimos
	isText   bool
	isJSON   bool
	original strings.Builder
}

func (p *Proxy) newResponsesReturn(tab *mask.Table) *responsesReturn {
	return &responsesReturn{p: p, tab: tab, streams: map[string]*responsesStream{}}
}

func (g *responsesReturn) reg(s string) string { return g.p.m.RecordResponse(s, g.tab) }

// cru: o item é de uma ferramenta que vai para a internet (segue com os pseudônimos).
func (g *responsesReturn) rawValue(k string) bool {
	fx := g.streams[k]
	return fx != nil && fx.rawValue
}

func itemKey(ev map[string]any) string {
	id, _ := ev["item_id"].(string)
	if id == "" {
		id = fmt.Sprint("i", intOf(ev["output_index"]))
	}
	return id
}

// unmaskArgs: os argumentos (um JSON em texto) com os valores reais, no mesmo formato em que
// vieram; cada string é registrada (quem escreveu).
func (g *responsesReturn) unmaskArgs(args string) string {
	if dec, ok := decode([]byte(args)); ok {
		recordAll(dec, g.reg)
	}
	return g.tab.Unmask(args, true)
}

// item: desmascara um item inteiro da saída, no lugar.
func (g *responsesReturn) item(it map[string]any) {
	name, _ := it["name"].(string)
	switch it["type"] {
	case "message":
		parts, _ := it["content"].([]any)
		for _, x := range parts {
			if pm, _ := x.(map[string]any); pm != nil {
				if t, ok := pm["text"].(string); ok && pm["type"] != "refusal" {
					pm["text"] = g.reg(t)
				}
			}
		}
	case "function_call":
		if a, ok := it["arguments"].(string); ok && !g.p.bridgeNoUnmask(name) {
			it["arguments"] = g.unmaskArgs(a)
		}
	case "custom_tool_call":
		if s, ok := it["input"].(string); ok && !g.p.bridgeNoUnmask(name) {
			it["input"] = g.reg(s)
		}
	case "local_shell_call":
		it["action"] = recordAll(it["action"], g.reg)
	}
}

// soltar: o que os fluxos de um item ainda seguravam, como eventos de pedaço a mais.
func (g *responsesReturn) release(prefixP string) []map[string]any {
	var ks []string
	for k := range g.streams {
		if k == prefixP || strings.HasPrefix(k, prefixP+"|") || prefixP == "" {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	var out []map[string]any
	for _, k := range ks {
		fx := g.streams[k]
		delete(g.streams, k)
		if fx.rawValue {
			continue
		}
		if rest := fx.f.Close(); rest != "" {
			ev := map[string]any{"type": fx.event, fx.field: rest}
			for c, v := range fx.fields {
				ev[c] = v
			}
			out = append(out, ev)
		}
	}
	return out
}

// evento desmascara um evento do streaming, no lugar. Devolve os eventos de pedaço a mais que
// têm de sair antes dele (o resto que um fluxo segurava quando o item acabou).
func (g *responsesReturn) event(ev map[string]any) []map[string]any {
	typ, _ := ev["type"].(string)
	piece := func(isJSON bool, k string) {
		fx := g.streams[k]
		if fx == nil {
			fx = &responsesStream{f: g.tab.NewStream(isJSON), event: typ, field: "delta", isJSON: isJSON, fields: map[string]any{}}
			for _, c := range []string{"item_id", "output_index", "content_index"} {
				if v, ok := ev[c]; ok {
					fx.fields[c] = v
				}
			}
			g.streams[k] = fx
		}
		s, ok := ev["delta"].(string)
		if !ok || fx.rawValue {
			return
		}
		fx.original.WriteString(s)
		ev["delta"] = fx.f.Push(s)
	}
	switch typ {
	case "response.output_text.delta":
		piece(false, itemKey(ev)+"|"+fmt.Sprint(intOf(ev["content_index"])))
	case "response.function_call_arguments.delta":
		piece(true, itemKey(ev))
	case "response.custom_tool_call_input.delta":
		piece(false, itemKey(ev))
	case "response.output_text.done":
		extra := g.release(itemKey(ev) + "|" + fmt.Sprint(intOf(ev["content_index"])))
		if t, ok := ev["text"].(string); ok {
			ev["text"] = g.reg(t)
		}
		return extra
	case "response.function_call_arguments.done":
		rawValue := g.rawValue(itemKey(ev))
		extra := g.release(itemKey(ev))
		if a, ok := ev["arguments"].(string); ok && !rawValue {
			ev["arguments"] = g.unmaskArgs(a)
		}
		return extra
	case "response.custom_tool_call_input.done":
		rawValue := g.rawValue(itemKey(ev))
		extra := g.release(itemKey(ev))
		if s, ok := ev["input"].(string); ok && !rawValue {
			ev["input"] = g.reg(s)
		}
		return extra
	case "response.content_part.added", "response.content_part.done":
		if pm, _ := ev["part"].(map[string]any); pm != nil {
			if t, ok := pm["text"].(string); ok && pm["type"] != "refusal" {
				pm["text"] = g.tab.Unmask(t, false)
			}
		}
	case "response.output_item.added", "response.output_item.done":
		it, _ := ev["item"].(map[string]any)
		var extra []map[string]any
		if typ == "response.output_item.done" {
			id, _ := it["id"].(string)
			if id == "" {
				id = fmt.Sprint("i", intOf(ev["output_index"]))
			}
			extra = g.release(id)
		}
		if it != nil {
			name, _ := it["name"].(string)
			if typ == "response.output_item.added" && g.p.bridgeNoUnmask(name) {
				// os pedaços dos argumentos de uma ferramenta de internet seguem com os pseudônimos
				id, _ := it["id"].(string)
				g.streams[id] = &responsesStream{f: g.tab.NewStream(true), rawValue: true}
			}
			g.item(it)
		}
		return extra
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		r, _ := ev["response"].(map[string]any)
		output, _ := r["output"].([]any)
		for _, x := range output {
			if it, _ := x.(map[string]any); it != nil {
				g.item(it)
			}
		}
		return g.release("")
	}
	return nil
}

// unmaskSSEResponses: streaming ("event: ..." e "data: {...}", um evento por bloco).
func (p *Proxy) unmaskSSEResponses(w http.ResponseWriter, body io.Reader, tab *mask.Table) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	g := p.newResponsesReturn(tab)
	writeIt := func(ev map[string]any) {
		if t, _ := ev["type"].(string); t != "" {
			fmt.Fprintf(w, "event: %s\n", t)
		}
		fmt.Fprintf(w, "data: %s\n\n", encode(ev))
	}
	var lines []string
	process := func() {
		if len(lines) == 0 {
			return
		}
		defer func() { lines = lines[:0] }()
		data := ""
		for _, l := range lines {
			if d, ok := strings.CutPrefix(l, "data:"); ok {
				data = strings.TrimPrefix(d, " ")
			}
		}
		var ev map[string]any
		if v, ok := decode([]byte(data)); ok && !tab.Empty() {
			ev, _ = v.(map[string]any)
		}
		if ev == nil { // sem nada para desmascarar: passa como veio
			io.WriteString(w, strings.Join(lines, "\n")+"\n\n")
			return
		}
		for _, extra := range g.event(ev) {
			writeIt(extra)
		}
		// o nome do evento continua o que veio (o cliente lê o "type" de dentro dos dados)
		for _, l := range lines {
			if !strings.HasPrefix(l, "data:") {
				io.WriteString(w, l+"\n")
			}
		}
		fmt.Fprintf(w, "data: %s\n\n", encode(ev))
	}
	for {
		line, err := rd.ReadString('\n')
		l := strings.TrimRight(line, "\r\n")
		if l == "" && line != "" {
			process()
			if fl != nil {
				fl.Flush()
			}
		} else if l != "" {
			lines = append(lines, l)
		}
		if err != nil {
			process()
			for _, extra := range g.release("") {
				writeIt(extra)
			}
			if fl != nil {
				fl.Flush()
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// unmaskResponsesJSON: resposta sem streaming. O texto de cada mensagem e a entrada de cada
// chamada de ferramenta voltam reais; o raciocínio não muda.
func (p *Proxy) unmaskResponsesJSON(b []byte, tab *mask.Table) []byte {
	v, ok := decode(b)
	resp, _ := v.(map[string]any)
	if !ok || resp == nil {
		return b
	}
	g := p.newResponsesReturn(tab)
	output, _ := resp["output"].([]any)
	for _, x := range output {
		if it, _ := x.(map[string]any); it != nil {
			g.item(it)
		}
	}
	return encode(resp)
}
