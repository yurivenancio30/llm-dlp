package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
)

// Outbound: masks the request body (Anthropic API format).

// maskBody masks the request JSON. A body that is not JSON does not go out (fail closed). Also
// returns the batch of texts, to freeze after the request goes out.
//
// format: "anthropic" (/v1/messages); "gemini", "codeassist", "openai" or "responses" (seen in the
// Anthropic shape by a bridge: see gemini.go, openai.go and responses.go); or another one (generic
// mode: all text is masked).
func (p *Proxy) maskBody(format, typ string, body []byte) ([]byte, []mask.Entry, *mask.Batch, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var original any
	if err := dec.Decode(&original); err != nil {
		return nil, nil, nil, fmt.Errorf("corpo não é JSON (%s)", typ)
	}
	anthropic := format == "anthropic"
	v, goBack := original, func() {}
	// another format with its own reader: the request is seen in the Anthropic shape (bridge)
	bridge := map[string]func(any) (map[string]any, func(), bool){"gemini": geminiBridge, "openai": openAIBridge,
		"codeassist": codeAssistBridge, "responses": responsesBridge}[format]
	withBridge := false
	if bridge != nil {
		if a, f, ok := bridge(original); ok {
			v, goBack, anthropic, withBridge = a, f, true, true
		}
	}
	batch := p.m.NewBatch()
	// 1st pass: only gathers the texts (the same ones the assembly will mask) and masks them first,
	// in parallel when there are many. This way, every value learned in this request already
	// applies when it is assembled, and the next request changes nothing of what this one sent.
	col := &collection{}
	wc := walker{cfg: p.cfg, m: p.m, col: col, batch: batch, pos: &position{}, bridge: withBridge}
	if anthropic {
		wc.anthropicRequest(v)
	} else {
		wc.pos.block = after(mask.Position{}, v)
		wc.generic(v)
	}
	batch.Warm(col.items)
	if anthropic {
		// priority (priority.go), with what each tool output decided (already in the memo)
		batch.UsePublic(modelPublics(v, p.m, batch.DecidedByText(col.items)))
	}
	// conversation memory: the names decided in all texts apply to the texts not yet sent (see
	// mask/memory_conversation.go)
	batch.Memory(col.items, col.extras)

	// 2nd pass: assembles, in order
	var ents []mask.Entry
	var errMedia error
	// the positions were already computed in the 1st pass (the blocks were still intact)
	w := walker{cfg: p.cfg, m: p.m, batch: batch, md: p.media, ents: &ents, err: &errMedia, pos: &position{seq: wc.pos.seq}, bridge: withBridge}
	if anthropic {
		v = w.anthropicRequest(v)
	} else {
		// outside /v1/messages there is no conversation: only freezes the resend of the same whole
		// body
		w.pos.block = after(mask.Position{}, v)
		v = w.generic(v)
	}
	if errMedia != nil {
		// an image/PDF that could not be checked never goes out, even with fail_closed=false
		return nil, nil, nil, requiredError{errMedia}
	}
	if withBridge {
		goBack() // what was masked in the Anthropic shape goes back to the original request
	} else {
		original = v
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(original); err != nil {
		return nil, nil, nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), ents, batch, nil
}

// collection: the texts the assembly will mask, with the position and origin of each one, and the
// decisions of the translated pieces of the assistant texts that come back as the API sent them.
type collection struct {
	items  []mask.BatchItem
	extras []mask.Decision
}

// position: where the walker is in the conversation. Each block (tool, system block, message block)
// chains the hash of the previous one, in the order in which the API builds the cache: tools,
// system, messages. The texts of a block have the position of everything that comes before it. This
// way freezing (see mask.Batch) only applies to the exact resend of that point of the conversation.
//
// The position of a text is the hash of its block (which covers everything that comes before and
// the whole block) plus the order of the text inside the block, in a fixed-order walk. This way the
// text of a block that already went out is recognized without computing its hash again.
type position struct {
	current mask.Position
	// seq: the position after each block, in order; the 1st pass computes it, the 2nd reuses it
	seq []mask.Position
	i   int
	// block and n: the current block and how many of its texts were already seen
	block mask.Position
	n     uint64
}

// ofText: the position of the next text of the current block.
func (p *position) ofText() mask.Position {
	var b [40]byte
	copy(b[:], p.block[:])
	binary.LittleEndian.PutUint64(b[32:], p.n)
	p.n++
	return sha256.Sum256(b[:])
}

// after: the position following a block (computed before the block is masked).
func after(prev mask.Position, v any) mask.Position {
	h := sha256.New()
	h.Write(prev[:])
	hashJSON(h, v)
	var out mask.Position
	copy(out[:], h.Sum(nil))
	return out
}

// hashJSON writes v into h in a stable form: keys in order and without cache_control (the cache
// marker moves with every message and is not part of the content).
func hashJSON(h hash.Hash, v any) {
	var n [8]byte
	writeIt := func(tag byte, s string) {
		binary.LittleEndian.PutUint64(n[:], uint64(len(s)))
		h.Write([]byte{tag})
		h.Write(n[:])
		io.WriteString(h, s)
	}
	switch x := v.(type) {
	case string:
		writeIt('s', x)
	case json.Number:
		writeIt('n', string(x))
	case bool:
		writeIt('b', fmt.Sprint(x))
	case nil:
		h.Write([]byte{'z'})
	case []any:
		h.Write([]byte{'['})
		for _, e := range x {
			hashJSON(h, e)
		}
		h.Write([]byte{']'})
	case map[string]any:
		ks := make([]string, 0, len(x))
		for k := range x {
			if k != "cache_control" {
				ks = append(ks, k)
			}
		}
		sort.Strings(ks)
		h.Write([]byte{'{'})
		for _, k := range ks {
			writeIt('k', k)
			hashJSON(h, x[k])
		}
		h.Write([]byte{'}'})
	default:
		writeIt('?', fmt.Sprint(x))
	}
}

// unit: processes v as a block of the conversation, with the chained position. Only top-level
// blocks chain: the content of a tool_result is part of its block.
func (w walker) unit(v any, f func(w walker)) {
	if w.inside {
		f(w)
		return
	}
	var next mask.Position
	if w.col != nil || w.pos.i >= len(w.pos.seq) {
		next = after(w.pos.current, v)
		if w.col != nil {
			w.pos.seq = append(w.pos.seq, next)
		}
	} else {
		next = w.pos.seq[w.pos.i]
	}
	w.pos.i++
	w.pos.block, w.pos.n = next, 0
	w.inside = true
	f(w)
	w.pos.current = next
}

// keys: the keys of a JSON object in order (the order of a Go map is random, and the order of the
// texts of a block is part of the position of each one).
func keys(x map[string]any) []string {
	ks := make([]string, 0, len(x))
	for k := range x {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// walker walks the JSON of a request and masks the strings that may carry user data, keeping the
// pseudonym -> real entries used.
type walker struct {
	cfg config.Config
	// bridge: the request came from another format (Gemini, OpenAI) and is seen in the Anthropic
	// shape. Only turns on what belongs to those formats (bridgeUntouchableKeys, bridgeWebTools).
	bridge bool
	m      *mask.Masker // only for the translated words of a tool_use (commandHints)
	batch  *mask.Batch
	md     *Media
	ents   *[]mask.Entry
	err    *error // first error while handling media (the whole request is refused)
	// col != nil: only gathers the texts, without changing anything (1st pass)
	col *collection
	// webIDSet: tool_use_id of the WebFetch/WebSearch calls of this conversation
	webIDSet map[string]bool
	// fromWeb: the current text came from the internet (mask, but do not learn)
	fromWeb bool
	// hints: tool_use_id -> what the command of the call says about the result (mask.Commands.Hint)
	hints map[string]string
	// usageHints: tool_use id -> what is known about the call input (echo, translated words)
	usageHints map[string]string
	// hint: the one of the current result
	hint string
	pos  *position
	// inside: already inside a block (does not chain again)
	inside bool
	// assist: assistant message (text and tool input written by the model)
	assist bool
	// message: inside the content of a message (outside a tool result)
	message bool
	// prose: message text, from the user or the assistant (not tool input nor output), and client
	// text
	prose bool
	// client: system or tool definition, the text Claude Code writes (instructions, descriptions,
	// schema). It is masked like the others, but the common word the conversation learned as a name
	// stays a word (as in prose): "billing" is a table in the data and a word in the instruction.
	client bool
	// fixed: system text block, where the client's fixed text (clientFixedText) passes intact
	fixed bool
}

// media handles an image/PDF block; returns the blocks that replace it.
func (w walker) media(b map[string]any) []any {
	if w.col != nil {
		return []any{b}
	}
	if w.md == nil {
		*w.err = errNoMedia
		return nil
	}
	blocks, ents, err := w.md.Process(b)
	if err != nil {
		if *w.err == nil {
			*w.err = err
		}
		return nil
	}
	*w.ents = append(*w.ents, ents...)
	return blocks
}

var errNoMedia = errorString("tratamento de imagens/PDF não configurado")

type errorString string

func (e errorString) Error() string { return string(e) }

func isBase64(b map[string]any) bool {
	src, _ := b["source"].(map[string]any)
	return src != nil && src["type"] == "base64"
}

func (w walker) s(v string) string {
	if w.assist && !w.fromWeb {
		return w.written(v)
	}
	if w.fixed && clientFixedText(v) {
		w.pos.ofText() // not data nor a source of decisions
		return v
	}
	if f, _ := mask.SplitSource(w.hint); w.col != nil && f == mask.SystemSource {
		w.pos.ofText() // system catalog: not a source of decisions (mask/memory_tracing.go)
		return v
	}
	if w.col != nil {
		w.col.items = append(w.col.items, mask.BatchItem{S: v, FromWeb: w.fromWeb, Pos: w.pos.ofText(), Hint: w.hint})
		return v
	}
	maskIt := w.batch.MaskHint
	if w.prose {
		maskIt = w.batch.MaskHintProse
	}
	out, e := maskIt(v, w.fromWeb, w.pos.ofText(), w.hint)
	*w.ents = append(*w.ents, e...)
	return out
}

// written: assistant text. If the proxy unmasked it (who wrote it), it comes back as the API sent
// it; the translated words enter the conversation memory.
func (w walker) written(v string) string {
	if w.col != nil {
		if d, ok := w.batch.Written(v); ok {
			w.pos.ofText()
			w.col.extras = append(w.col.extras, d...)
			return v
		}
		if !w.fromWeb {
			w.pos.ofText() // the assistant text is not a source of decisions (mask/memory_tracing.go)
			return v
		}
		w.col.items = append(w.col.items, mask.BatchItem{S: v, FromWeb: w.fromWeb, Pos: w.pos.ofText(), Hint: w.hint})
		return v
	}
	maskIt := w.batch.MaskWritten
	if w.prose {
		maskIt = w.batch.MaskProse
	}
	out, e := maskIt(v, w.fromWeb, w.pos.ofText(), w.hint)
	*w.ents = append(*w.ents, e...)
	return out
}

// Client fixed text: the two system blocks Claude Code identifies itself to the API with. The API
// only accepts the request as Claude Code's with them intact; with one word changed, it answers 429
// to every message of the conversation. They carry no user data, and only pass with proof:
//
//   - the header ("x-anthropic-billing-header: cc_version=2.1.296.c17; cc_entrypoint=cli;") by
//     its exact shape: its name and short name=value pairs, without spaces or free text;
//   - the identity sentence by being, whole, one of the public sentences of Claude Code.
//
// The same text with anything more is a text like the others.
var (
	reClientHeader  = regexp.MustCompile(`^x-anthropic-billing-header:( ?[a-z][a-z0-9_]{1,23}=[A-Za-z0-9._-]{1,64};)+ ?$`)
	clientSentences = map[string]bool{
		"You are Claude Code, Anthropic's official CLI for Claude.":                                      true,
		"You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.": true,
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.":                                 true,
	}
)

func clientFixedText(v string) bool {
	return clientSentences[v] || len(v) <= 400 && reClientHeader.MatchString(v)
}

// web: the tools of the tools_without_unmask list (WebFetch, WebSearch) bring content from the
// internet. A documentation example (the example password in a URL) is not the user's secret: it is
// masked where it appears, but is not remembered. The claude.ai connectors (mcp__claude_ai_*) stay
// out: they bring the user's own data.
func (w walker) web(name string) bool {
	if w.bridge && bridgeWebTools[name] {
		return true
	}
	for _, f := range w.cfg.ToolsWithoutUnmask {
		if f == name {
			return true
		}
	}
	return false
}

// webIDs gathers the ids of the web tool calls made in the conversation and of the reads of public
// dependency files: their result is masked, but does not teach.
func (w walker) webIDs(msgs []any) map[string]bool {
	ids := map[string]bool{}
	for _, mm := range msgs {
		msg, _ := mm.(map[string]any)
		blocks, _ := msg["content"].([]any)
		for _, b := range blocks {
			bl, _ := b.(map[string]any)
			if bl == nil || bl["type"] != "tool_use" {
				continue
			}
			id, _ := bl["id"].(string)
			if name, _ := bl["name"].(string); w.web(name) && id != "" {
				ids[id] = true
			}
			// file read from a public dependency (site-packages/pandas/..., node_modules/
			// express/...): vendor code, masked but without teaching the memory (like the web)
			if in, _ := bl["input"].(map[string]any); in != nil && id != "" && w.m != nil {
				for _, k := range []string{"file_path", "path", "notebook_path"} {
					if p, _ := in[k].(string); p != "" && w.m.ThirdPartyPath(p) {
						ids[id] = true
					}
				}
			}
		}
	}
	return ids
}

// commandHints: for each tool result, what the command of the call (the tool_use with the same id)
// says about it: the columns of a SELECT, the type asked for in a listing, the column of a file
// whose header already went by, the program words (see mask/call_command.go and mask/call.go). And,
// for each tool_use, what is known about the input (echo of a previous output, words the proxy
// translated from a pseudonym). Computed in order, before masking (the tool_use input is still the
// original), and the same in both passes.
func commandHints(msgs []any, m *mask.Masker) (hints, usageHints map[string]string) {
	cs := mask.NewCommands()
	cmds := map[string]string{}
	chs := map[string]*mask.Call{}
	hints, usageHints = map[string]string{}, map[string]string{}
	blocksOf := func(mm any) (string, []any) {
		msg, _ := mm.(map[string]any)
		hostRole, _ := msg["role"].(string)
		if t, ok := msg["content"].(string); ok {
			return hostRole, []any{map[string]any{"type": "text", "text": t}}
		}
		blocks, _ := msg["content"].([]any)
		return hostRole, blocks
	}
	// the arguments of all calls (the echo looks only for them in the outputs)
	writtenSet := map[string]string{} // file the conversation wrote -> the sources it cites
	for _, mm := range msgs {
		_, blocks := blocksOf(mm)
		for _, b := range blocks {
			if bl, _ := b.(map[string]any); bl != nil && bl["type"] == "tool_use" {
				if id, _ := bl["id"].(string); id != "" {
					cmds[id] = commandOf(bl["input"])
					cs.ArgumentText(cmds[id])
					if in, _ := bl["input"].(map[string]any); in != nil && m != nil {
						if c, ok := in["command"].(string); ok {
							m.RecordCommand(c) // o programa rodado prova software (mask/memory_software.go)
						}
					}
				}
				noteWritten(writtenSet, bl["input"])
			}
		}
	}
	for _, mm := range msgs {
		hostRole, blocks := blocksOf(mm)
		for _, b := range blocks {
			bl, _ := b.(map[string]any)
			if bl == nil {
				continue
			}
			id, _ := bl["id"].(string)
			switch bl["type"] {
			case "text":
				if hostRole == "user" {
					if t, ok := bl["text"].(string); ok {
						cs.User(noReminders(t))
					}
				}
			case "tool_use":
				if id != "" {
					ch := cs.Usage(cmds[id], translatedIn(m, bl["input"]))
					chs[id] = ch
					if e := ch.UsageExt(); e != "" {
						usageHints[id] = mask.WithExtension("", e)
					}
				}
			case "tool_result":
				rid, _ := bl["tool_use_id"].(string)
				output := textOf(bl["content"])
				d := ""
				if cmd := cmds[rid]; cmd != "" {
					d = cs.Hint(cmd, output)
				}
				d = mask.WithExtension(d, cs.Result(chs[rid], output))
				if cmd := cmds[rid]; cmd != "" {
					d = mask.WithSource(sourceWith(writtenSet, cmd), d) // the source of the result
					if mask.SystemTarget(cmd) {
						d = mask.WithSource(mask.SystemSource, "")
					}
				}
				if d != "" {
					hints[rid] = d
				}
			}
		}
		if hostRole == "assistant" {
			cs.EndTurn()
		}
	}
	return hints, usageHints
}

// noReminders: the user text without the <system-reminder> blocks Claude Code adds (they are not
// the user's request).
func noReminders(t string) string {
	const ab, fe = "<system-reminder>", "</system-reminder>"
	if !strings.Contains(t, ab) {
		return t
	}
	var b strings.Builder
	for {
		i := strings.Index(t, ab)
		if i < 0 {
			b.WriteString(t)
			return b.String()
		}
		b.WriteString(t[:i])
		j := strings.Index(t[i:], fe)
		if j < 0 {
			return b.String()
		}
		t = t[i+j+len(fe):]
	}
}

// translatedIn: the words the proxy translated from a pseudonym in the strings of a tool_use input
// (see mask.Masker.RecordResponse).
func translatedIn(m *mask.Masker, v any) []mask.TranslatedWord {
	if m == nil {
		return nil
	}
	var out []mask.TranslatedWord
	var f func(v any)
	f = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, m.TranslatedWords(x)...)
		case []any:
			for _, e := range x {
				f(e)
			}
		case map[string]any:
			for _, k := range keys(x) {
				f(x[k])
			}
		}
	}
	f(v)
	return out
}

