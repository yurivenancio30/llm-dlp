package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Formato da API do Gemini (generateContent, streamGenerateContent, countTokens): o que o Gemini
// CLI, o opencode e as SDKs do Google mandam.
//
// A ida não tem um mascarador próprio. A requisição é vista na forma da Anthropic (ponteGemini) e
// passa pelo mesmo caminho de /v1/messages: a memória da conversa, o rastreamento, as dicas dos
// comandos, o congelamento do que já saiu e o tratamento de imagens valem igual. O que muda de
// nome:
//
//	systemInstruction.parts[].text          -> system
//	tools[].functionDeclarations[]          -> tools (descrição e schema)
//	contents[] com role "model"             -> mensagem do assistente
//	parts[].text                            -> bloco de texto
//	parts[].text com "thought": true        -> raciocínio (não muda, como o thinking)
//	parts[].functionCall {name, args}       -> tool_use
//	parts[].functionResponse {name, response} -> tool_result (cada texto de response)
//	parts[].inlineData {mimeType, data}     -> imagem ou PDF (OCR)
//
// Os textos são mascarados no lugar: os mapas da requisição original são os mesmos que o
// mascarador percorre, e o que ele devolve em bloco novo é copiado de volta (voltar).

// marcaParte: no bloco criado pela ponte, o índice da parte de onde ele veio.
const marcaParte = "\x00parte"

// parâmetros do Gemini que não levam dado do usuário (ou que não podem mudar)
var paramsGemini = map[string]bool{"contents": true, "tools": true, "systemInstruction": true, "system_instruction": true,
	"generationConfig": true, "generation_config": true, "safetySettings": true, "safety_settings": true,
	"toolConfig": true, "tool_config": true, "cachedContent": true, "cached_content": true, "model": true,
	"labels": true, "serviceTier": true, "service_tier": true, "store": true}

func campo(m map[string]any, nomes ...string) (any, string) {
	for _, n := range nomes {
		if v, ok := m[n]; ok {
			return v, n
		}
	}
	return nil, nomes[0]
}

// idsGemini casa cada functionResponse com a sua functionCall. O Gemini mais novo manda um id;
// sem ele, vale a ordem: a n-ésima resposta de um nome é da n-ésima chamada desse nome.
type idsGemini struct{ chamadas, respostas map[string]int }

func (g *idsGemini) de(m map[string]any, cont map[string]int) string {
	if id, _ := m["id"].(string); id != "" {
		return id
	}
	nome, _ := m["name"].(string)
	cont[nome]++
	return fmt.Sprintf("g:%s:%d", nome, cont[nome])
}

// folhas chama f para cada string de v (objetos em ordem de chave), com como gravá-la de volta.
func folhas(v any, gravar func(any), f func(s string, gravar func(any))) {
	switch x := v.(type) {
	case string:
		f(x, gravar)
	case []any:
		for i := range x {
			i := i
			folhas(x[i], func(n any) { x[i] = n }, f)
		}
	case map[string]any:
		for _, k := range chaves(x) {
			k := k
			folhas(x[k], func(n any) { x[k] = n }, f)
		}
	}
}

