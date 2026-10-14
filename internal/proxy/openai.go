package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Format da OpenAI (Chat Completions): o que o Copilot (provedor próprio), o opencode e a
// maioria dos clientes mandam para a OpenAI, a xAI (Grok), o OpenRouter e o endereço compatível
// do Gemini. Vale para todo caminho terminado em /chat/completions, de qualquer provedor.
//
// Como no Gemini (gemini.go), a ida não tem um mascarador próprio: a requisição é vista na forma
// da Anthropic (openAIBridge) e passa pelo mesmo caminho de /v1/messages. O que muda de nome:
//
//	messages[] com role "system"/"developer"   -> system
//	tools[].function                           -> tools (descrição e schema)
//	messages[].content (texto ou partes)       -> blocos de texto, imagem e PDF
//	messages[].tool_calls[].function           -> tool_use (arguments é um JSON em texto)
//	messages[] com role "tool"                 -> tool_result
//	reasoning_content / reasoning              -> não muda (o modelo só viu pseudônimos)

// parâmetros da OpenAI que não levam dado do usuário (ou que não podem mudar)
var openAIParams = map[string]bool{"messages": true, "tools": true, "model": true, "stream": true, "stream_options": true,
	"tool_choice": true, "parallel_tool_calls": true, "temperature": true, "top_p": true, "n": true, "max_tokens": true,
	"max_completion_tokens": true, "presence_penalty": true, "frequency_penalty": true, "logit_bias": true, "logprobs": true,
	"top_logprobs": true, "seed": true, "stop": true, "reasoning_effort": true, "reasoning": true, "service_tier": true,
	"store": true, "modalities": true, "verbosity": true, "prompt_cache_key": true, "safety_identifier": true}

// callMark: no tool_use criado pela ponte, o índice da chamada (tool_calls) de onde ele veio.
const callMark = "\x00chamada"

const openAIFileNote = "[llm-dlp] Um arquivo anexado foi retirado daqui e não foi enviado: o llm-dlp só consegue verificar imagem e PDF. Avise o usuário."

// dataURL separa "data:<tipo>;base64,<dados>".
func dataURL(u string) (typ, data string, ok bool) {
	rest, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return "", "", false
	}
	hdr, data, ok := strings.Cut(rest, ",")
	if !ok || !strings.HasSuffix(hdr, ";base64") {
		return "", "", false
	}
	return strings.TrimSuffix(hdr, ";base64"), data, true
}

// openAIBlocks: o conteúdo de uma mensagem (texto ou lista de partes) como blocos da Anthropic.
// partes: as partes de onde os blocos vieram (nil quando o conteúdo era um texto só).
func openAIBlocks(content any) (blocks []any, parts []map[string]any) {
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
			case "text", "input_text", "output_text":
				b["type"], b["text"] = "text", pm["text"]
			case "image_url":
				im, _ := pm["image_url"].(map[string]any)
				u, _ := im["url"].(string)
				media(u)
			case "file":
				fm, _ := pm["file"].(map[string]any)
				u, _ := fm["file_data"].(string)
				media(u)
			case "input_audio", "audio":
				delete(b, partMark) // áudio: não há como verificar; não sai (falha fechada)
				b["type"], b["text"] = "text", openAIFileNote
			default:
				b["type"], b["parte"] = "parte desconhecida", pm // mascara todo texto dela, por precaução
			}
			blocks = append(blocks, b)
		}
	}
	return blocks, parts
}

// openAIContent: os blocos já mascarados, de volta como o conteúdo de uma mensagem da OpenAI.
// O bloco que veio de uma parte atualiza a própria parte; o bloco novo (a nota do llm-dlp, a
// página de um PDF) vira uma parte de texto nova. Com partes == nil o conteúdo era um texto.
func openAIContent(blocks any, parts []map[string]any) any {
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
				pm = map[string]any{"type": "text"}
			}
			pm["text"] = b["text"]
		case "image", "document":
			if src["type"] != "base64" { // PDF que virou texto mascarado
				t, _ := src["data"].(string)
				if title, _ := b["title"].(string); title != "" {
					t = title + "\n\n" + t
				}
				pm = map[string]any{"type": "text", "text": t}
				break
			}
			u := fmt.Sprintf("data:%v;base64,%v", src["media_type"], src["data"])
			switch {
			case pm == nil:
				pm = map[string]any{"type": "image_url", "image_url": map[string]any{"url": u}}
			case pm["type"] == "file":
				if fm, _ := pm["file"].(map[string]any); fm != nil {
					fm["file_data"] = u
				}
			default:
				if im, _ := pm["image_url"].(map[string]any); im != nil {
					im["url"] = u
				}
			}
		}
		if pm != nil {
			out = append(out, pm)
		}
	}
	return out
}

