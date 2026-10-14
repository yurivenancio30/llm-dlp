package main

// Commands to inspect llm-dlp: "last" (what the API received in the last message) and "simulate"
// (replays an old Claude Code session through llm-dlp, without talking to the API).

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
)

// last shows the last messages of the last request, as they went to the API. It is for checking,
// during use, what went out masked.
func lastConv(args []string) error {
	n := 2
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			n = v
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// the conversation, even masked, only goes out to whoever reads the key (proxy/local.go)
	key, err := mask.Key(config.Path("chave"))
	if err != nil {
		return err
	}
	request, _ := http.NewRequest("GET", "http://"+address(cfg)+"/__llm-dlp/ultima", nil)
	request.Header.Set(proxy.ProofHeader, proxy.Proof(key, "ultima"))
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("o llm-dlp não está no ar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return errors.New("o llm-dlp no ar não aceitou a prova: ele é de outra versão ou usa outra chave (rode llm-dlp status)")
	}
	body, _ := io.ReadAll(resp.Body)
	msgs := lastMessages(body)
	if len(msgs) == 0 {
		fmt.Println("Ainda não passou nenhuma mensagem desde que o llm-dlp subiu.")
		return nil
	}
	if n > len(msgs) {
		n = len(msgs)
	}
	fmt.Printf("Últimas %d de %d mensagens, como a API recebeu (os pseudônimos estão no lugar dos valores reais):\n", n, len(msgs))
	for _, m := range msgs[len(msgs)-n:] {
		for _, t := range m.texts {
			if len(t) > 3000 {
				t = t[:3000] + fmt.Sprintf("\n… (mais %d caracteres)", len(t)-3000)
			}
			fmt.Printf("\n── %s ──\n%s\n", m.hostRole, t)
		}
	}
	return nil
}

// seenMessage: one message of the last request, with the texts "llm-dlp last" shows.
type seenMessage struct {
	hostRole string
	texts    []string
}

// lastMessages reads the messages of the last masked request, in the Anthropic format (messages) or
// the Gemini one (contents).
func lastMessages(body []byte) []seenMessage {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
			// OpenAI format: tool calls are outside the content
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Contents []struct {
			Role  string `json:"role"`
			Parts []any  `json:"parts"`
		} `json:"contents"`
		// Gemini CLI with the Google account: the Gemini conversation goes inside "request"
		Request struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []any  `json:"parts"`
			} `json:"contents"`
		} `json:"request"`
		// Responses (Codex): instructions and items
		Instructions string `json:"instructions"`
		Input        any    `json:"input"`
	}
	if json.Unmarshal(body, &req) != nil {
		return nil
	}
	var out []seenMessage
	for _, m := range req.Messages {
		ts := contentTexts(m.Content)
		for _, tc := range m.ToolCalls {
			ts = append(ts, fmt.Sprintf("[ferramenta %s] %s", tc.Function.Name, tc.Function.Arguments))
		}
		out = append(out, seenMessage{m.Role, ts})
	}
	for _, c := range append(req.Contents, req.Request.Contents...) {
		out = append(out, seenMessage{c.Role, partTexts(c.Parts)})
	}
	if req.Instructions != "" {
		out = append(out, seenMessage{"instructions", []string{req.Instructions}})
	}
	return append(out, responsesItems(req.Input)...)
}

