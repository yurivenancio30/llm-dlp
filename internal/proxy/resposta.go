package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Volta: desmascara a resposta, em streaming (SSE) ou inteira (JSON).

// ---- SSE (streaming) -------------------------------------------------------

func (p *Proxy) desmascararSSE(w http.ResponseWriter, body io.Reader, tab *mask.Tabela) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	fluxos := map[int]*mask.Fluxo{}
	tipos := map[int]string{}
	crus := map[int]bool{} // blocos que passam sem desmascarar (ferramentas que vão para a internet)
	var evento []string
	escrever := func(nome string, dados []byte) {
		if nome != "" {
			fmt.Fprintf(w, "event: %s\n", nome)
		}
		fmt.Fprintf(w, "data: %s\n\n", dados)
	}
	processar := func(linhas []string) {
		nome, dados := "", ""
		for _, l := range linhas {
			if strings.HasPrefix(l, "event:") {
				nome = strings.TrimSpace(l[6:])
			} else if strings.HasPrefix(l, "data:") {
				dados = strings.TrimPrefix(strings.TrimPrefix(l, "data:"), " ")
			}
		}
		if dados == "" || tab.Vazia() {
			w.Write([]byte(strings.Join(linhas, "\n") + "\n\n"))
			return
		}
		var ev map[string]any
		if json.Unmarshal([]byte(dados), &ev) != nil {
			w.Write([]byte(strings.Join(linhas, "\n") + "\n\n"))
			return
		}
		idx := intDe(ev["index"])
		mudou := false // evento que não mudou sai byte a byte como veio (thinking, ping, uso...)
		switch ev["type"] {
		case "content_block_start":
			cb, _ := ev["content_block"].(map[string]any)
			tp, _ := cb["type"].(string)
			tipos[idx] = tp
			if tp == "text" {
				if s, ok := cb["text"].(string); ok && s != "" {
					cb["text"], mudou = tab.Desmascarar(s, false), true
				}
			}
			if tp == "tool_use" {
				if nomeF, _ := cb["name"].(string); p.cfg.SemDesmascarar(nomeF) {
					crus[idx] = true // WebFetch/WebSearch: a entrada segue com pseudônimos
					break
				}
				// entrada que já chega completa no início do bloco (sem deltas)
				if in, ok := cb["input"].(map[string]any); ok && len(in) > 0 {
					cb["input"], mudou = desmascararTudo(in, tab), true
				}
			}
			fluxos[idx] = tab.NovoFluxo(tp == "tool_use")
		case "content_block_delta":
			if crus[idx] {
				break
			}
			d, _ := ev["delta"].(map[string]any)
			campo := map[any]string{"text_delta": "text", "input_json_delta": "partial_json"}[d["type"]]
			if campo == "" { // thinking, assinatura, citações: passa intacto
				break
			}
			f := fluxos[idx]
			if f == nil {
				f = tab.NovoFluxo(campo == "partial_json")
				fluxos[idx] = f
			}
			s, _ := d[campo].(string)
			out := f.Empurrar(s)
			if out == "" {
				return // segurando um possível pseudônimo cortado
			}
			if out != s {
				d[campo], mudou = out, true
			}
		case "content_block_stop":
			if f := fluxos[idx]; f != nil {
				if resto := f.Fechar(); resto != "" {
					tipoDelta, campo := "text_delta", "text"
					if tipos[idx] == "tool_use" {
						tipoDelta, campo = "input_json_delta", "partial_json"
					}
					extra, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": idx,
						"delta": map[string]any{"type": tipoDelta, campo: resto}})
					escrever("content_block_delta", extra)
				}
				delete(fluxos, idx)
			}
		}
		if !mudou {
			w.Write([]byte(strings.Join(linhas, "\n") + "\n\n"))
			return
		}
		b, _ := json.Marshal(ev)
		escrever(nome, b)
	}
	for {
		linha, err := rd.ReadString('\n')
		linha = strings.TrimRight(linha, "\r\n")
		if linha == "" && len(evento) > 0 {
			processar(evento)
			evento = evento[:0]
			if fl != nil {
				fl.Flush()
			}
		} else if linha != "" {
			evento = append(evento, linha)
		}
		if err != nil {
			if len(evento) > 0 {
				processar(evento)
				if fl != nil {
					fl.Flush()
				}
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func intDe(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	}
	return -1
}

// desmascararTudo troca pseudônimos em todas as strings de um valor JSON já decodificado.
func desmascararTudo(v any, tab *mask.Tabela) any {
	switch x := v.(type) {
	case string:
		return tab.Desmascarar(x, false)
	case []any:
		for i := range x {
			x[i] = desmascararTudo(x[i], tab)
		}
	case map[string]any:
		for k := range x {
			x[k] = desmascararTudo(x[k], tab)
		}
	}
	return v
}

// desmascararJSONResposta: respostas sem streaming (texto e entradas de ferramenta).
// Entradas de ferramentas em semDesm (WebFetch, WebSearch) seguem com pseudônimos.
func desmascararJSONResposta(b []byte, tab *mask.Tabela, semDesm func(string) bool) []byte {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return b
	}
	var rec func(any, bool) any
	rec = func(v any, dentro bool) any {
		switch x := v.(type) {
		case string:
			if dentro {
				return tab.Desmascarar(x, false)
			}
		case []any:
			for i := range x {
				x[i] = rec(x[i], dentro)
			}
		case map[string]any:
			t, _ := x["type"].(string)
			if t == "thinking" || t == "redacted_thinking" {
				return x
			}
			if nomeF, _ := x["name"].(string); t == "tool_use" && semDesm != nil && semDesm(nomeF) {
				return x
			}
			for k := range x {
				if k == "text" || k == "input" || k == "content" || dentro {
					if !chavesIntocaveis[k] {
						x[k] = rec(x[k], true)
					}
				} else {
					x[k] = rec(x[k], false)
				}
			}
		}
		return v
	}
	v = rec(v, false)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return b
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}
