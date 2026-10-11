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

// Formato Responses da OpenAI (/v1/responses): o do Codex CLI, com a chave de API da OpenAI e com
// a assinatura do ChatGPT (chatgpt.com/backend-api/codex/responses). Vale para todo caminho
// terminado em /responses, de qualquer provedor.
//
// Como no Gemini e no Chat Completions, a ida é vista na forma da Anthropic (ponteResponses) e
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
var paramsResponses = map[string]bool{"model": true, "input": true, "instructions": true, "tools": true,
	"tool_choice": true, "parallel_tool_calls": true, "reasoning": true, "store": true, "stream": true,
	"include": true, "prompt_cache_key": true, "text": true, "service_tier": true, "max_output_tokens": true,
	"temperature": true, "top_p": true, "truncation": true, "previous_response_id": true, "background": true,
	"top_logprobs": true, "safety_identifier": true, "prompt_cache_retention": true}

// itens que já estão do lado da API, ou que o modelo escreveu a partir de pseudônimos: não mudam
var itensIntactosResponses = map[string]bool{"reasoning": true, "web_search_call": true, "image_generation_call": true,
	"compaction": true, "compaction_summary": true, "context_compaction": true, "compaction_trigger": true,
	"item_reference": true, "configuration_update": true, "tool_search_call": true, "tool_search_output": true,
	"file_search_call": true, "code_interpreter_call": true, "mcp_list_tools": true}

// blocosResponses: o conteúdo de uma mensagem (texto ou lista de partes) como blocos da Anthropic.
func blocosResponses(conteudo any) (blocos []any, partes []map[string]any) {
	blocos = []any{}
	switch c := conteudo.(type) {
	case string:
		return []any{map[string]any{"type": "text", "text": c}}, nil
	case []any:
		partes = []map[string]any{}
		for _, x := range c {
			pm, _ := x.(map[string]any)
			if pm == nil {
				continue
			}
			b := map[string]any{marcaParte: len(partes)}
			partes = append(partes, pm)
			midia := func(u string) {
				tipo, dados, ok := dataURL(u)
				switch {
				case !ok:
					b["type"] = "server_tool_use" // endereço ou arquivo já do lado da API: não muda
					return
				case tipo == "application/pdf":
					b["type"] = "document"
				case strings.HasPrefix(tipo, "image/"):
					b["type"] = "image"
				default:
					delete(b, marcaParte)
					b["type"], b["text"] = "text", notaArquivoOpenAI
					return
				}
				b["source"] = map[string]any{"type": "base64", "media_type": tipo, "data": dados}
			}
			switch pm["type"] {
			case "input_text", "output_text", "text", "summary_text":
				b["type"], b["text"] = "text", pm["text"]
			case "input_image":
				u, _ := pm["image_url"].(string)
				midia(u)
			case "input_file":
				u, _ := pm["file_data"].(string)
				midia(u)
			case "input_audio":
				delete(b, marcaParte) // áudio: não há como verificar; não sai (falha fechada)
				b["type"], b["text"] = "text", notaArquivoOpenAI
			case "refusal":
				b["type"] = "server_tool_use" // recusa do próprio modelo: não muda
			default:
				b["type"], b["parte"] = "parte desconhecida", pm // mascara todo texto dela, por precaução
			}
			blocos = append(blocos, b)
		}
	}
	return blocos, partes
}