// responsesItems: the messages of the Responses "input" (text, tool call and tool result).
func responsesItems(entry any) []seenMessage {
	if s, ok := entry.(string); ok {
		return []seenMessage{{"user", []string{s}}}
	}
	items, _ := entry.([]any)
	texts := func(c any) []string {
		if s, ok := c.(string); ok {
			return []string{s}
		}
		var out []string
		parts, _ := c.([]any)
		for _, x := range parts {
			pm, _ := x.(map[string]any)
			if t, ok := pm["text"].(string); ok {
				out = append(out, t)
			} else if pm != nil {
				out = append(out, "[arquivo]")
			}
		}
		return out
	}
	var out []seenMessage
	for _, x := range items {
		it, _ := x.(map[string]any)
		hostRole, _ := it["role"].(string)
		switch it["type"] {
		case "message", nil:
			out = append(out, seenMessage{hostRole, texts(it["content"])})
		case "function_call":
			out = append(out, seenMessage{"assistant", []string{fmt.Sprintf("[ferramenta %v] %v", it["name"], it["arguments"])}})
		case "custom_tool_call":
			out = append(out, seenMessage{"assistant", []string{fmt.Sprintf("[ferramenta %v] %v", it["name"], it["input"])}})
		case "function_call_output", "custom_tool_call_output":
			var ts []string
			for _, t := range texts(it["output"]) {
				ts = append(ts, "[resultado de ferramenta]\n"+t)
			}
			out = append(out, seenMessage{"user", ts})
		}
	}
	return out
}

// partTexts returns the texts of a Gemini message (text, tool call and tool result).
func partTexts(parts []any) []string {
	var out []string
	for _, p := range parts {
		pt, _ := p.(map[string]any)
		if t, _ := pt["text"].(string); t != "" {
			out = append(out, t)
		}
		if fc, _ := pt["functionCall"].(map[string]any); fc != nil {
			b, _ := json.Marshal(fc["args"])
			out = append(out, fmt.Sprintf("[ferramenta %v] %s", fc["name"], b))
		}
		if fr, _ := pt["functionResponse"].(map[string]any); fr != nil {
			out = append(out, "[resultado de ferramenta]\n"+responseText(fr["response"]))
		}
		for _, k := range []string{"inlineData", "inline_data", "fileData", "file_data"} {
			if d, _ := pt[k].(map[string]any); d != nil {
				out = append(out, "[arquivo]")
			}
		}
	}
	return out
}

// responseText: the result of a Gemini tool. Texts come out as text (one per line, with the field
// name when there is more than one); the rest, as JSON.
func responseText(r any) string {
	m, _ := r.(map[string]any)
	var names []string
	for k, v := range m {
		if _, ok := v.(string); !ok {
			b, _ := json.Marshal(r)
			return string(b)
		}
		names = append(names, k)
	}
	if len(names) == 0 {
		b, _ := json.Marshal(r)
		return string(b)
	}
	sort.Strings(names)
	var lines []string
	for _, k := range names {
		if len(names) == 1 {
			return m[k].(string)
		}
		lines = append(lines, k+": "+m[k].(string))
	}
	return strings.Join(lines, "\n")
}

// contentTexts returns the texts of a message (text, tool result and tool input).
func contentTexts(c any) []string {
	switch x := c.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, b := range x {
			bl, _ := b.(map[string]any)
			switch bl["type"] {
			case "text":
				if t, _ := bl["text"].(string); t != "" {
					out = append(out, t)
				}
			case "tool_result":
				for _, t := range contentTexts(bl["content"]) {
					out = append(out, "[resultado de ferramenta]\n"+t)
				}
			case "tool_use":
				b, _ := json.Marshal(bl["input"])
				out = append(out, fmt.Sprintf("[ferramenta %v] %s", bl["name"], b))
			case "image", "document":
				out = append(out, fmt.Sprintf("[%v]", bl["type"]))
			}
		}
		return out
	}
	return nil
}

