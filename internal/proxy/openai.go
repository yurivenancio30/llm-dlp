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

// Formato da OpenAI (Chat Completions): o que o Copilot (provedor próprio), o opencode e a
// maioria dos clientes mandam para a OpenAI, a xAI (Grok), o OpenRouter e o endereço compatível
// do Gemini. Vale para todo caminho terminado em /chat/completions, de qualquer provedor.
//
// Como no Gemini (gemini.go), a ida não tem um mascarador próprio: a requisição é vista na forma
// da Anthropic (ponteOpenAI) e passa pelo mesmo caminho de /v1/messages. O que muda de nome:
//
//	messages[] com role "system"/"developer"   -> system
//	tools[].function                           -> tools (descrição e schema)
//	messages[].content (texto ou partes)       -> blocos de texto, imagem e PDF
//	messages[].tool_calls[].function           -> tool_use (arguments é um JSON em texto)
//	messages[] com role "tool"                 -> tool_result
//	reasoning_content / reasoning              -> não muda (o modelo só viu pseudônimos)

// parâmetros da OpenAI que não levam dado do usuário (ou que não podem mudar)
var paramsOpenAI = map[string]bool{"messages": true, "tools": true, "model": true, "stream": true, "stream_options": true,
	"tool_choice": true, "parallel_tool_calls": true, "temperature": true, "top_p": true, "n": true, "max_tokens": true,
	"max_completion_tokens": true, "presence_penalty": true, "frequency_penalty": true, "logit_bias": true, "logprobs": true,
	"top_logprobs": true, "seed": true, "stop": true, "reasoning_effort": true, "reasoning": true, "service_tier": true,
	"store": true, "modalities": true, "verbosity": true, "prompt_cache_key": true, "safety_identifier": true}

// marcaChamada: no tool_use criado pela ponte, o índice da chamada (tool_calls) de onde ele veio.
const marcaChamada = "\x00chamada"

const notaArquivoOpenAI = "[llm-dlp] Um arquivo anexado foi retirado daqui e não foi enviado: o llm-dlp só consegue verificar imagem e PDF. Avise o usuário."

// dataURL separa "data:<tipo>;base64,<dados>".
func dataURL(u string) (tipo, dados string, ok bool) {
	resto, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return "", "", false
	}
	cab, dados, ok := strings.Cut(resto, ",")
	if !ok || !strings.HasSuffix(cab, ";base64") {
		return "", "", false
	}
	return strings.TrimSuffix(cab, ";base64"), dados, true
}

// blocosOpenAI: o conteúdo de uma mensagem (texto ou lista de partes) como blocos da Anthropic.
// partes: as partes de onde os blocos vieram (nil quando o conteúdo era um texto só).
func blocosOpenAI(conteudo any) (blocos []any, partes []map[string]any) {
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
			case "text", "input_text", "output_text":
				b["type"], b["text"] = "text", pm["text"]
			case "image_url":
				im, _ := pm["image_url"].(map[string]any)
				u, _ := im["url"].(string)
				midia(u)
			case "file":
				fm, _ := pm["file"].(map[string]any)
				u, _ := fm["file_data"].(string)
				midia(u)
			case "input_audio", "audio":
				delete(b, marcaParte) // áudio: não há como verificar; não sai (falha fechada)
				b["type"], b["text"] = "text", notaArquivoOpenAI
			default:
				b["type"], b["parte"] = "parte desconhecida", pm // mascara todo texto dela, por precaução
			}
			blocos = append(blocos, b)
		}
	}
	return blocos, partes
}