// conteudoResponses: os blocos já mascarados, de volta como o conteúdo de uma mensagem do
// Responses. Com partes == nil o conteúdo era um texto.
func conteudoResponses(blocos any, partes []map[string]any) any {
	bs, _ := blocos.([]any)
	if partes == nil {
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
		if i, ok := b[marcaParte].(int); ok && i < len(partes) {
			pm = partes[i]
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
				if titulo, _ := b["title"].(string); titulo != "" {
					t = titulo + "\n\n" + t
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

// ponteResponses devolve a requisição do Responses na forma da Anthropic e a função que, depois de
// mascarada, copia o resultado de volta. ok = false: não é uma requisição com "input" (cai no modo
// genérico, que mascara todo texto).
func ponteResponses(v any) (map[string]any, func(), bool) {
	req, _ := v.(map[string]any)
	entrada, ok := req["input"]
	if !ok {
		return nil, nil, false
	}
	anth := map[string]any{}
	var voltas []func()

	if ts, _ := req["tools"].([]any); ts != nil {
		tools := []any{}
		for _, t := range ts {
			if tm, ok := t.(map[string]any); ok {
				tools = append(tools, tm) // o mesmo mapa: descrição e schema são mascarados no lugar
			}
		}
		anth["tools"] = tools
	}

	sistema := []any{}
	if s, ok := req["instructions"].(string); ok {
		b := map[string]any{"type": "text", "text": s}
		sistema = append(sistema, b)
		voltas = append(voltas, func() { req["instructions"] = b["text"] })
	}

	var itens []any
	switch x := entrada.(type) {
	case string:
		m := map[string]any{"type": "message", "role": "user", "content": x}
		itens = []any{m}
		voltas = append(voltas, func() { req["input"] = m["content"] })
	case []any:
		itens = x
	}

	msgs := []any{}
	for _, x := range itens {
		it, _ := x.(map[string]any)
		if it == nil {
			continue
		}
		tipo, _ := it["type"].(string)
		if tipo == "" && it["role"] != nil {
			tipo = "message"
		}
		switch {
		case itensIntactosResponses[tipo]:
			continue
		case tipo == "message":
			papel, _ := it["role"].(string)
			blocos, partes := blocosResponses(it["content"])
			if papel == "system" || papel == "developer" {
				// instruções do cliente: cada texto é mascarado no lugar
				for _, bx := range blocos {
					b, _ := bx.(map[string]any)
					if b == nil || b["type"] != "text" {
						continue
					}
					sistema = append(sistema, b)
					if partes == nil {
						voltas = append(voltas, func() { it["content"] = b["text"] })
					} else if i, ok := b[marcaParte].(int); ok {
						pm := partes[i]
						voltas = append(voltas, func() { pm["text"] = b["text"] })
					}
				}
				continue
			}
			am := map[string]any{"role": "user", "content": blocos}
			if papel == "assistant" {
				am["role"] = "assistant"
			}
			msgs = append(msgs, am)
			voltas = append(voltas, func() { it["content"] = conteudoResponses(am["content"], partes) })
		case tipo == "function_call":
			args, _ := it["arguments"].(string)
			var entrada any = map[string]any{}
			if strings.TrimSpace(args) != "" {
				if dec, ok := decodificar([]byte(args)); ok {
					entrada = dec
				} else {
					entrada = map[string]any{"\x00cru": args} // não é JSON: mascara o texto como veio
				}
			}
			antes := codificar(entrada)
			b := map[string]any{"type": "tool_use", "id": it["call_id"], "name": it["name"], "input": entrada}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{b}})
			voltas = append(voltas, func() {
				// só reescreve os argumentos que mudaram (o texto original fica como o cliente mandou)
				if novo := codificar(b["input"]); string(novo) != string(antes) {
					if m, _ := b["input"].(map[string]any); m != nil && len(m) == 1 && m["\x00cru"] != nil {
						it["arguments"] = m["\x00cru"]
					} else {
						it["arguments"] = string(novo)
					}
				}
			})
		case tipo == "custom_tool_call":
			b := map[string]any{"type": "tool_use", "id": it["call_id"], "name": it["name"], "input": map[string]any{"input": it["input"]}}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{b}})
			voltas = append(voltas, func() {
				if m, _ := b["input"].(map[string]any); m != nil {
					it["input"] = m["input"]
				}
			})
		case tipo == "local_shell_call":
			b := map[string]any{"type": "tool_use", "id": it["call_id"], "name": "local_shell", "input": it["action"]}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": []any{b}})
			voltas = append(voltas, func() { it["action"] = b["input"] })
		case tipo == "function_call_output" || tipo == "custom_tool_call_output" || tipo == "local_shell_call_output":
			blocos, partes := blocosResponses(it["output"])
			res := map[string]any{"type": "tool_result", "tool_use_id": it["call_id"], "content": blocos}
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{res}})
			if it["output"] != nil {
				voltas = append(voltas, func() { it["output"] = conteudoResponses(res["content"], partes) })
			}
		default:
			// item que não conheço: mascara todo texto dele, por precaução
			b := map[string]any{"type": "parte desconhecida", "parte": it}
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{b}})
		}
	}
	if len(sistema) > 0 {
		anth["system"] = sistema
	}
	anth["messages"] = msgs

	for _, k := range chaves(req) {
		if !paramsResponses[k] { // campo que não conheço: mascara por precaução, como na Anthropic
			k := k
			anth["responses:"+k] = req[k]
			voltas = append(voltas, func() { req[k] = anth["responses:"+k] })
		}
	}
	return anth, func() {
		for _, f := range voltas {
			f()
		}
	}, true
}

// ---- volta --------------------------------------------------------------------------------

// voltaResponses desmascara a resposta do Responses. Em streaming, o texto, os argumentos de uma
// chamada (pedaços de um JSON) e a entrada de uma ferramenta custom (o patch do apply_patch)
// chegam em pedaços e passam cada um por um Fluxo, que segura o pseudônimo cortado entre dois
// pedaços. Os eventos que trazem o item inteiro (*.done, output_item.done, response.completed)
// são desmascarados inteiros: o cliente pode usar tanto os pedaços quanto o item.
type voltaResponses struct {
	p      *Proxy
	tab    *mask.Tabela
	fluxos map[string]*fluxoResponses // por item (e parte, no texto)
}

