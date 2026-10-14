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

// Gemini API format (generateContent, streamGenerateContent, countTokens): what Gemini CLI,
// opencode and Google's SDKs send.
//
// The outbound side has no masker of its own. The request is seen in the Anthropic shape
// (geminiBridge) and goes through the same path as /v1/messages: the conversation memory, tracing,
// command hints, freezing of what already went out and image handling apply the same. What changes
// name:
//
//	systemInstruction.parts[].text          -> system
//	tools[].functionDeclarations[]          -> tools (description and schema)
//	contents[] with role "model"            -> assistant message
//	parts[].text                            -> text block
//	parts[].text with "thought": true       -> reasoning (unchanged, like thinking)
//	parts[].functionCall {name, args}       -> tool_use
//	parts[].functionResponse {name, response} -> tool_result (each text of response)
//	parts[].inlineData {mimeType, data}     -> image or PDF (OCR)
//
// Texts are masked in place: the maps of the original request are the same ones the masker walks,
// and what it returns in a new block is copied back (goBack).

// partMark: in the block created by the bridge, the index of the part it came from.
const partMark = "\x00parte"

// Gemini parameters that carry no user data (or that cannot change)
var geminiParams = map[string]bool{"contents": true, "tools": true, "systemInstruction": true, "system_instruction": true,
	"generationConfig": true, "generation_config": true, "safetySettings": true, "safety_settings": true,
	"toolConfig": true, "tool_config": true, "cachedContent": true, "cached_content": true, "model": true,
	"labels": true, "serviceTier": true, "service_tier": true, "store": true, "session_id": true}

// Gemini CLI with the Google account (Login with Google) does not talk to the Gemini API: it talks
// to the Code Assist API (cloudcode-pa.googleapis.com/v1internal:<method>), pointed by the
// CODE_ASSIST_ENDPOINT variable. The conversation goes in the Gemini format inside "request"
// ({model, project, user_prompt_id, request: {contents, systemInstruction, tools...}}), and the
// response comes back inside "response" ({response: {candidates...}, traceId}). The account methods
// (login, quota, experiments) carry no conversation and pass as they came.
var accountMethods = map[string]bool{"loadCodeAssist": true, "onboardUser": true, "getCodeAssistGlobalUserSetting": true,
	"setCodeAssistGlobalUserSetting": true, "listExperiments": true, "retrieveUserQuota": true, "fetchAdminControls": true}

// accountFormat: a Code Assist account method (see accountMethods): passes without changing
// anything.
const accountFormat = "codeassist-conta"

// codeAssistBridge: the Gemini request that goes inside "request", seen as the Gemini one. The
// envelope (model, project, ids) does not change.
func codeAssistBridge(v any) (map[string]any, func(), bool) {
	req, _ := v.(map[string]any)
	in, ok := req["request"].(map[string]any)
	if !ok {
		return nil, nil, false
	}
	return geminiBridge(in)
}

func field(m map[string]any, names ...string) (any, string) {
	for _, n := range names {
		if v, ok := m[n]; ok {
			return v, n
		}
	}
	return nil, names[0]
}

// geminiIDs matches each functionResponse with its functionCall. Newer Gemini sends an id; without
// it, order applies: the n-th response of a name belongs to the n-th call of that name.
type geminiIDs struct{ calls, replies map[string]int }

func (g *geminiIDs) de(m map[string]any, cnt map[string]int) string {
	if id, _ := m["id"].(string); id != "" {
		return id
	}
	name, _ := m["name"].(string)
	cnt[name]++
	return fmt.Sprintf("g:%s:%d", name, cnt[name])
}

// leaves calls f for each string of v (objects in key order), with how to write it back.
func leaves(v any, saveIt func(any), f func(s string, saveIt func(any))) {
	switch x := v.(type) {
	case string:
		f(x, saveIt)
	case []any:
		for i := range x {
			i := i
			leaves(x[i], func(n any) { x[i] = n }, f)
		}
	case map[string]any:
		for _, k := range keys(x) {
			k := k
			leaves(x[k], func(n any) { x[k] = n }, f)
		}
	}
}