// commandOf: the text of a tool input that says what it does (the shell command, the query, the
// path). The free description stays out.
func commandOf(v any) string {
	in, _ := v.(map[string]any)
	if c, ok := in["command"].(string); ok {
		return c
	}
	var b strings.Builder
	for _, k := range keys(in) {
		if s, ok := in[k].(string); ok && k != "description" && len(s) <= 4<<10 {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// textOf: the text of a tool_result content (string or text blocks).
func textOf(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, x := range c {
			if bl, ok := x.(map[string]any); ok && bl["type"] == "text" {
				if t, ok := bl["text"].(string); ok {
					b.WriteString(t)
					b.WriteByte('\n')
				}
			}
		}
		return b.String()
	}
	return ""
}

// Keys that never carry user data (or that cannot change).
var untouchableKeys = map[string]bool{"type": true, "id": true, "tool_use_id": true, "signature": true,
	"media_type": true, "cache_control": true, "model": true, "role": true, "stop_reason": true,
	"stop_sequence": true, "citations": false}

// bridgeUntouchableKeys: those of untouchableKeys and, only in requests seen by a bridge (Gemini,
// OpenAI), the extra ones those formats have: the reasoning signature, the file type and the format
// of a schema. They never apply to the Anthropic API: there a "format" field in a tool input is
// still masked.
var bridgeUntouchableKeys = map[string]bool{"thoughtSignature": true, "thought_signature": true,
	"mimeType": true, "mime_type": true, "format": true}

// bridgeWebTools: the names Gemini CLI, opencode and Copilot give to the internet search and read
// tools (Claude Code's are in tools_without_unmask). They only apply in requests seen by a bridge.
var bridgeWebTools = map[string]bool{"web_fetch": true, "google_web_search": true, "webfetch": true, "websearch": true}

func (w walker) untouchable(k string) bool {
	return untouchableKeys[k] || w.bridge && bridgeUntouchableKeys[k]
}

// anthropicRequest: /v1/messages e /v1/messages/count_tokens.

func (w walker) anthropicRequest(v any) any {
	req, ok := v.(map[string]any)
	if !ok {
		return w.generic(v)
	}
	// fixed order (the order of a Go map is random): the same in which the API builds the cache
	ks := make([]string, 0, len(req))
	for k := range req {
		if k != "tools" && k != "system" && k != "messages" {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	ks = append([]string{"tools", "system", "messages"}, ks...)
	for _, k := range ks {
		val, ok := req[k]
		if !ok {
			continue
		}
		switch k {
		case "system":
			ws := w
			ws.client, ws.fixed = true, true
			req[k] = ws.content(val)
		case "messages":
			if msgs, ok := val.([]any); ok {
				w.webIDSet = w.webIDs(msgs)
				w.hints, w.usageHints = commandHints(msgs, w.m)
				for _, mm := range msgs {
					if msg, ok := mm.(map[string]any); ok {
						// who speaks is also part of the position
						w.pos.current = after(w.pos.current, msg["role"])
						wm := w
						wm.assist = msg["role"] == "assistant"
						wm.message = true
						msg["content"] = wm.content(msg["content"])
					}
				}
			}
		case "tools":
			// descriptions may cite data (e.g. MCP tool); the name cannot change
			if ts, ok := val.([]any); ok {
				for _, t := range ts {
					if tm, ok := t.(map[string]any); ok {
						w.unit(tm, func(w walker) {
							w.client, w.prose = true, true
							for _, tk := range keys(tm) {
								if tk != "name" && tk != "type" {
									tm[tk] = w.generic(tm[tk])
								}
							}
						})
					}
				}
			}
		case "model", "max_tokens", "stream", "temperature", "top_p", "top_k", "stop_sequences", "tool_choice",
			"thinking", "metadata", "service_tier", "context_management", "container", "output_config",
			"betas", "speed", "mcp_servers":
			// parameters and identifiers: carry no user data and cannot change
		default:
			// field I do not know: mask as a precaution (fail toward the safe side)
			w.unit(val, func(w walker) { req[k] = w.generic(val) })
		}
	}
	return req
}

// content: string or list of blocks.
func (w walker) content(v any) any {
	switch c := v.(type) {
	case string:
		var out string
		w.unit(c, func(w walker) {
			w.prose = w.message || w.client
			out = w.s(c)
		})
		return out
	case []any:
		// an image/PDF can become more than one block (covered image + note, pages)
		out := make([]any, 0, len(c))
		for _, b := range c {
			bl, ok := b.(map[string]any)
			if !ok {
				out = append(out, b)
				continue
			}
			w.unit(bl, func(w walker) {
				if (bl["type"] == "image" || bl["type"] == "document") && isBase64(bl) {
					out = append(out, w.media(bl)...)
					return
				}
				out = append(out, w.block(bl))
			})
		}
		return out
	}
	return v
}

func (w walker) block(b map[string]any) map[string]any {
	switch b["type"] {
	case "thinking", "redacted_thinking":
		// signed by the API: any change invalidates the conversation. The model only saw
		// pseudonyms, so its reasoning also only contains pseudonyms.
		return b
	case "image":
		return b // only gets here if it is not base64 (url/hosted file)
	case "server_tool_use", "web_search_tool_result", "web_fetch_tool_result", "code_execution_tool_result",
		"container_upload", "mcp_tool_use", "mcp_tool_result":
		return b // generated on the API side
	case "text":
		if s, ok := b["text"].(string); ok {
			w.prose = w.message || w.client
			b["text"] = w.s(s)
		}
		return b
	case "tool_use":
		// the WebFetch/WebSearch input is written by the model from what it read on the web
		if name, _ := b["name"].(string); w.web(name) {
			w.fromWeb = true
		}
		id, _ := b["id"].(string)
		w.hint = w.usageHints[id]
		b["input"] = w.all(b["input"])
		return b
	case "tool_result":
		id, _ := b["tool_use_id"].(string)
		if w.webIDSet[id] {
			w.fromWeb = true
		}
		w.hint = w.hints[id]
		w.message = false // tool output: data, not prose
		b["content"] = w.content(b["content"])
		return b
	case "document":
		if src, ok := b["source"].(map[string]any); ok {
			switch src["type"] {
			case "text":
				if s, ok := src["data"].(string); ok {
					src["data"] = w.s(s)
				}
			case "content":
				src["content"] = w.content(src["content"])
			}
		}
		for _, k := range []string{"title", "context"} {
			if s, ok := b[k].(string); ok {
				b[k] = w.s(s)
			}
		}
		return b
	}
	// unknown type: mask every string outside the untouchable keys
	return w.generic(b).(map[string]any)
}

// all masks all strings (tool input: command, path, content...).
func (w walker) all(v any) any {
	switch x := v.(type) {
	case string:
		return w.s(x)
	case []any:
		for i := range x {
			x[i] = w.all(x[i])
		}
	case map[string]any:
		for _, k := range keys(x) {
			x[k] = w.all(x[k])
		}
	}
	return v
}

// generic masks the strings of any JSON, except untouchable keys and reasoning blocks.
func (w walker) generic(v any) any {
	switch x := v.(type) {
	case string:
		return w.s(x)
	case []any:
		for i := range x {
			x[i] = w.generic(x[i])
		}
	case map[string]any:
		if t, _ := x["type"].(string); t == "thinking" || t == "redacted_thinking" || t == "image" {
			return x
		}
		for _, k := range keys(x) {
			if !w.untouchable(k) && k != "data" {
				x[k] = w.generic(x[k])
			}
		}
	}
	return v
}

// noteWritten: a file the conversation wrote (Write, Edit) cites the sources it reads; running it
// later has those sources too ("python3 gerar_tags.py" reads the catalogo.csv the script cites).
func noteWritten(writtenSet map[string]string, v any) {
	in, _ := v.(map[string]any)
	p, _ := in["file_path"].(string)
	if p == "" {
		return
	}
	var body string
	for _, k := range []string{"content", "new_string"} {
		if c, ok := in[k].(string); ok {
			body += c + "\n"
		}
	}
	if body == "" {
		return
	}
	name := strings.ToLower(p[strings.LastIndexAny(p, "/\\")+1:])
	if f := mask.CommandSource(body); f != "" && !strings.HasPrefix(f, "cmd:") {
		writtenSet[name] = f
	}
}

// sourceWith: the source of the command, with the sources inherited from the files the conversation
// wrote.
func sourceWith(writtenSet map[string]string, cmd string) string {
	f := mask.CommandSource(cmd)
	var extra []string
	for _, x := range strings.Split(f, ",") {
		if e := writtenSet[x]; e != "" {
			extra = append(extra, e)
		}
	}
	if len(extra) == 0 {
		return f
	}
	return f + "," + strings.Join(extra, ",")
}