// simulate replays an old Claude Code session: it rebuilds the requests Claude Code would have
// made, passes each one through llm-dlp and delivers them to a local fake API, which only returns
// the last text it received. Nothing goes to the internet. It checks, on every request: the proxy
// accepted it; no value it detected itself was left in another part of the request; the response
// came back equal to the original text; and the old messages went out identical to those of the
// previous request (the API cache depends on it). It only prints counts.
func simulate(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	max := fs.Int("max", 400, "número máximo de requisições por sessão")
	window := fs.Int("window", 600, "tamanho máximo da conversa, em KB (como o limite de contexto)")
	fs.IntVar(window, "janela", 600, "nome antigo de --window")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("uso: llm-dlp simulate [--max N] [--window KB] SESSAO.jsonl...")
	}
	cfg, key, _, _, _, err := loadAll()
	if err != nil {
		return err
	}
	for _, file := range fs.Args() {
		// a copy of the people registry and no hash file: the simulation changes nothing
		tmp, err := os.MkdirTemp("", "llm-dlp-simular-")
		if err != nil {
			return err
		}
		if b, err := os.ReadFile(config.Path("pessoas.json")); err == nil {
			os.WriteFile(tmp+"/pessoas.json", b, 0o600)
		}
		ps, _ := mask.LoadPeople(tmp + "/pessoas.json")
		m, err := mask.NewMasker(cfg, key, ps, nil)
		if err != nil {
			os.RemoveAll(tmp)
			return err
		}
		err = simulateSession(file, cfg, m, *max, *window<<10)
		os.RemoveAll(tmp)
		if err != nil {
			return err
		}
	}
	return nil
}