// geminiBridge returns the Gemini request in the Anthropic shape and the function that, after
// masking, copies the result back to the original request. ok = false: not a request with
// "contents" (falls into generic mode, which masks all text).
func geminiBridge(v any) (map[string]any, func(), bool) {
	req, _ := v.(map[string]any)
	if req == nil {
		return nil, nil, false
	}
	if in, ok := req["generateContentRequest"].(map[string]any); ok { // countTokens
		req = in
	}
	cnts, ok := req["contents"].([]any)
	if !ok {
		return nil, nil, false
	}
	anth := map[string]any{}
	var returns []func()

	if ts, _ := req["tools"].([]any); ts != nil {
		tools := []any{}
		for _, t := range ts {
			tm, _ := t.(map[string]any)
			fds, _ := field(tm, "functionDeclarations", "function_declarations")
			list, _ := fds.([]any)
			for _, fd := range list {
				if m, ok := fd.(map[string]any); ok {
					tools = append(tools, m) // the same map: description and schema are masked in place
				}
			}
		}
		anth["tools"] = tools
	}

	if si, _ := field(req, "systemInstruction", "system_instruction"); si != nil {
		sm, _ := si.(map[string]any)
		ps, _ := sm["parts"].([]any)
		blocks := []any{}
		for _, pt := range ps {
			pm, _ := pt.(map[string]any)
			if t, ok := pm["text"].(string); ok {
				b := map[string]any{"type": "text", "text": t}
				blocks = append(blocks, b)
				returns = append(returns, func() { pm["text"] = b["text"] })
			}
		}
		anth["system"] = blocks
	}

	ids := &geminiIDs{map[string]int{}, map[string]int{}}
	msgs := []any{}
	for _, c := range cnts {
		cm, _ := c.(map[string]any)
		ps, _ := cm["parts"].([]any)
		if cm == nil {
			continue
		}
		hostRole := "user"
		if cm["role"] == "model" {
			hostRole = "assistant"
		}
		var parts []map[string]any
		blocks := []any{}
		for _, pt := range ps {
			pm, _ := pt.(map[string]any)
			if pm == nil {
				continue
			}
			b := map[string]any{partMark: len(parts)}
			parts = append(parts, pm)
			fc, _ := field(pm, "functionCall", "function_call")
			fr, _ := field(pm, "functionResponse", "function_response")
			in, _ := field(pm, "inlineData", "inline_data")
			text, hasText := pm["text"].(string)
			switch {
			case pm["thought"] == true:
				// reasoning summary: the model only saw pseudonyms and the return path does not
				// unmask it
				b["type"], b["thinking"] = "thinking", pm["text"]
			case fc != nil:
				fm, _ := fc.(map[string]any)
				b["type"], b["id"], b["name"], b["input"] = "tool_use", ids.de(fm, ids.calls), fm["name"], fm["args"]
			case fr != nil:
				fm, _ := fr.(map[string]any)
				cnt := []any{}
				leaves(fm["response"], func(n any) { fm["response"] = n }, func(s string, saveIt func(any)) {
					t := map[string]any{"type": "text", "text": s}
					cnt = append(cnt, t)
					returns = append(returns, func() { saveIt(t["text"]) })
				})
				b["type"], b["tool_use_id"], b["content"] = "tool_result", ids.de(fm, ids.replies), cnt
			case in != nil:
				im, _ := in.(map[string]any)
				mt, _ := field(im, "mimeType", "mime_type")
				typ, _ := mt.(string)
				switch {
				case typ == "application/pdf":
					b["type"] = "document"
				case strings.HasPrefix(typ, "image/"):
					b["type"] = "image"
				default:
					// audio, video and others: there is no way to check; does not go out (fail
					// closed)
					delete(b, partMark)
					b["type"], b["text"] = "text", "[llm-dlp] Um arquivo anexado foi retirado daqui e não foi enviado: o llm-dlp só consegue verificar imagem e PDF. Avise o usuário."
				}
				if b["type"] != "text" {
					b["source"] = map[string]any{"type": "base64", "media_type": typ, "data": im["data"]}
				}
			case hasText:
				b["type"], b["text"] = "text", text
			case pm["fileData"] != nil || pm["file_data"] != nil || pm["executableCode"] != nil || pm["codeExecutionResult"] != nil:
				b["type"] = "server_tool_use" // already on the API side, or the API generated it: unchanged
			default:
				b["type"], b["parte"] = "parte desconhecida", pm // masks all of its text, as a precaution
			}
			blocks = append(blocks, b)
		}
		am := map[string]any{"role": hostRole, "content": blocks}
		msgs = append(msgs, am)
		returns = append(returns, func() { cm["parts"] = partsOf(am["content"], parts) })
	}
	anth["messages"] = msgs

	for _, k := range keys(req) {
		if !geminiParams[k] { // field I do not know: mask as a precaution, as with Anthropic
			k := k
			anth["gemini:"+k] = req[k]
			returns = append(returns, func() { req[k] = anth["gemini:"+k] })
		}
	}
	return anth, func() {
		for _, f := range returns {
			f()
		}
	}, true
}