// conteudoOpenAI: os blocos já mascarados, de volta como o conteúdo de uma mensagem da OpenAI.
// O bloco que veio de uma parte atualiza a própria parte; o bloco novo (a nota do llm-dlp, a
// página de um PDF) vira uma parte de texto nova. Com partes == nil o conteúdo era um texto.
func conteudoOpenAI(blocos any, partes []map[string]any) any {
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
				pm = map[string]any{"type": "text"}
			}
			pm["text"] = b["text"]
		case "image", "document":
			if src["type"] != "base64" { // PDF que virou texto mascarado
				t, _ := src["data"].(string)
				if titulo, _ := b["title"].(string); titulo != "" {
					t = titulo + "\n\n" + t
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

// ponteOpenAI devolve a requisição da OpenAI na forma da Anthropic e a função que, depois de
// mascarada, copia o resultado de volta para a requisição original. ok = false: não é uma
// requisição com "messages" (cai no modo genérico, que mascara todo texto).
func ponteOpenAI(v any) (map[string]any, func(), bool) {
	req, _ := v.(map[string]any)
	lista, ok := req["messages"].([]any)
	if !ok {
		return nil, nil, false
	}
	anth := map[string]any{}
	var voltas []func()

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

	sistema := []any{}
	msgs := []any{}
	for _, x := range lista {
		mm, _ := x.(map[string]any)
		if mm == nil {
			continue
		}
		papel, _ := mm["role"].(string)
		blocos, partes := blocosOpenAI(mm["content"])
		temConteudo := mm["content"] != nil
		switch papel {
		case "system", "developer":
			// instruções do cliente: cada texto é mascarado no lugar
			for _, bx := range blocos {
				b := bx.(map[string]any)
				if b["type"] != "text" {
					continue
				}
				sistema = append(sistema, b)
				if partes == nil {
					voltas = append(voltas, func() { mm["content"] = b["text"] })
				} else if i, ok := b[marcaParte].(int); ok {
					pm := partes[i]
					voltas = append(voltas, func() { pm["text"] = b["text"] })
				}
			}
			continue
		case "tool", "function":
			id, _ := mm["tool_call_id"].(string)
			res := map[string]any{"type": "tool_result", "tool_use_id": id, "content": blocos}
			am := map[string]any{"role": "user", "content": []any{res}}
			msgs = append(msgs, am)
			if temConteudo {
				voltas = append(voltas, func() {
					if bs, _ := am["content"].([]any); len(bs) > 0 {
						if r, _ := bs[0].(map[string]any); r != nil {
							mm["content"] = conteudoOpenAI(r["content"], partes)
						}
					}
				})
			}
			continue
		}
		am := map[string]any{"role": "user"}
		if papel == "assistant" {
			am["role"] = "assistant"
		}
		var chamadas []map[string]any
		var antes [][]byte
		tcs, _ := mm["tool_calls"].([]any)
		for _, t := range tcs {
			tm, _ := t.(map[string]any)
			fm, _ := tm["function"].(map[string]any)
			if fm == nil {
				continue
			}
			var entrada any = map[string]any{}
			if args, _ := fm["arguments"].(string); strings.TrimSpace(args) != "" {
				if dec, ok := decodificar([]byte(args)); ok {
					entrada = dec
				} else {
					entrada = map[string]any{"\x00cru": args} // não é JSON: mascara o texto como veio
				}
			}
			blocos = append(blocos, map[string]any{"type": "tool_use", "id": tm["id"], "name": fm["name"], "input": entrada, marcaChamada: len(chamadas)})
			chamadas = append(chamadas, fm)
			antes = append(antes, codificar(entrada))
		}
		am["content"] = blocos
		msgs = append(msgs, am)
		voltas = append(voltas, func() {
			bs, _ := am["content"].([]any)
			resto := make([]any, 0, len(bs))
			for _, bx := range bs {
				b, _ := bx.(map[string]any)
				i, ehChamada := b[marcaChamada].(int)
				if !ehChamada || i >= len(chamadas) {
					resto = append(resto, bx)
					continue
				}
				// só reescreve os argumentos que mudaram (o texto original fica como o cliente mandou)
				if novo := codificar(b["input"]); !bytes.Equal(novo, antes[i]) {
					if m, _ := b["input"].(map[string]any); m != nil && len(m) == 1 && m["\x00cru"] != nil {
						chamadas[i]["arguments"] = m["\x00cru"]
					} else {
						chamadas[i]["arguments"] = string(novo)
					}
				}
			}
			if temConteudo {
				mm["content"] = conteudoOpenAI(resto, partes)
			}
		})
	}
	if len(sistema) > 0 {
		anth["system"] = sistema
	}
	anth["messages"] = msgs

	for _, k := range chaves(req) {
		if !paramsOpenAI[k] { // campo que não conheço: mascara por precaução, como na Anthropic
			k := k
			anth["openai:"+k] = req[k]
			voltas = append(voltas, func() { req[k] = anth["openai:"+k] })
		}
	}
	return anth, func() {
		for _, f := range voltas {
			f()
		}
	}, true
}

// ---- volta --------------------------------------------------------------------------------

// voltaOpenAI desmascara a resposta em streaming. O texto de cada escolha (choice) passa por um
// Fluxo, que segura o pseudônimo cortado entre dois pedaços; os argumentos de cada chamada de
// ferramenta chegam em pedaços de JSON e passam por um Fluxo próprio (o valor real é escapado).
type voltaOpenAI struct {
	p      *Proxy
	tab    *mask.Tabela
	textos map[int]*fluxoOpenAI
	args   map[[2]int]*fluxoOpenAI // (escolha, índice da chamada)
	// cabecalho: os campos de fora do último pedaço (id, model...), para um pedaço a mais no fim
	cabecalho map[string]any
}

type fluxoOpenAI struct {
	f    *mask.Fluxo
	orig strings.Builder
	cru  bool // ferramenta que vai para a internet: os argumentos seguem com os pseudônimos
}

func (p *Proxy) novaVoltaOpenAI(tab *mask.Tabela) *voltaOpenAI {
	return &voltaOpenAI{p: p, tab: tab, textos: map[int]*fluxoOpenAI{}, args: map[[2]int]*fluxoOpenAI{}}
}

func (g *voltaOpenAI) reg(s string) string { return g.p.m.RegistrarResposta(s, g.tab) }

// soltarTexto: o texto que o fluxo da escolha ainda segurava, e registra o que a API escreveu.
func (g *voltaOpenAI) soltarTexto(i int) string {
	fx := g.textos[i]
	if fx == nil {
		return ""
	}
	delete(g.textos, i)
	g.reg(fx.orig.String())
	return fx.f.Fechar()
}

// soltarArgs: o que os fluxos das chamadas da escolha ainda seguravam, como pedaços de tool_calls.
func (g *voltaOpenAI) soltarArgs(i int) []any {
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
		if fx.cru {
			continue
		}
		registrarBloco("tool_use", fx.orig.String(), g.reg)
		if resto := fx.f.Fechar(); resto != "" {
			out = append(out, map[string]any{"index": k[1], "function": map[string]any{"arguments": resto}})
		}
	}
	return out
}

// pedaco desmascara um pedaço do streaming, no lugar.
func (g *voltaOpenAI) pedaco(resp map[string]any) {
	g.cabecalho = map[string]any{}
	for _, k := range []string{"id", "object", "created", "model", "system_fingerprint"} {
		if v, ok := resp[k]; ok {
			g.cabecalho[k] = v
		}
	}
	escolhas, _ := resp["choices"].([]any)
	for pos, c := range escolhas {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		i := intDe(cm["index"])
		if i < 0 {
			i = pos
		}
		d, _ := cm["delta"].(map[string]any)
		if d == nil {
			d = map[string]any{}
		}
		tcs, _ := d["tool_calls"].([]any)
		if t, ok := d["content"].(string); ok {
			fx := g.textos[i]
			if fx == nil {
				fx = &fluxoOpenAI{f: g.tab.NovoFluxo(false)}
				g.textos[i] = fx
			}
			fx.orig.WriteString(t)
			d["content"] = fx.f.Empurrar(t)
		}
		fim := cm["finish_reason"] != nil
		if len(tcs) > 0 || fim { // o texto acabou: entrega o que estava segurado
			if resto := g.soltarTexto(i); resto != "" {
				t, _ := d["content"].(string)
				d["content"] = t + resto
				cm["delta"] = d
			}
		}
		for _, t := range tcs {
			tm, _ := t.(map[string]any)
			fm, _ := tm["function"].(map[string]any)
			if fm == nil {
				continue
			}
			k := [2]int{i, max(intDe(tm["index"]), 0)}
			fx := g.args[k]
			if fx == nil {
				fx = &fluxoOpenAI{f: g.tab.NovoFluxo(true)}
				g.args[k] = fx
			}
			if nome, _ := fm["name"].(string); nome != "" && g.p.semDesmascararPonte(nome) {
				fx.cru = true
			}
			if a, ok := fm["arguments"].(string); ok && !fx.cru {
				fx.orig.WriteString(a)
				fm["arguments"] = fx.f.Empurrar(a)
			}
		}
		if fim {
			if extra := g.soltarArgs(i); len(extra) > 0 {
				d["tool_calls"] = append(tcs, extra...)
				cm["delta"] = d
			}
		}
	}
}

// resto: o que ficou segurado quando o streaming acaba sem finish_reason, como um pedaço a mais.
func (g *voltaOpenAI) resto() map[string]any {
	idx := map[int]bool{}
	for i := range g.textos {
		idx[i] = true
	}
	for k := range g.args {
		idx[k[0]] = true
	}
	ordem := make([]int, 0, len(idx))
	for i := range idx {
		ordem = append(ordem, i)
	}
	sort.Ints(ordem)
	var escolhas []any
	for _, i := range ordem {
		d := map[string]any{}
		if s := g.soltarTexto(i); s != "" {
			d["content"] = s
		}
		if extra := g.soltarArgs(i); len(extra) > 0 {
			d["tool_calls"] = extra
		}
		if len(d) > 0 {
			escolhas = append(escolhas, map[string]any{"index": i, "delta": d, "finish_reason": nil})
		}
	}
	if escolhas == nil {
		return nil
	}
	m := map[string]any{"choices": escolhas}
	for k, v := range g.cabecalho {
		m[k] = v
	}
	return m
}

// desmascararSSEOpenAI: streaming. Cada linha "data:" é um pedaço; a última é "data: [DONE]".
func (p *Proxy) desmascararSSEOpenAI(w http.ResponseWriter, body io.Reader, tab *mask.Tabela) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	g := p.novaVoltaOpenAI(tab)
	sobra := func() {
		if m := g.resto(); m != nil {
			fmt.Fprintf(w, "data: %s\n\n", codificar(m))
		}
	}
	for {
		linha, err := rd.ReadString('\n')
		l := strings.TrimRight(linha, "\r\n")
		if dados, ok := strings.CutPrefix(l, "data:"); ok && !tab.Vazia() {
			if strings.TrimSpace(dados) == "[DONE]" {
				sobra()
			} else if v, ok := decodificar([]byte(dados)); ok {
				if m, _ := v.(map[string]any); m != nil {
					g.pedaco(m)
					linha = "data: " + string(codificar(m)) + linha[len(l):]
				}
			}
		}
		io.WriteString(w, linha)
		if err != nil {
			sobra()
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

// desmascararOpenAIJSON: resposta sem streaming. O texto de cada escolha e os argumentos de cada
// chamada de ferramenta voltam reais; o raciocínio (reasoning_content) não muda.
func (p *Proxy) desmascararOpenAIJSON(b []byte, tab *mask.Tabela) []byte {
	v, ok := decodificar(b)
	resp, _ := v.(map[string]any)
	if !ok || resp == nil {
		return b
	}
	reg := func(s string) string { return p.m.RegistrarResposta(s, tab) }
	escolhas, _ := resp["choices"].([]any)
	for _, c := range escolhas {
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
			nome, _ := fm["name"].(string)
			args, ok := fm["arguments"].(string)
			if fm == nil || !ok || p.semDesmascararPonte(nome) {
				continue
			}
			if dec, ok := decodificar([]byte(args)); ok {
				fm["arguments"] = string(codificar(registrarTudo(dec, reg)))
			} else {
				fm["arguments"] = tab.Desmascarar(args, true)
			}
		}
	}
	return codificar(resp)
}