type fluxoResponses struct {
	f        *mask.Fluxo
	evento   string         // o tipo do evento de pedaço, para o pedaço a mais no fim
	campos   map[string]any // item_id, output_index, content_index
	campo    string         // "delta"
	cru      bool           // ferramenta que vai para a internet: segue com os pseudônimos
	ehTexto  bool
	ehJSON   bool
	original strings.Builder
}

func (p *Proxy) novaVoltaResponses(tab *mask.Tabela) *voltaResponses {
	return &voltaResponses{p: p, tab: tab, fluxos: map[string]*fluxoResponses{}}
}

func (g *voltaResponses) reg(s string) string { return g.p.m.RegistrarResposta(s, g.tab) }

// cru: o item é de uma ferramenta que vai para a internet (segue com os pseudônimos).
func (g *voltaResponses) cru(k string) bool { fx := g.fluxos[k]; return fx != nil && fx.cru }

func chaveItem(ev map[string]any) string {
	id, _ := ev["item_id"].(string)
	if id == "" {
		id = fmt.Sprint("i", intDe(ev["output_index"]))
	}
	return id
}

// desmascararArgs: os argumentos (um JSON em texto) com os valores reais, no mesmo formato em que
// vieram; cada string é registrada (quem escreveu).
func (g *voltaResponses) desmascararArgs(args string) string {
	if dec, ok := decodificar([]byte(args)); ok {
		registrarTudo(dec, g.reg)
	}
	return g.tab.Desmascarar(args, true)
}

// item: desmascara um item inteiro da saída, no lugar.
func (g *voltaResponses) item(it map[string]any) {
	nome, _ := it["name"].(string)
	switch it["type"] {
	case "message":
		partes, _ := it["content"].([]any)
		for _, x := range partes {
			if pm, _ := x.(map[string]any); pm != nil {
				if t, ok := pm["text"].(string); ok && pm["type"] != "refusal" {
					pm["text"] = g.reg(t)
				}
			}
		}
	case "function_call":
		if a, ok := it["arguments"].(string); ok && !g.p.semDesmascararPonte(nome) {
			it["arguments"] = g.desmascararArgs(a)
		}
	case "custom_tool_call":
		if s, ok := it["input"].(string); ok && !g.p.semDesmascararPonte(nome) {
			it["input"] = g.reg(s)
		}
	case "local_shell_call":
		it["action"] = registrarTudo(it["action"], g.reg)
	}
}