// partsOf: the blocks of an already masked message, back as Gemini parts. The block that came from
// a part updates that part (and keeps whatever else it had, like the thoughtSignature); a new block
// (the llm-dlp note, a PDF page) becomes a new part.
func partsOf(content any, parts []map[string]any) []any {
	blocks, _ := content.([]any)
	out := make([]any, 0, len(blocks))
	for _, x := range blocks {
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
				pm = map[string]any{}
			}
			pm["text"] = b["text"]
		case "tool_use":
			if fc, _ := field(pm, "functionCall", "function_call"); fc != nil && b["input"] != nil {
				fc.(map[string]any)["args"] = b["input"]
			}
		case "image", "document":
			if src["type"] != "base64" { // PDF that became masked text
				t, _ := src["data"].(string)
				if title, _ := b["title"].(string); title != "" {
					t = title + "\n\n" + t
				}
				pm = map[string]any{"text": t}
				break
			}
			var im map[string]any
			if pm != nil {
				v, _ := field(pm, "inlineData", "inline_data")
				im, _ = v.(map[string]any)
			}
			if im == nil {
				im = map[string]any{}
				pm = map[string]any{"inlineData": im}
			}
			_, key := field(im, "mimeType", "mime_type")
			im[key], im["data"] = src["media_type"], src["data"]
		}
		if pm != nil {
			out = append(out, pm)
		}
	}
	return out
}

// ---- volta --------------------------------------------------------------------------------

// geminiReturn unmasks the Gemini response, whole or in pieces (each streaming piece is a partial
// response). The text goes through one Stream per candidate, which holds a pseudonym cut between
// two pieces; the arguments of a tool call arrive whole.
type geminiReturn struct {
	p       *Proxy
	tab     *mask.Table
	streams map[int]*mask.Stream
	orig    map[int]*strings.Builder // the text as the API sent it (who wrote it)
}

func (p *Proxy) newGeminiReturn(tab *mask.Table) *geminiReturn {
	return &geminiReturn{p: p, tab: tab, streams: map[int]*mask.Stream{}, orig: map[int]*strings.Builder{}}
}

// release: the text the candidate's stream was still holding, and records what the API wrote.
func (g *geminiReturn) release(i int) string {
	rest := ""
	if f := g.streams[i]; f != nil {
		rest = f.Close()
		delete(g.streams, i)
	}
	if o := g.orig[i]; o != nil {
		g.p.m.RecordResponse(o.String(), g.tab)
		delete(g.orig, i)
	}
	return rest
}