// ponteGemini devolve a requisição do Gemini na forma da Anthropic e a função que, depois de
// mascarada, copia o resultado de volta para a requisição original. ok = false: não é uma
// requisição com "contents" (cai no modo genérico, que mascara todo texto).
func ponteGemini(v any) (map[string]any, func(), bool) {
	req, _ := v.(map[string]any)
	if req == nil {
		return nil, nil, false
	}
	if in, ok := req["generateContentRequest"].(map[string]any); ok { // countTokens
		req = in
	}
	conts, ok := req["contents"].([]any)
	if !ok {
		return nil, nil, false
	}
	anth := map[string]any{}
	var voltas []func()

	if ts, _ := req["tools"].([]any); ts != nil {
		tools := []any{}
		for _, t := range ts {
			tm, _ := t.(map[string]any)
			fds, _ := campo(tm, "functionDeclarations", "function_declarations")
			lista, _ := fds.([]any)
			for _, fd := range lista {
				if m, ok := fd.(map[string]any); ok {
					tools = append(tools, m) // o mesmo mapa: descrição e schema são mascarados no lugar
				}
			}
		}
		anth["tools"] = tools
	}

	if si, _ := campo(req, "systemInstruction", "system_instruction"); si != nil {
		sm, _ := si.(map[string]any)
		ps, _ := sm["parts"].([]any)
		blocos := []any{}
		for _, pt := range ps {
			pm, _ := pt.(map[string]any)
			if t, ok := pm["text"].(string); ok {
				b := map[string]any{"type": "text", "text": t}
				blocos = append(blocos, b)
				voltas = append(voltas, func() { pm["text"] = b["text"] })
			}
		}
		anth["system"] = blocos
	}

	ids := &idsGemini{map[string]int{}, map[string]int{}}
	msgs := []any{}
	for _, c := range conts {
		cm, _ := c.(map[string]any)
		ps, _ := cm["parts"].([]any)
		if cm == nil {
			continue
		}
		papel := "user"
		if cm["role"] == "model" {
			papel = "assistant"
		}
		var partes []map[string]any
		blocos := []any{}
		for _, pt := range ps {
			pm, _ := pt.(map[string]any)
			if pm == nil {
				continue
			}
			b := map[string]any{marcaParte: len(partes)}
			partes = append(partes, pm)
			fc, _ := campo(pm, "functionCall", "function_call")
			fr, _ := campo(pm, "functionResponse", "function_response")
			in, _ := campo(pm, "inlineData", "inline_data")
			texto, temTexto := pm["text"].(string)
			switch {
			case pm["thought"] == true:
				// resumo do raciocínio: o modelo só viu pseudônimos e a volta não o desmascara
				b["type"], b["thinking"] = "thinking", pm["text"]
			case fc != nil:
				fm, _ := fc.(map[string]any)
				b["type"], b["id"], b["name"], b["input"] = "tool_use", ids.de(fm, ids.chamadas), fm["name"], fm["args"]
			case fr != nil:
				fm, _ := fr.(map[string]any)
				cont := []any{}
				folhas(fm["response"], func(n any) { fm["response"] = n }, func(s string, gravar func(any)) {
					t := map[string]any{"type": "text", "text": s}
					cont = append(cont, t)
					voltas = append(voltas, func() { gravar(t["text"]) })
				})
				b["type"], b["tool_use_id"], b["content"] = "tool_result", ids.de(fm, ids.respostas), cont
			case in != nil:
				im, _ := in.(map[string]any)
				mt, _ := campo(im, "mimeType", "mime_type")
				tipo, _ := mt.(string)
				switch {
				case tipo == "application/pdf":
					b["type"] = "document"
				case strings.HasPrefix(tipo, "image/"):
					b["type"] = "image"
				default:
					// áudio, vídeo e outros: não há como verificar; não sai (falha fechada)
					delete(b, marcaParte)
					b["type"], b["text"] = "text", "[llm-dlp] Um arquivo anexado foi retirado daqui e não foi enviado: o llm-dlp só consegue verificar imagem e PDF. Avise o usuário."
				}
				if b["type"] != "text" {
					b["source"] = map[string]any{"type": "base64", "media_type": tipo, "data": im["data"]}
				}
			case temTexto:
				b["type"], b["text"] = "text", texto
			case pm["fileData"] != nil || pm["file_data"] != nil || pm["executableCode"] != nil || pm["codeExecutionResult"] != nil:
				b["type"] = "server_tool_use" // já está do lado da API, ou foi ela que gerou: não muda
			default:
				b["type"], b["parte"] = "parte desconhecida", pm // mascara todo texto dela, por precaução
			}
			blocos = append(blocos, b)
		}
		am := map[string]any{"role": papel, "content": blocos}
		msgs = append(msgs, am)
		voltas = append(voltas, func() { cm["parts"] = partesDe(am["content"], partes) })
	}
	anth["messages"] = msgs

	for _, k := range chaves(req) {
		if !paramsGemini[k] { // campo que não conheço: mascara por precaução, como na Anthropic
			k := k
			anth["gemini:"+k] = req[k]
			voltas = append(voltas, func() { req[k] = anth["gemini:"+k] })
		}
	}
	return anth, func() {
		for _, f := range voltas {
			f()
		}
	}, true
}

