package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Ida: mascara o corpo da requisição (formato da API da Anthropic).

// mascararCorpo mascara o JSON da requisição. Corpo que não é JSON não sai (falha fechada).
func (p *Proxy) mascararCorpo(r *http.Request, corpo []byte) ([]byte, []mask.Entrada, error) {
	dec := json.NewDecoder(bytes.NewReader(corpo))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, nil, fmt.Errorf("corpo não é JSON (%s)", r.Header.Get("content-type"))
	}
	var textos []string
	coletarTextos(v, &textos)
	p.m.Aquecer(textos)
	var ents []mask.Entrada
	var errMidia error
	w := walker{m: p.m, md: p.midia, ents: &ents, err: &errMidia}
	if strings.HasPrefix(r.URL.Path, "/v1/messages") {
		v = w.requisicaoAnthropic(v)
	} else {
		v = w.generico(v)
	}
	if errMidia != nil {
		// imagem/PDF que não deu para verificar nunca sai, mesmo com falhar_fechado=false
		return nil, nil, erroObrigatorio{errMidia}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), ents, nil
}

// coletarTextos junta os textos grandes da requisição, para serem mascarados em paralelo
// antes da montagem (ver mask.Aquecer). Fica de fora o que nunca é mascarado: o raciocínio
// assinado e o conteúdo de imagens/PDFs em base64.
func coletarTextos(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		if len(x) >= 512 {
			*out = append(*out, x)
		}
	case []any:
		for _, e := range x {
			coletarTextos(e, out)
		}
	case map[string]any:
		if t, _ := x["type"].(string); t == "thinking" || t == "redacted_thinking" || t == "base64" {
			return
		}
		for _, e := range x {
			coletarTextos(e, out)
		}
	}
}

// walker percorre o JSON de uma requisição e mascara as strings que podem levar
// dado do usuário, guardando as entradas pseudônimo -> real usadas.
type walker struct {
	m    *mask.Masker
	md   *Midia
	ents *[]mask.Entrada
	err  *error // primeiro erro ao tratar mídia (a requisição inteira é recusada)
}

// midia trata um bloco de imagem/PDF; devolve os blocos que o substituem.
func (w walker) midia(b map[string]any) []any {
	if w.md == nil {
		*w.err = errSemMidia
		return nil
	}
	blocos, ents, err := w.md.Processar(b)
	if err != nil {
		if *w.err == nil {
			*w.err = err
		}
		return nil
	}
	*w.ents = append(*w.ents, ents...)
	return blocos
}

var errSemMidia = errorString("tratamento de imagens/PDF não configurado")

type errorString string

func (e errorString) Error() string { return string(e) }

func ehBase64(b map[string]any) bool {
	src, _ := b["source"].(map[string]any)
	return src != nil && src["type"] == "base64"
}

func (w walker) s(v string) string {
	out, e := w.m.Mascarar(v)
	*w.ents = append(*w.ents, e...)
	return out
}

// Chaves que nunca carregam dado do usuário (ou que não podem mudar).
var chavesIntocaveis = map[string]bool{"type": true, "id": true, "tool_use_id": true, "signature": true,
	"media_type": true, "cache_control": true, "model": true, "role": true, "stop_reason": true,
	"stop_sequence": true, "citations": false}

// requisicaoAnthropic: /v1/messages e /v1/messages/count_tokens.

func (w walker) requisicaoAnthropic(v any) any {
	req, ok := v.(map[string]any)
	if !ok {
		return w.generico(v)
	}
	for k, val := range req {
		switch k {
		case "system":
			req[k] = w.conteudo(val)
		case "messages":
			if msgs, ok := val.([]any); ok {
				for _, mm := range msgs {
					if msg, ok := mm.(map[string]any); ok {
						msg["content"] = w.conteudo(msg["content"])
					}
				}
			}
		case "tools":
			// descrições podem citar dado (ex.: ferramenta de MCP); o nome não pode mudar
			if ts, ok := val.([]any); ok {
				for _, t := range ts {
					if tm, ok := t.(map[string]any); ok {
						for tk, tv := range tm {
							if tk != "name" && tk != "type" {
								tm[tk] = w.generico(tv)
							}
						}
					}
				}
			}
		case "model", "max_tokens", "stream", "temperature", "top_p", "top_k", "stop_sequences", "tool_choice",
			"thinking", "metadata", "service_tier", "context_management", "container", "output_config",
			"betas", "speed", "mcp_servers":
			// parâmetros e identificadores: não carregam dado do usuário e não podem mudar
		default:
			// campo que não conheço: mascara por precaução (falha para o lado seguro)
			req[k] = w.generico(val)
		}
	}
	return req
}

// conteudo: string ou lista de blocos.
func (w walker) conteudo(v any) any {
	switch c := v.(type) {
	case string:
		return w.s(c)
	case []any:
		// uma imagem/PDF pode virar mais de um bloco (imagem coberta + nota, páginas)
		out := make([]any, 0, len(c))
		for _, b := range c {
			bl, ok := b.(map[string]any)
			if !ok {
				out = append(out, b)
				continue
			}
			if (bl["type"] == "image" || bl["type"] == "document") && ehBase64(bl) {
				out = append(out, w.midia(bl)...)
				continue
			}
			out = append(out, w.bloco(bl))
		}
		return out
	}
	return v
}

func (w walker) bloco(b map[string]any) map[string]any {
	switch b["type"] {
	case "thinking", "redacted_thinking":
		// assinado pela API: qualquer mudança invalida a conversa. O modelo só viu
		// pseudônimos, então o raciocínio dele também só contém pseudônimos.
		return b
	case "image":
		return b // só chega aqui se não for base64 (url/arquivo hospedado)
	case "server_tool_use", "web_search_tool_result", "web_fetch_tool_result", "code_execution_tool_result",
		"container_upload", "mcp_tool_use", "mcp_tool_result":
		return b // gerados do lado da API
	case "text":
		if s, ok := b["text"].(string); ok {
			b["text"] = w.s(s)
		}
		return b
	case "tool_use":
		b["input"] = w.tudo(b["input"])
		return b
	case "tool_result":
		b["content"] = w.conteudo(b["content"])
		return b
	case "document":
		if src, ok := b["source"].(map[string]any); ok {
			switch src["type"] {
			case "text":
				if s, ok := src["data"].(string); ok {
					src["data"] = w.s(s)
				}
			case "content":
				src["content"] = w.conteudo(src["content"])
			}
		}
		for _, k := range []string{"title", "context"} {
			if s, ok := b[k].(string); ok {
				b[k] = w.s(s)
			}
		}
		return b
	}
	// tipo desconhecido: mascara toda string fora das chaves intocáveis
	return w.generico(b).(map[string]any)
}

// tudo mascara todas as strings (entrada de ferramenta: comando, caminho, conteúdo...).
func (w walker) tudo(v any) any {
	switch x := v.(type) {
	case string:
		return w.s(x)
	case []any:
		for i := range x {
			x[i] = w.tudo(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = w.tudo(x[k])
		}
	}
	return v
}

// generico mascara strings de qualquer JSON, exceto chaves intocáveis e blocos de raciocínio.
func (w walker) generico(v any) any {
	switch x := v.(type) {
	case string:
		return w.s(x)
	case []any:
		for i := range x {
			x[i] = w.generico(x[i])
		}
	case map[string]any:
		if t, _ := x["type"].(string); t == "thinking" || t == "redacted_thinking" || t == "image" {
			return x
		}
		for k := range x {
			if !chavesIntocaveis[k] && k != "data" {
				x[k] = w.generico(x[k])
			}
		}
	}
	return v
}
