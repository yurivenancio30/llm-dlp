package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Ida: mascara o corpo da requisição (formato da API da Anthropic).

// mascararCorpo mascara o JSON da requisição. Corpo que não é JSON não sai (falha fechada).
// Devolve também o lote dos textos, para congelar depois que a requisição sair.
func (p *Proxy) mascararCorpo(r *http.Request, corpo []byte) ([]byte, []mask.Entrada, *mask.Lote, error) {
	dec := json.NewDecoder(bytes.NewReader(corpo))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, nil, nil, fmt.Errorf("corpo não é JSON (%s)", r.Header.Get("content-type"))
	}
	anthropic := strings.HasPrefix(r.URL.Path, "/v1/messages")
	lote := p.m.NovoLote()
	// 1ª passada: só junta os textos (os mesmos que a montagem vai mascarar) e os mascara
	// antes, em paralelo quando são muitos. Assim, todo valor aprendido nesta requisição já
	// vale quando ela é montada, e a requisição seguinte não muda nada do que esta enviou.
	col := &coleta{}
	wc := walker{cfg: p.cfg, col: col}
	if anthropic {
		wc.requisicaoAnthropic(v)
	} else {
		wc.generico(v)
	}
	lote.Aquecer(col.locais, col.daWeb)

	// 2ª passada: monta, em ordem
	var ents []mask.Entrada
	var errMidia error
	w := walker{cfg: p.cfg, lote: lote, md: p.midia, ents: &ents, err: &errMidia}
	if anthropic {
		v = w.requisicaoAnthropic(v)
	} else {
		v = w.generico(v)
	}
	if errMidia != nil {
		// imagem/PDF que não deu para verificar nunca sai, mesmo com falhar_fechado=false
		return nil, nil, nil, erroObrigatorio{errMidia}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, nil, nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), ents, lote, nil
}

// coleta: os textos que a montagem vai mascarar, separados pela origem.
type coleta struct{ locais, daWeb []string }

// walker percorre o JSON de uma requisição e mascara as strings que podem levar
// dado do usuário, guardando as entradas pseudônimo -> real usadas.
type walker struct {
	cfg  config.Config
	lote *mask.Lote
	md   *Midia
	ents *[]mask.Entrada
	err  *error // primeiro erro ao tratar mídia (a requisição inteira é recusada)
	// col != nil: só junta os textos, sem mudar nada (1ª passada)
	col *coleta
	// idsWeb: tool_use_id das chamadas de WebFetch/WebSearch desta conversa
	idsWeb map[string]bool
	// daWeb: o texto atual veio da internet (mascara, mas não aprende)
	daWeb bool
}

// midia trata um bloco de imagem/PDF; devolve os blocos que o substituem.
func (w walker) midia(b map[string]any) []any {
	if w.col != nil {
		return []any{b}
	}
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
	if w.col != nil {
		if w.daWeb {
			w.col.daWeb = append(w.col.daWeb, v)
		} else {
			w.col.locais = append(w.col.locais, v)
		}
		return v
	}
	out, e := w.lote.Mascarar(v, w.daWeb)
	*w.ents = append(*w.ents, e...)
	return out
}

// web: as ferramentas da lista ferramentas_sem_desmascarar (WebFetch, WebSearch) trazem
// conteúdo da internet. Um exemplo de documentação (a senha de exemplo numa URL) não é
// segredo do usuário: é mascarado onde aparece, mas não é lembrado. Os conectores do
// claude.ai (mcp__claude_ai_*) ficam de fora: trazem dado do próprio usuário.
func (w walker) web(nome string) bool {
	for _, f := range w.cfg.FerramentasSemDesmascarar {
		if f == nome {
			return true
		}
	}
	return false
}

// idsDaWeb junta os ids das chamadas de ferramentas da web feitas na conversa.
func (w walker) idsDaWeb(msgs []any) map[string]bool {
	ids := map[string]bool{}
	for _, mm := range msgs {
		msg, _ := mm.(map[string]any)
		blocos, _ := msg["content"].([]any)
		for _, b := range blocos {
			bl, _ := b.(map[string]any)
			if bl == nil || bl["type"] != "tool_use" {
				continue
			}
			if nome, _ := bl["name"].(string); w.web(nome) {
				if id, _ := bl["id"].(string); id != "" {
					ids[id] = true
				}
			}
		}
	}
	return ids
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
				w.idsWeb = w.idsDaWeb(msgs)
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
		// a entrada de WebFetch/WebSearch é escrita pelo modelo a partir do que leu na web
		if nome, _ := b["name"].(string); w.web(nome) {
			w.daWeb = true
		}
		b["input"] = w.tudo(b["input"])
		return b
	case "tool_result":
		if id, _ := b["tool_use_id"].(string); w.idsWeb[id] {
			w.daWeb = true
		}
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