// partesDe: os blocos de uma mensagem já mascarada, de volta como partes do Gemini. O bloco que
// veio de uma parte atualiza a própria parte (e mantém o que mais ela tinha, como a
// thoughtSignature); o bloco novo (a nota do llm-dlp, a página de um PDF) vira uma parte nova.
func partesDe(conteudo any, partes []map[string]any) []any {
	blocos, _ := conteudo.([]any)
	out := make([]any, 0, len(blocos))
	for _, x := range blocos {
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
				pm = map[string]any{}
			}
			pm["text"] = b["text"]
		case "tool_use":
			if fc, _ := campo(pm, "functionCall", "function_call"); fc != nil && b["input"] != nil {
				fc.(map[string]any)["args"] = b["input"]
			}
		case "image", "document":
			if src["type"] != "base64" { // PDF que virou texto mascarado
				t, _ := src["data"].(string)
				if titulo, _ := b["title"].(string); titulo != "" {
					t = titulo + "\n\n" + t
				}
				pm = map[string]any{"text": t}
				break
			}
			var im map[string]any
			if pm != nil {
				v, _ := campo(pm, "inlineData", "inline_data")
				im, _ = v.(map[string]any)
			}
			if im == nil {
				im = map[string]any{}
				pm = map[string]any{"inlineData": im}
			}
			_, chave := campo(im, "mimeType", "mime_type")
			im[chave], im["data"] = src["media_type"], src["data"]
		}
		if pm != nil {
			out = append(out, pm)
		}
	}
	return out
}

// ---- volta --------------------------------------------------------------------------------

// voltaGemini desmascara a resposta do Gemini, inteira ou em pedaços (cada pedaço do streaming é
// uma resposta parcial). O texto passa por um Fluxo por candidato, que segura o pseudônimo
// cortado entre dois pedaços; os argumentos de uma chamada de ferramenta chegam inteiros.
type voltaGemini struct {
	p      *Proxy
	tab    *mask.Tabela
	fluxos map[int]*mask.Fluxo
	orig   map[int]*strings.Builder // o texto como a API mandou (quem escreveu)
}

func (p *Proxy) novaVoltaGemini(tab *mask.Tabela) *voltaGemini {
	return &voltaGemini{p: p, tab: tab, fluxos: map[int]*mask.Fluxo{}, orig: map[int]*strings.Builder{}}
}

// soltar: o texto que o fluxo do candidato ainda segurava, e registra o que a API escreveu.
func (g *voltaGemini) soltar(i int) string {
	resto := ""
	if f := g.fluxos[i]; f != nil {
		resto = f.Fechar()
		delete(g.fluxos, i)
	}
	if o := g.orig[i]; o != nil {
		g.p.m.RegistrarResposta(o.String(), g.tab)
		delete(g.orig, i)
	}
	return resto
}