// openAIBridge devolve a requisição da OpenAI na forma da Anthropic e a função que, depois de
// mascarada, copia o resultado de volta para a requisição original. ok = false: não é uma
// requisição com "messages" (cai no modo genérico, que mascara todo texto).
func openAIBridge(v any) (map[string]any, func(), bool) {
	req, _ := v.(map[string]any)
	list, ok := req["messages"].([]any)
	if !ok {
		return nil, nil, false
	}
	anth := map[string]any{}
	var returns []func()

	if ts, _ := req["tools"].([]any); ts != nil {
		tools := []any{}
		for _, t := range ts {
			tm, _ := t.(map[string]any)
			if fm, _ := tm["function"].(map[string]any); fm != nil {
				tools = append(tools, fm) // o mesmo mapa: descrição e schema são mascarados no lugar
			}
		}
		anth["tools"] = tools
	}

	system := []any{}
	msgs := []any{}
	for _, x := range list {
		mm, _ := x.(map[string]any)
		if mm == nil {
			continue
		}
		hostRole, _ := mm["role"].(string)
		blocks, parts := openAIBlocks(mm["content"])
		hasContent := mm["content"] != nil
		switch hostRole {
		case "system", "developer":
			// instruções do cliente: cada texto é mascarado no lugar
			for _, bx := range blocks {
				b := bx.(map[string]any)
				if b["type"] != "text" {
					continue
				}
				system = append(system, b)
				if parts == nil {
					returns = append(returns, func() { mm["content"] = b["text"] })
				} else if i, ok := b[partMark].(int); ok {
					pm := parts[i]
					returns = append(returns, func() { pm["text"] = b["text"] })
				}
			}
			continue
		case "tool", "function":
			id, _ := mm["tool_call_id"].(string)
			res := map[string]any{"type": "tool_result", "tool_use_id": id, "content": blocks}
			am := map[string]any{"role": "user", "content": []any{res}}
			msgs = append(msgs, am)
			if hasContent {
				returns = append(returns, func() {
					if bs, _ := am["content"].([]any); len(bs) > 0 {
						if r, _ := bs[0].(map[string]any); r != nil {
							mm["content"] = openAIContent(r["content"], parts)
						}
					}
				})
			}
			continue
		}
		am := map[string]any{"role": "user"}
		if hostRole == "assistant" {
			am["role"] = "assistant"
		}
		var calls []map[string]any
		var before [][]byte
		tcs, _ := mm["tool_calls"].([]any)
		for _, t := range tcs {
			tm, _ := t.(map[string]any)
			fm, _ := tm["function"].(map[string]any)
			if fm == nil {
				continue
			}
			var entry any = map[string]any{}
			if args, _ := fm["arguments"].(string); strings.TrimSpace(args) != "" {
				if dec, ok := decode([]byte(args)); ok {
					entry = dec
				} else {
					entry = map[string]any{"\x00cru": args} // não é JSON: mascara o texto como veio
				}
			}
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": tm["id"], "name": fm["name"], "input": entry, callMark: len(calls)})
			calls = append(calls, fm)
			before = append(before, encode(entry))
		}
		am["content"] = blocks
		msgs = append(msgs, am)
		returns = append(returns, func() {
			bs, _ := am["content"].([]any)
			rest := make([]any, 0, len(bs))
			for _, bx := range bs {
				b, _ := bx.(map[string]any)
				i, isCall := b[callMark].(int)
				if !isCall || i >= len(calls) {
					rest = append(rest, bx)
					continue
				}
				// só reescreve os argumentos que mudaram (o texto original fica como o cliente mandou)
				if newOne := encode(b["input"]); !bytes.Equal(newOne, before[i]) {
					if m, _ := b["input"].(map[string]any); m != nil && len(m) == 1 && m["\x00cru"] != nil {
						calls[i]["arguments"] = m["\x00cru"]
					} else {
						calls[i]["arguments"] = string(newOne)
					}
				}
			}
			if hasContent {
				mm["content"] = openAIContent(rest, parts)
			}
		})
	}
	if len(system) > 0 {
		anth["system"] = system
	}
	anth["messages"] = msgs

	for _, k := range keys(req) {
		if !openAIParams[k] { // campo que não conheço: mascara por precaução, como na Anthropic
			k := k
			anth["openai:"+k] = req[k]
			returns = append(returns, func() { req[k] = anth["openai:"+k] })
		}
	}
	return anth, func() {
		for _, f := range returns {
			f()
		}
	}, true
}

