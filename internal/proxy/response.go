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

// Return: unmasks the response, in streaming (SSE) or whole (JSON).

// ---- SSE (streaming) -------------------------------------------------------

func (p *Proxy) unmaskSSE(w http.ResponseWriter, body io.Reader, tab *mask.Table) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	streams := map[int]*mask.Stream{}
	types := map[int]string{}
	raws := map[int]bool{} // blocks that pass without unmasking (tools that go to the internet)
	// who wrote it: the original text of each block (as the API sent it), recorded at the end of
	// the block by the hash of the unmasked one (see mask/memory_conversation.go)
	orig := map[int]*strings.Builder{}
	reg := func(s string) string { return p.m.RecordResponse(s, tab) }
	var event []string
	writeIt := func(name string, data []byte) {
		if name != "" {
			fmt.Fprintf(w, "event: %s\n", name)
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	process := func(lines []string) {
		name, data := "", ""
		for _, l := range lines {
			if strings.HasPrefix(l, "event:") {
				name = strings.TrimSpace(l[6:])
			} else if strings.HasPrefix(l, "data:") {
				data = strings.TrimPrefix(strings.TrimPrefix(l, "data:"), " ")
			}
		}
		if data == "" || tab.Empty() {
			w.Write([]byte(strings.Join(lines, "\n") + "\n\n"))
			return
		}
		var ev map[string]any
		if json.Unmarshal([]byte(data), &ev) != nil {
			w.Write([]byte(strings.Join(lines, "\n") + "\n\n"))
			return
		}
		idx := intOf(ev["index"])
		changed := false // an event that did not change goes out byte for byte as it came (thinking, ping, usage...)
		switch ev["type"] {
		case "content_block_start":
			cb, _ := ev["content_block"].(map[string]any)
			tp, _ := cb["type"].(string)
			types[idx] = tp
			if tp == "text" || tp == "tool_use" {
				orig[idx] = &strings.Builder{}
			}
			if tp == "text" {
				if s, ok := cb["text"].(string); ok && s != "" {
					orig[idx].WriteString(s)
					cb["text"], changed = tab.Unmask(s, false), true
				}
			}
			if tp == "tool_use" {
				if nameF, _ := cb["name"].(string); p.cfg.NoUnmask(nameF) {
					raws[idx] = true // WebFetch/WebSearch: the input keeps pseudonyms
					delete(orig, idx)
					break
				}
				// an input that arrives complete at the start of the block (without deltas)
				if in, ok := cb["input"].(map[string]any); ok && len(in) > 0 {
					cb["input"], changed = recordAll(in, reg), true
				}
			}
			streams[idx] = tab.NewStream(tp == "tool_use")
		case "content_block_delta":
			if raws[idx] {
				break
			}
			d, _ := ev["delta"].(map[string]any)
			field := map[any]string{"text_delta": "text", "input_json_delta": "partial_json"}[d["type"]]
			if field == "" { // thinking, signature, citations: pass intact
				break
			}
			f := streams[idx]
			if f == nil {
				f = tab.NewStream(field == "partial_json")
				streams[idx] = f
			}
			s, _ := d[field].(string)
			if o := orig[idx]; o != nil {
				o.WriteString(s)
			}
			out := f.Push(s)
			if out == "" {
				return // holding a possible cut pseudonym
			}
			if out != s {
				d[field], changed = out, true
			}
		case "content_block_stop":
			if f := streams[idx]; f != nil {
				if rest := f.Close(); rest != "" {
					deltaType, field := "text_delta", "text"
					if types[idx] == "tool_use" {
						deltaType, field = "input_json_delta", "partial_json"
					}
					extra, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": idx,
						"delta": map[string]any{"type": deltaType, field: rest}})
					writeIt("content_block_delta", extra)
				}
				delete(streams, idx)
			}
			if o := orig[idx]; o != nil {
				recordBlock(types[idx], o.String(), reg)
				delete(orig, idx)
			}
		}
		if !changed {
			w.Write([]byte(strings.Join(lines, "\n") + "\n\n"))
			return
		}
		b, _ := json.Marshal(ev)
		writeIt(name, b)
	}
	for {
		line, err := rd.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" && len(event) > 0 {
			process(event)
			event = event[:0]
			if fl != nil {
				fl.Flush()
			}
		} else if line != "" {
			event = append(event, line)
		}
		if err != nil {
			if len(event) > 0 {
				process(event)
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

func intOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	}
	return -1
}

// recordBlock: the end of a response block. Text: records the whole block. Tool input: records each
// of its strings (Claude Code resends it as an object, and the walker masks string by string).
func recordBlock(typ, original string, reg func(string) string) {
	switch typ {
	case "text":
		reg(original)
	case "tool_use":
		if original == "" {
			return
		}
		dec := json.NewDecoder(strings.NewReader(original))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) == nil {
			recordAll(v, reg)
		}
	}
}

// recordAll: like unmaskAll, recording each string (who wrote it).
func recordAll(v any, reg func(string) string) any {
	switch x := v.(type) {
	case string:
		return reg(x)
	case []any:
		for i := range x {
			x[i] = recordAll(x[i], reg)
		}
	case map[string]any:
		for k := range x {
			x[k] = recordAll(x[k], reg)
		}
	}
	return v
}

// unmaskAll swaps pseudonyms in all strings of an already decoded JSON value.
func unmaskAll(v any, tab *mask.Table) any {
	switch x := v.(type) {
	case string:
		return tab.Unmask(x, false)
	case []any:
		for i := range x {
			x[i] = unmaskAll(x[i], tab)
		}
	case map[string]any:
		for k := range x {
			x[k] = unmaskAll(x[k], tab)
		}
	}
	return v
}

// unmaskJSONResponse: responses without streaming (text and tool inputs). Inputs of tools in
// noUnmask (WebFetch, WebSearch) keep pseudonyms. reg (optional) unmasks recording who wrote it:
// the text of each text block and each string of the input of each tool.
func unmaskJSONResponse(b []byte, tab *mask.Table, noUnmask func(string) bool, reg func(string) string) []byte {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return b
	}
	var rec func(any, bool, bool) any
	rec = func(v any, inside, written bool) any {
		switch x := v.(type) {
		case string:
			if inside && written && reg != nil {
				return reg(x)
			}
			if inside {
				return tab.Unmask(x, false)
			}
		case []any:
			for i := range x {
				x[i] = rec(x[i], inside, written)
			}
		case map[string]any:
			t, _ := x["type"].(string)
			if t == "thinking" || t == "redacted_thinking" {
				return x
			}
			if nameF, _ := x["name"].(string); t == "tool_use" && noUnmask != nil && noUnmask(nameF) {
				return x
			}
			for k := range x {
				esc := written || t == "text" && k == "text" || t == "tool_use" && k == "input"
				if k == "text" || k == "input" || k == "content" || inside {
					if !untouchableKeys[k] {
						x[k] = rec(x[k], true, esc)
					}
				} else {
					x[k] = rec(x[k], false, esc)
				}
			}
		}
		return v
	}
	v = rec(v, false, false)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return b
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}