// soltar: o que os fluxos de um item ainda seguravam, como eventos de pedaço a mais.
func (g *voltaResponses) soltar(prefixo string) []map[string]any {
	var ks []string
	for k := range g.fluxos {
		if k == prefixo || strings.HasPrefix(k, prefixo+"|") || prefixo == "" {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	var out []map[string]any
	for _, k := range ks {
		fx := g.fluxos[k]
		delete(g.fluxos, k)
		if fx.cru {
			continue
		}
		if resto := fx.f.Fechar(); resto != "" {
			ev := map[string]any{"type": fx.evento, fx.campo: resto}
			for c, v := range fx.campos {
				ev[c] = v
			}
			out = append(out, ev)
		}
	}
	return out
}

// evento desmascara um evento do streaming, no lugar. Devolve os eventos de pedaço a mais que
// têm de sair antes dele (o resto que um fluxo segurava quando o item acabou).
func (g *voltaResponses) evento(ev map[string]any) []map[string]any {
	tipo, _ := ev["type"].(string)
	pedaco := func(ehJSON bool, k string) {
		fx := g.fluxos[k]
		if fx == nil {
			fx = &fluxoResponses{f: g.tab.NovoFluxo(ehJSON), evento: tipo, campo: "delta", ehJSON: ehJSON, campos: map[string]any{}}
			for _, c := range []string{"item_id", "output_index", "content_index"} {
				if v, ok := ev[c]; ok {
					fx.campos[c] = v
				}
			}
			g.fluxos[k] = fx
		}
		s, ok := ev["delta"].(string)
		if !ok || fx.cru {
			return
		}
		fx.original.WriteString(s)
		ev["delta"] = fx.f.Empurrar(s)
	}
	switch tipo {
	case "response.output_text.delta":
		pedaco(false, chaveItem(ev)+"|"+fmt.Sprint(intDe(ev["content_index"])))
	case "response.function_call_arguments.delta":
		pedaco(true, chaveItem(ev))
	case "response.custom_tool_call_input.delta":
		pedaco(false, chaveItem(ev))
	case "response.output_text.done":
		extra := g.soltar(chaveItem(ev) + "|" + fmt.Sprint(intDe(ev["content_index"])))
		if t, ok := ev["text"].(string); ok {
			ev["text"] = g.reg(t)
		}
		return extra
	case "response.function_call_arguments.done":
		cru := g.cru(chaveItem(ev))
		extra := g.soltar(chaveItem(ev))
		if a, ok := ev["arguments"].(string); ok && !cru {
			ev["arguments"] = g.desmascararArgs(a)
		}
		return extra
	case "response.custom_tool_call_input.done":
		cru := g.cru(chaveItem(ev))
		extra := g.soltar(chaveItem(ev))
		if s, ok := ev["input"].(string); ok && !cru {
			ev["input"] = g.reg(s)
		}
		return extra
	case "response.content_part.added", "response.content_part.done":
		if pm, _ := ev["part"].(map[string]any); pm != nil {
			if t, ok := pm["text"].(string); ok && pm["type"] != "refusal" {
				pm["text"] = g.tab.Desmascarar(t, false)
			}
		}
	case "response.output_item.added", "response.output_item.done":
		it, _ := ev["item"].(map[string]any)
		var extra []map[string]any
		if tipo == "response.output_item.done" {
			id, _ := it["id"].(string)
			if id == "" {
				id = fmt.Sprint("i", intDe(ev["output_index"]))
			}
			extra = g.soltar(id)
		}
		if it != nil {
			nome, _ := it["name"].(string)
			if tipo == "response.output_item.added" && g.p.semDesmascararPonte(nome) {
				// os pedaços dos argumentos de uma ferramenta de internet seguem com os pseudônimos
				id, _ := it["id"].(string)
				g.fluxos[id] = &fluxoResponses{f: g.tab.NovoFluxo(true), cru: true}
			}
			g.item(it)
		}
		return extra
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		r, _ := ev["response"].(map[string]any)
		saida, _ := r["output"].([]any)
		for _, x := range saida {
			if it, _ := x.(map[string]any); it != nil {
				g.item(it)
			}
		}
		return g.soltar("")
	}
	return nil
}

// desmascararSSEResponses: streaming ("event: ..." e "data: {...}", um evento por bloco).
func (p *Proxy) desmascararSSEResponses(w http.ResponseWriter, body io.Reader, tab *mask.Tabela) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	g := p.novaVoltaResponses(tab)
	escrever := func(ev map[string]any) {
		if t, _ := ev["type"].(string); t != "" {
			fmt.Fprintf(w, "event: %s\n", t)
		}
		fmt.Fprintf(w, "data: %s\n\n", codificar(ev))
	}
	var linhas []string
	processar := func() {
		if len(linhas) == 0 {
			return
		}
		defer func() { linhas = linhas[:0] }()
		dados := ""
		for _, l := range linhas {
			if d, ok := strings.CutPrefix(l, "data:"); ok {
				dados = strings.TrimPrefix(d, " ")
			}
		}
		var ev map[string]any
		if v, ok := decodificar([]byte(dados)); ok && !tab.Vazia() {
			ev, _ = v.(map[string]any)
		}
		if ev == nil { // sem nada para desmascarar: passa como veio
			io.WriteString(w, strings.Join(linhas, "\n")+"\n\n")
			return
		}
		for _, extra := range g.evento(ev) {
			escrever(extra)
		}
		// o nome do evento continua o que veio (o cliente lê o "type" de dentro dos dados)
		for _, l := range linhas {
			if !strings.HasPrefix(l, "data:") {
				io.WriteString(w, l+"\n")
			}
		}
		fmt.Fprintf(w, "data: %s\n\n", codificar(ev))
	}
	for {
		linha, err := rd.ReadString('\n')
		l := strings.TrimRight(linha, "\r\n")
		if l == "" && linha != "" {
			processar()
			if fl != nil {
				fl.Flush()
			}
		} else if l != "" {
			linhas = append(linhas, l)
		}
		if err != nil {
			processar()
			for _, extra := range g.soltar("") {
				escrever(extra)
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

// desmascararResponsesJSON: resposta sem streaming. O texto de cada mensagem e a entrada de cada
// chamada de ferramenta voltam reais; o raciocínio não muda.
func (p *Proxy) desmascararResponsesJSON(b []byte, tab *mask.Tabela) []byte {
	v, ok := decodificar(b)
	resp, _ := v.(map[string]any)
	if !ok || resp == nil {
		return b
	}
	g := p.novaVoltaResponses(tab)
	saida, _ := resp["output"].([]any)
	for _, x := range saida {
		if it, _ := x.(map[string]any); it != nil {
			g.item(it)
		}
	}
	return codificar(resp)
}