// ---- volta --------------------------------------------------------------------------------

// openAIReturn desmascara a resposta em streaming. O texto de cada escolha (choice) passa por um
// Stream, que segura o pseudônimo cortado entre dois pedaços; os argumentos de cada chamada de
// ferramenta chegam em pedaços de JSON e passam por um Stream próprio (o valor real é escapado).
type openAIReturn struct {
	p     *Proxy
	tab   *mask.Table
	texts map[int]*openAIStream
	args  map[[2]int]*openAIStream // (escolha, índice da chamada)
	// cabecalho: os campos de fora do último pedaço (id, model...), para um pedaço a mais no fim
	header map[string]any
}

type openAIStream struct {
	f        *mask.Stream
	orig     strings.Builder
	rawValue bool // ferramenta que vai para a internet: os argumentos seguem com os pseudônimos
}

func (p *Proxy) newOpenAIReturn(tab *mask.Table) *openAIReturn {
	return &openAIReturn{p: p, tab: tab, texts: map[int]*openAIStream{}, args: map[[2]int]*openAIStream{}}
}

func (g *openAIReturn) reg(s string) string { return g.p.m.RecordResponse(s, g.tab) }

// releaseText: o texto que o fluxo da escolha ainda segurava, e registra o que a API escreveu.
func (g *openAIReturn) releaseText(i int) string {
	fx := g.texts[i]
	if fx == nil {
		return ""
	}
	delete(g.texts, i)
	g.reg(fx.orig.String())
	return fx.f.Close()
}

// releaseArgs: o que os fluxos das chamadas da escolha ainda seguravam, como pedaços de tool_calls.
func (g *openAIReturn) releaseArgs(i int) []any {
	var ks [][2]int
	for k := range g.args {
		if k[0] == i {
			ks = append(ks, k)
		}
	}
	sort.Slice(ks, func(a, b int) bool { return ks[a][1] < ks[b][1] })
	var out []any
	for _, k := range ks {
		fx := g.args[k]
		delete(g.args, k)
		if fx.rawValue {
			continue
		}
		recordBlock("tool_use", fx.orig.String(), g.reg)
		if rest := fx.f.Close(); rest != "" {
			out = append(out, map[string]any{"index": k[1], "function": map[string]any{"arguments": rest}})
		}
	}
	return out
}

// pedaco desmascara um pedaço do streaming, no lugar.
func (g *openAIReturn) piece(resp map[string]any) {
	g.header = map[string]any{}
	for _, k := range []string{"id", "object", "created", "model", "system_fingerprint"} {
		if v, ok := resp[k]; ok {
			g.header[k] = v
		}
	}
	choices, _ := resp["choices"].([]any)
	for pos, c := range choices {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		i := intOf(cm["index"])
		if i < 0 {
			i = pos
		}
		d, _ := cm["delta"].(map[string]any)
		if d == nil {
			d = map[string]any{}
		}
		tcs, _ := d["tool_calls"].([]any)
		if t, ok := d["content"].(string); ok {
			fx := g.texts[i]
			if fx == nil {
				fx = &openAIStream{f: g.tab.NewStream(false)}
				g.texts[i] = fx
			}
			fx.orig.WriteString(t)
			d["content"] = fx.f.Push(t)
		}
		end := cm["finish_reason"] != nil
		if len(tcs) > 0 || end { // o texto acabou: entrega o que estava segurado
			if rest := g.releaseText(i); rest != "" {
				t, _ := d["content"].(string)
				d["content"] = t + rest
				cm["delta"] = d
			}
		}
		for _, t := range tcs {
			tm, _ := t.(map[string]any)
			fm, _ := tm["function"].(map[string]any)
			if fm == nil {
				continue
			}
			k := [2]int{i, max(intOf(tm["index"]), 0)}
			fx := g.args[k]
			if fx == nil {
				fx = &openAIStream{f: g.tab.NewStream(true)}
				g.args[k] = fx
			}
			if name, _ := fm["name"].(string); name != "" && g.p.bridgeNoUnmask(name) {
				fx.rawValue = true
			}
			if a, ok := fm["arguments"].(string); ok && !fx.rawValue {
				fx.orig.WriteString(a)
				fm["arguments"] = fx.f.Push(a)
			}
		}
		if end {
			if extra := g.releaseArgs(i); len(extra) > 0 {
				d["tool_calls"] = append(tcs, extra...)
				cm["delta"] = d
			}
		}
	}
}