// pedaco desmascara uma resposta (ou um pedaço dela), no lugar. fim = não vem mais nada.
func (g *voltaGemini) pedaco(resp map[string]any, fim bool) {
	cands, _ := resp["candidates"].([]any)
	for pos, c := range cands {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		i := intDe(cm["index"])
		if i < 0 {
			i = pos
		}
		cont, _ := cm["content"].(map[string]any)
		ps, _ := cont["parts"].([]any)
		novas := make([]any, 0, len(ps)+1)
		texto := func(s string) { // acrescenta texto já desmascarado ao último bloco de texto
			if s == "" {
				return
			}
			if n := len(novas); n > 0 {
				if u, _ := novas[n-1].(map[string]any); u != nil && u["thought"] != true {
					if t, ok := u["text"].(string); ok {
						u["text"] = t + s
						return
					}
				}
			}
			novas = append(novas, map[string]any{"text": s})
		}
		for _, pt := range ps {
			pm, _ := pt.(map[string]any)
			t, temTexto := pm["text"].(string)
			fc, _ := campo(pm, "functionCall", "function_call")
			switch {
			case pm == nil:
				novas = append(novas, pt)
			case temTexto && pm["thought"] != true:
				if g.fluxos[i] == nil {
					g.fluxos[i], g.orig[i] = g.tab.NovoFluxo(false), &strings.Builder{}
				}
				g.orig[i].WriteString(t)
				out := g.fluxos[i].Empurrar(t)
				if len(pm) == 1 { // só o texto: junta ao anterior, ou some se o fluxo segurou tudo
					texto(out)
				} else {
					pm["text"] = out
					novas = append(novas, pm)
				}
			default:
				// outra coisa depois de texto: o bloco de texto acabou
				texto(g.soltar(i))
				if fm, _ := fc.(map[string]any); fm != nil {
					if nome, _ := fm["name"].(string); !g.p.semDesmascararPonte(nome) {
						fm["args"] = registrarTudo(fm["args"], func(s string) string { return g.p.m.RegistrarResposta(s, g.tab) })
					}
				}
				novas = append(novas, pm)
			}
		}
		if fr, _ := cm["finishReason"].(string); fr != "" || fim {
			texto(g.soltar(i))
		}
		if cont != nil {
			cont["parts"] = novas
		} else if len(novas) > 0 {
			cm["content"] = map[string]any{"role": "model", "parts": novas}
		}
	}
}

// resto: o que ficou segurado quando o streaming acaba sem aviso, como um pedaço a mais.
func (g *voltaGemini) resto() map[string]any {
	var cands []any
	idx := make([]int, 0, len(g.fluxos))
	for i := range g.fluxos {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		if s := g.soltar(i); s != "" {
			cands = append(cands, map[string]any{"index": i, "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": s}}}})
		}
	}
	if cands == nil {
		return nil
	}
	return map[string]any{"candidates": cands}
}

func decodificar(b []byte) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	return v, dec.Decode(&v) == nil
}

func codificar(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// desmascararGeminiJSON: resposta sem streaming (um objeto) ou o streaming sem SSE (uma lista de
// pedaços).
func (p *Proxy) desmascararGeminiJSON(b []byte, tab *mask.Tabela) []byte {
	v, ok := decodificar(b)
	if !ok {
		return b
	}
	g := p.novaVoltaGemini(tab)
	switch x := v.(type) {
	case map[string]any:
		g.pedaco(x, true)
	case []any:
		for n, e := range x {
			if m, _ := e.(map[string]any); m != nil {
				g.pedaco(m, n == len(x)-1)
			}
		}
	default:
		return b
	}
	return codificar(v)
}

// desmascararSSEGemini: streaming (alt=sse). Cada linha "data:" é um pedaço da resposta.
func (p *Proxy) desmascararSSEGemini(w http.ResponseWriter, body io.Reader, tab *mask.Tabela) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	g := p.novaVoltaGemini(tab)
	for {
		linha, err := rd.ReadString('\n')
		l := strings.TrimRight(linha, "\r\n")
		if dados, ok := strings.CutPrefix(l, "data:"); ok && !tab.Vazia() {
			if v, ok := decodificar([]byte(dados)); ok {
				if m, _ := v.(map[string]any); m != nil {
					g.pedaco(m, false)
					linha = "data: " + string(codificar(m)) + linha[len(l):]
				}
			}
		}
		io.WriteString(w, linha)
		if err != nil {
			if m := g.resto(); m != nil {
				fmt.Fprintf(w, "data: %s\n\n", codificar(m))
			}
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