func simulateSession(file string, cfg config.Config, m *mask.Masker, limitN, window int) error {
	// the fake API: keeps the body it received and returns, in pieces, the last text
	var received []byte
	cutEcho := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		cutEcho = false
		var req struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(received, &req)
		echo := ""
		if n := len(req.Messages); n > 0 {
			if ts := contentTexts(req.Messages[n-1].Content); len(ts) > 0 {
				echo = ts[len(ts)-1]
			}
		}
		if len(echo) > 4000 {
			cutEcho = true
			echo = echo[:4000]
			for len(echo) > 0 && echo[len(echo)-1]&0xC0 == 0x80 {
				echo = echo[:len(echo)-1]
			}
		}
		w.Header().Set("content-type", "text/event-stream")
		ev := func(name string, v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
		}
		ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		rs := []rune(echo)
		for i := 0; i < len(rs); i += 9 {
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0,
				"delta": map[string]any{"type": "text_delta", "text": string(rs[i:min(i+9, len(rs))])}})
		}
		ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		// the writing: the conversation's last tool call comes back as if the model had written it
		// now, with the pseudonyms it received, in pieces (as the API sends it)
		var contentList []any
		for _, m := range req.Messages {
			contentList = append(contentList, m.Content)
		}
		if usage := lastUse(contentList); usage != nil {
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": 1,
				"content_block": map[string]any{"type": "tool_use", "id": "toolu_simulacao", "name": usage["name"], "input": map[string]any{}}})
			var in bytes.Buffer
			enc := json.NewEncoder(&in)
			enc.SetEscapeHTML(false)
			enc.Encode(usage["input"])
			rs := []rune(strings.TrimRight(in.String(), "\n"))
			for i := 0; i < len(rs); i += 11 {
				ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": string(rs[i:min(i+11, len(rs))])}})
			}
			ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})
		}
	}))
	defer api.Close()
	cfg.Upstream = api.URL
	px := httptest.NewServer(proxy.New(cfg, m, nil, log.New(io.Discard, "", 0)))
	defer px.Close()

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 512<<20)

	type msg struct {
		id   int
		raw  map[string]any
		size int
	}
	var conversation []msg
	total, next := 0, 0
	previousOnes := map[int][32]byte{} // message id -> how it went out masked the last time
	var (
		reqs, refused, errs, roundTripDiff, withLeftover, leftovers, cacheChanged, images int
		timings                                                                           []time.Duration
		reasons                                                                           = map[string]int{}
		leftoverTypes                                                                     = map[string]int{}
	)
	debug := map[string]int{}
	var (
		writes, maskedWrites, writesDiff, writesWithLeftover, webWrites int
		diffByTool                                                      = map[string]int{}
	)
	// a subagent's conversation: in the session file, it is marked as a side chain (isSidechain)
	// and is skipped; in the subagent's own file (subagents/agent-*.jsonl), all of it is a side
	// chain and it is what repeats. The first message of the file decides.
	lateral, firstOne := false, true
	for sc.Scan() && reqs < limitN {
		var e struct {
			Type        string         `json:"type"`
			IsSidechain bool           `json:"isSidechain"`
			Message     map[string]any `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || (e.Type != "user" && e.Type != "assistant") || e.Message == nil {
			continue
		}
		if firstOne {
			lateral, firstOne = e.IsSidechain, false
		}
		if e.IsSidechain != lateral {
			continue
		}
		role, _ := e.Message["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}
		mm := map[string]any{"role": role, "content": e.Message["content"]}
		b, _ := json.Marshal(mm)
		conversation = append(conversation, msg{next, mm, len(b)})
		next++
		total += len(b)
		for total > window && len(conversation) > 1 {
			total -= conversation[0].size
			conversation = conversation[1:]
		}
		if role != "user" {
			continue
		}
		// builds and sends the request, as Claude Code would at this point
		msgs := make([]any, len(conversation))
		for i, c := range conversation {
			msgs[i] = c.raw
		}
		body, _ := json.Marshal(map[string]any{"model": "simulacao", "stream": true, "max_tokens": 1000, "messages": msgs})
		images += bytes.Count(body, []byte(`"type":"image"`))
		expected := ""
		if ts := contentTexts(mm["content"]); len(ts) > 0 {
			expected = ts[len(ts)-1]
		}
		received = nil
		t0 := time.Now()
		resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(body))
		if err != nil {
			errs++
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		response := readSSEText(bytes.NewReader(respBody))
		timings = append(timings, time.Since(t0))
		reqs++
		if resp.StatusCode != 200 {
			refused++
			var er struct {
				Error struct{ Message string } `json:"error"`
			}
			json.Unmarshal([]byte(response), &er)
			rsn := "outro motivo"
			switch {
			case strings.Contains(response, "OCR") || strings.Contains(response, "image") || strings.Contains(response, "verificar"):
				rsn = "imagem ou PDF que não deu para verificar (falta o OCR)"
			}
			reasons[rsn]++
			continue
		}
		// 1) round trip. The cut echo (the masked text went over 4000 bytes, even with the
		// original below that: the pseudonym can be longer than the value) cannot be compared.
		if expected != "" && !cutEcho && response != expected {
			roundTripDiff++
		}
		// 1b) the writing: did the tool call that "the model wrote" with pseudonyms come back equal
		// to the original? It is what Claude Code writes to the file or runs in the terminal.
		var contentList []any
		for _, c := range conversation {
			contentList = append(contentList, c.raw["content"])
		}
		if usage := lastUse(contentList); usage != nil {
			name, _ := usage["name"].(string)
			cameBack, ok := responseEntry(respBody)
			switch {
			case cfg.NoUnmask(name):
				webWrites++ // goes to the internet: keeps the pseudonyms, on purpose
			case ok:
				writes++
				orig, _ := json.Marshal(usage["input"])
				var env struct {
					Messages []struct {
						Content any `json:"content"`
					} `json:"messages"`
				}
				json.Unmarshal(received, &env)
				var sentSet []any
				for _, m := range env.Messages {
					sentSet = append(sentSet, m.Content)
				}
				if e := lastUse(sentSet); e != nil {
					if b, _ := json.Marshal(e["input"]); !bytes.Equal(b, orig) {
						maskedWrites++
					}
				}
				var a, b any
				json.Unmarshal(orig, &a)
				json.Unmarshal(cameBack, &b)
				if !reflect.DeepEqual(a, b) {
					writesDiff++
					diffByTool[name]++
					if len(rePseudoShape.FindAll(cameBack, -1)) > len(rePseudoShape.FindAll(orig, -1)) {
						writesWithLeftover++
					}
				}
			}
		}
		// 2) was something llm-dlp detected itself left in another part of what was sent?
		var env struct {
			Messages []json.RawMessage `json:"messages"`
		}
		json.Unmarshal(received, &env)
		sent := noReasoning(env.Messages)
		reals := map[string]string{}
		for _, t := range allTexts(msgs) {
			if os.Getenv("LLM_DLP_DEPURAR") == "2" {
				for _, a := range m.Detect(t) {
					if a.Type == "usuario" || a.Type == "nome" {
						i0 := max(0, a.Start-40)
						for i0 < a.Start && t[i0]&0xC0 == 0x80 {
							i0++
						}
						before := strings.ToLower(strings.ReplaceAll(t[i0:a.Start], "\n", "⏎"))
						var sb strings.Builder
						for _, r := range before {
							if r >= '0' && r <= '9' {
								sb.WriteByte('9')
							} else {
								sb.WriteRune(r)
							}
						}
						debug["ORIGEM "+a.Type+" | antes "+sb.String()]++
					}
				}
			}
			_, ents := m.Mask(t)
			for _, en := range ents {
				if len(en.Real) >= 6 {
					reals[en.Real] = en.Type
				}
			}
		}
		foundIt := false
		for real, typ := range reals {
			if strings.Contains(sent, real) {
				if os.Getenv("LLM_DLP_DEPURAR") != "" {
					i := strings.Index(sent, real)
					a, b := max(0, i-24), min(len(sent), i+len(real)+12)
					shape := func(t string) string {
						var sb strings.Builder
						for _, r := range t {
							switch {
							case r >= '0' && r <= '9':
								sb.WriteByte('9')
							case r >= 'A' && r <= 'Z':
								sb.WriteByte('A')
							case r >= 'a' && r <= 'z', r > 127:
								sb.WriteByte('a')
							default:
								sb.WriteRune(r)
							}
						}
						return sb.String()
					}
					debug[typ+" | valor "+shape(real)+" | antes "+strings.ToLower(sent[a:i])+"▮"+shape(sent[i+len(real):b])]++
				}
				leftovers++
				leftoverTypes[typ]++
				foundIt = true
			}
		}
		if foundIt {
			withLeftover++
		}
		// 3) did the old messages go out equal to those of the previous request?
		if len(env.Messages) == len(conversation) {
			changed := false
			for i, c := range conversation {
				h := sha256.Sum256(env.Messages[i])
				if prev, ok := previousOnes[c.id]; ok && prev != h {
					changed = true
				}
				previousOnes[c.id] = h
			}
			if changed {
				cacheChanged++
			}
		}
	}
	if len(debug) > 0 {
		var ks []string
		for k := range debug {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool { return debug[ks[i]] > debug[ks[j]] })
		for i, k := range ks {
			if i < 25 {
				fmt.Printf("    DEP %5d  %s\n", debug[k], strings.ReplaceAll(k, "\n", "⏎"))
			}
		}
	}
	sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
	q := func(p float64) time.Duration {
		if len(timings) == 0 {
			return 0
		}
		return timings[int(float64(len(timings)-1)*p)].Round(time.Millisecond)
	}
	name := file
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		name = file[i+1:]
	}
	fmt.Printf("### %.8s: %d requisições simuladas (conversa de até %d KB), %d com imagem\n", name, reqs, window>>10, images)
	if reqs == 0 {
		fmt.Println("    ATENÇÃO: nenhuma requisição repetida: o arquivo não tem mensagem do usuário que o simular leia. Nada foi conferido.")
	}
	fmt.Printf("    aceitas: %d | recusadas pelo llm-dlp: %d %v | erros de conexão: %d\n", reqs-refused, refused, reasons, errs)
	fmt.Printf("    resposta voltou diferente do original: %d\n", roundTripDiff)
	fmt.Printf("    escrita (chamada de ferramenta que o modelo escreve com pseudônimos): %d conferidas, %d tinham valor mascarado\n", writes, maskedWrites)
	fmt.Printf("    escrita que voltou diferente da original: %d %v, das quais %d com pseudônimo sobrando | para a internet (ficam mascaradas): %d\n",
		writesDiff, diffByTool, writesWithLeftover, webWrites)
	fmt.Printf("    valor detectado que sobrou em outro ponto da requisição: %d ocorrências em %d requisições %v\n", leftovers, withLeftover, leftoverTypes)
	fmt.Printf("    requisições em que uma mensagem antiga mudou (cache refeito): %d\n", cacheChanged)
	fmt.Printf("    tempo por requisição (ida, API falsa e volta): mediana %s | 95%% abaixo de %s | máximo %s\n", q(.5), q(.95), q(1))
	return nil
}

// rePseudoShape: what has the shape of an llm-dlp pseudonym (to count what was left).
var rePseudoShape = regexp.MustCompile(`p\.[a-z2-7]{8}@d[a-z2-7]{4}\.invalid|\bh[a-z2-7]{8}\.invalid|\b[a-z]{1,5}_[a-z2-7]{8}\b|\b(?:[A-Z]{2,12}|[a-z]{2,12}|[A-Z][a-z]{1,11})-[a-z2-7]{8}\b|[Pp]essoa [a-z2-7]{8}|PESSOA [A-Z2-7]{8}`)

// lastUse: the last tool call (tool_use) in the message contents.
func lastUse(contentList []any) map[string]any {
	for i := len(contentList) - 1; i >= 0; i-- {
		blocks, _ := contentList[i].([]any)
		for j := len(blocks) - 1; j >= 0; j-- {
			if b, _ := blocks[j].(map[string]any); b != nil && b["type"] == "tool_use" {
				return b
			}
		}
	}
	return nil
}

// responseEntry joins the input of the tool call of a streaming response.
func responseEntry(b []byte) ([]byte, bool) {
	var out bytes.Buffer
	foundIt := false
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "data: ") {
			continue
		}
		var ev struct {
			Type  string
			Delta struct {
				Type        string
				PartialJSON string `json:"partial_json"`
			}
		}
		json.Unmarshal([]byte(l[6:]), &ev)
		if ev.Delta.Type == "input_json_delta" {
			foundIt = true
			out.WriteString(ev.Delta.PartialJSON)
		}
	}
	return out.Bytes(), foundIt
}

// readSSEText joins the text of a streaming response (or returns the body, if it is an error).
func readSSEText(body io.Reader) string {
	b, _ := io.ReadAll(body)
	if !bytes.Contains(b, []byte("data: ")) {
		return string(b)
	}
	var out strings.Builder
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "data: ") {
			continue
		}
		var ev struct {
			Delta struct{ Type, Text string }
		}
		json.Unmarshal([]byte(l[6:]), &ev)
		if ev.Delta.Type == "text_delta" {
			out.WriteString(ev.Delta.Text)
		}
	}
	return out.String()
}

// noReasoning returns the messages as text, without the reasoning blocks (which by API rule cannot
// be changed; in a session that already went through llm-dlp they only have pseudonyms).
func noReasoning(msgs []json.RawMessage) string {
	var b strings.Builder
	var strip func(v any) any
	strip = func(v any) any {
		switch x := v.(type) {
		case []any:
			out := x[:0:0]
			for _, e := range x {
				if mm, ok := e.(map[string]any); ok && (mm["type"] == "thinking" || mm["type"] == "redacted_thinking") {
					continue
				}
				out = append(out, strip(e))
			}
			return out
		case map[string]any:
			for k, e := range x {
				x[k] = strip(e)
			}
		}
		return v
	}
	for _, raw := range msgs {
		var v any
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.Encode(strip(v))
	}
	return b.String()
}

// allTexts joins the texts of all messages (except reasoning and images).
func allTexts(msgs []any) []string {
	var out []string
	var rec func(v any)
	rec = func(v any) {
		switch x := v.(type) {
		case string:
			if len(x) >= 4 {
				out = append(out, x)
			}
		case []any:
			for _, e := range x {
				rec(e)
			}
		case map[string]any:
			if t := x["type"]; t == "thinking" || t == "redacted_thinking" || t == "image" {
				return
			}
			for k, e := range x {
				if k != "type" && k != "id" && k != "tool_use_id" && k != "role" && k != "name" && k != "signature" {
					rec(e)
				}
			}
		}
	}
	for _, mm := range msgs {
		rec(mm)
	}
	return out
}