// resto: o que ficou segurado quando o streaming acaba sem finish_reason, como um pedaço a mais.
func (g *openAIReturn) rest() map[string]any {
	idx := map[int]bool{}
	for i := range g.texts {
		idx[i] = true
	}
	for k := range g.args {
		idx[k[0]] = true
	}
	order := make([]int, 0, len(idx))
	for i := range idx {
		order = append(order, i)
	}
	sort.Ints(order)
	var choices []any
	for _, i := range order {
		d := map[string]any{}
		if s := g.releaseText(i); s != "" {
			d["content"] = s
		}
		if extra := g.releaseArgs(i); len(extra) > 0 {
			d["tool_calls"] = extra
		}
		if len(d) > 0 {
			choices = append(choices, map[string]any{"index": i, "delta": d, "finish_reason": nil})
		}
	}
	if choices == nil {
		return nil
	}
	m := map[string]any{"choices": choices}
	for k, v := range g.header {
		m[k] = v
	}
	return m
}

// unmaskSSEOpenAI: streaming. Cada linha "data:" é um pedaço; a última é "data: [DONE]".
func (p *Proxy) unmaskSSEOpenAI(w http.ResponseWriter, body io.Reader, tab *mask.Table) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	g := p.newOpenAIReturn(tab)
	leftover := func() {
		if m := g.rest(); m != nil {
			fmt.Fprintf(w, "data: %s\n\n", encode(m))
		}
	}
	for {
		line, err := rd.ReadString('\n')
		l := strings.TrimRight(line, "\r\n")
		if data, ok := strings.CutPrefix(l, "data:"); ok && !tab.Empty() {
			if strings.TrimSpace(data) == "[DONE]" {
				leftover()
			} else if v, ok := decode([]byte(data)); ok {
				if m, _ := v.(map[string]any); m != nil {
					g.piece(m)
					line = "data: " + string(encode(m)) + line[len(l):]
				}
			}
		}
		io.WriteString(w, line)
		if err != nil {
			leftover()
			if fl != nil {
				fl.Flush()
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		if l == "" && fl != nil {
			fl.Flush()
		}
	}
}

// unmaskOpenAIJSON: resposta sem streaming. O texto de cada escolha e os argumentos de cada
// chamada de ferramenta voltam reais; o raciocínio (reasoning_content) não muda.
func (p *Proxy) unmaskOpenAIJSON(b []byte, tab *mask.Table) []byte {
	v, ok := decode(b)
	resp, _ := v.(map[string]any)
	if !ok || resp == nil {
		return b
	}
	reg := func(s string) string { return p.m.RecordResponse(s, tab) }
	choices, _ := resp["choices"].([]any)
	for _, c := range choices {
		cm, _ := c.(map[string]any)
		msg, _ := cm["message"].(map[string]any)
		if msg == nil {
			continue
		}
		switch t := msg["content"].(type) {
		case string:
			msg["content"] = reg(t)
		case []any:
			for _, x := range t {
				if pm, _ := x.(map[string]any); pm != nil {
					if s, ok := pm["text"].(string); ok {
						pm["text"] = reg(s)
					}
				}
			}
		}
		tcs, _ := msg["tool_calls"].([]any)
		for _, t := range tcs {
			tm, _ := t.(map[string]any)
			fm, _ := tm["function"].(map[string]any)
			name, _ := fm["name"].(string)
			args, ok := fm["arguments"].(string)
			if fm == nil || !ok || p.bridgeNoUnmask(name) {
				continue
			}
			if dec, ok := decode([]byte(args)); ok {
				fm["arguments"] = string(encode(recordAll(dec, reg)))
			} else {
				fm["arguments"] = tab.Unmask(args, true)
			}
		}
	}
	return encode(resp)
}