// piece unmasks a response (or a piece of it), in place. end = nothing else is coming.
func (g *geminiReturn) piece(resp map[string]any, end bool) {
	if _, ok := resp["candidates"]; !ok {
		if in, _ := resp["response"].(map[string]any); in != nil { // Code Assist: {response: {...}, traceId}
			g.piece(in, end)
			return
		}
	}
	cands, _ := resp["candidates"].([]any)
	for pos, c := range cands {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		i := intOf(cm["index"])
		if i < 0 {
			i = pos
		}
		cnt, _ := cm["content"].(map[string]any)
		ps, _ := cnt["parts"].([]any)
		newOnes := make([]any, 0, len(ps)+1)
		text := func(s string) { // appends already unmasked text to the last text block
			if s == "" {
				return
			}
			if n := len(newOnes); n > 0 {
				if u, _ := newOnes[n-1].(map[string]any); u != nil && u["thought"] != true {
					if t, ok := u["text"].(string); ok {
						u["text"] = t + s
						return
					}
				}
			}
			newOnes = append(newOnes, map[string]any{"text": s})
		}
		for _, pt := range ps {
			pm, _ := pt.(map[string]any)
			t, hasText := pm["text"].(string)
			fc, _ := field(pm, "functionCall", "function_call")
			switch {
			case pm == nil:
				newOnes = append(newOnes, pt)
			case hasText && pm["thought"] != true:
				if g.streams[i] == nil {
					g.streams[i], g.orig[i] = g.tab.NewStream(false), &strings.Builder{}
				}
				g.orig[i].WriteString(t)
				out := g.streams[i].Push(t)
				if len(pm) == 1 { // only the text: joins the previous one, or disappears if the stream held everything
					text(out)
				} else {
					pm["text"] = out
					newOnes = append(newOnes, pm)
				}
			default:
				// something else after text: the text block is over
				text(g.release(i))
				if fm, _ := fc.(map[string]any); fm != nil {
					if name, _ := fm["name"].(string); !g.p.bridgeNoUnmask(name) {
						fm["args"] = recordAll(fm["args"], func(s string) string { return g.p.m.RecordResponse(s, g.tab) })
					}
				}
				newOnes = append(newOnes, pm)
			}
		}
		if fr, _ := cm["finishReason"].(string); fr != "" || end {
			text(g.release(i))
		}
		if cnt != nil {
			cnt["parts"] = newOnes
		} else if len(newOnes) > 0 {
			cm["content"] = map[string]any{"role": "model", "parts": newOnes}
		}
	}
}

// rest: what was held when streaming ends without notice, as one more piece.
func (g *geminiReturn) rest() map[string]any {
	var cands []any
	idx := make([]int, 0, len(g.streams))
	for i := range g.streams {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		if s := g.release(i); s != "" {
			cands = append(cands, map[string]any{"index": i, "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": s}}}})
		}
	}
	if cands == nil {
		return nil
	}
	return map[string]any{"candidates": cands}
}

func decode(b []byte) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	return v, dec.Decode(&v) == nil
}

func encode(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// unmaskGeminiJSON: a response without streaming (an object) or streaming without SSE (a list of
// pieces).
func (p *Proxy) unmaskGeminiJSON(b []byte, tab *mask.Table) []byte {
	v, ok := decode(b)
	if !ok {
		return b
	}
	g := p.newGeminiReturn(tab)
	switch x := v.(type) {
	case map[string]any:
		g.piece(x, true)
	case []any:
		for n, e := range x {
			if m, _ := e.(map[string]any); m != nil {
				g.piece(m, n == len(x)-1)
			}
		}
	default:
		return b
	}
	return encode(v)
}

// unmaskSSEGemini: streaming (alt=sse). Each "data:" line is a piece of the response.
func (p *Proxy) unmaskSSEGemini(w http.ResponseWriter, body io.Reader, tab *mask.Table) error {
	fl, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(body, 64<<10)
	g := p.newGeminiReturn(tab)
	for {
		line, err := rd.ReadString('\n')
		l := strings.TrimRight(line, "\r\n")
		if data, ok := strings.CutPrefix(l, "data:"); ok && !tab.Empty() {
			if v, ok := decode([]byte(data)); ok {
				if m, _ := v.(map[string]any); m != nil {
					g.piece(m, false)
					line = "data: " + string(encode(m)) + line[len(l):]
				}
			}
		}
		io.WriteString(w, line)
		if err != nil {
			if m := g.rest(); m != nil {
				fmt.Fprintf(w, "data: %s\n\n", encode(m))
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
