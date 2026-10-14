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

// Ida: mascara o corpo da requisição (formato da API da Anthropic).

// maskBody mascara o JSON da requisição. Corpo que não é JSON não sai (falha fechada).
// Devolve também o lote dos textos, para congelar depois que a requisição sair.
//
// formato: "anthropic" (/v1/messages); "gemini", "codeassist", "openai" ou "responses" (vista na
// forma da Anthropic por uma ponte: ver gemini.go, openai.go e responses.go); ou outro (modo
// genérico: todo texto é mascarado).
func (p *Proxy) maskBody(format, typ string, body []byte) ([]byte, []mask.Entry, *mask.Batch, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var original any
	if err := dec.Decode(&original); err != nil {
		return nil, nil, nil, fmt.Errorf("corpo não é JSON (%s)", typ)
	}
	anthropic := format == "anthropic"
	v, goBack := original, func() {}
	// outro formato com leitor próprio: a requisição é vista na forma da Anthropic (ponte)
	bridge := map[string]func(any) (map[string]any, func(), bool){"gemini": geminiBridge, "openai": openAIBridge,
		"codeassist": codeAssistBridge, "responses": responsesBridge}[format]
	withBridge := false
	if bridge != nil {
		if a, f, ok := bridge(original); ok {
			v, goBack, anthropic, withBridge = a, f, true, true
		}
	}
	batch := p.m.NewBatch()
	// 1ª passada: só junta os textos (os mesmos que a montagem vai mascarar) e os mascara
	// antes, em paralelo quando são muitos. Assim, todo valor aprendido nesta requisição já
	// vale quando ela é montada, e a requisição seguinte não muda nada do que esta enviou.
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
		// anterioridade (anterioridade.go), com o que cada saída de ferramenta decidiu (já no memo)
		batch.UsePublic(modelPublics(v, p.m, batch.DecidedByText(col.items)))
	}
	// memória da conversa: os nomes decididos em todos os textos valem para os textos ainda
	// não enviados (ver mask/memoria_conversa.go)
	batch.Memory(col.items, col.extras)

	// 2ª passada: monta, em ordem
	var ents []mask.Entry
	var errMedia error
	// as posições já foram calculadas na 1ª passada (os blocos ainda estavam intactos)
	w := walker{cfg: p.cfg, m: p.m, batch: batch, md: p.media, ents: &ents, err: &errMedia, pos: &position{seq: wc.pos.seq}, bridge: withBridge}
	if anthropic {
		v = w.anthropicRequest(v)
	} else {
		// fora de /v1/messages não há conversa: congela só o reenvio do corpo inteiro igual
		w.pos.block = after(mask.Position{}, v)
		v = w.generic(v)
	}
	if errMedia != nil {
		// imagem/PDF que não deu para verificar nunca sai, mesmo com falhar_fechado=false
		return nil, nil, nil, requiredError{errMedia}
	}
	if withBridge {
		goBack() // o que foi mascarado na forma da Anthropic volta para a requisição original
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

// coleta: os textos que a montagem vai mascarar, com a posição e a origem de cada um, e as
// decisões dos trechos traduzidos dos textos do assistente que voltam como a API os mandou.
type collection struct {
	items  []mask.BatchItem
	extras []mask.Decision
}

// posicao: onde o walker está na conversa. Cada bloco (ferramenta, bloco do system, bloco
// de mensagem) encadeia o hash do anterior, na ordem em que a API monta o cache: tools,
// system, messages. Os textos de um bloco têm a posição de tudo o que vem antes dele. Assim
// o congelamento (ver mask.Batch) vale só para o reenvio exato daquele ponto da conversa.
//
// A posição de um texto é o hash do seu bloco (que cobre tudo o que vem antes e o bloco
// inteiro) mais a ordem do texto dentro do bloco, num percurso em ordem fixa. Assim o texto
// de um bloco que já saiu é reconhecido sem calcular o hash dele de novo.
type position struct {
	current mask.Position
	// seq: a posição depois de cada bloco, na ordem; a 1ª passada calcula, a 2ª reaproveita
	seq []mask.Position
	i   int
	// bloco e n: o bloco atual e quantos textos dele já foram vistos
	block mask.Position
	n     uint64
}

// ofText: a posição do próximo texto do bloco atual.
func (p *position) ofText() mask.Position {
	var b [40]byte
	copy(b[:], p.block[:])
	binary.LittleEndian.PutUint64(b[32:], p.n)
	p.n++
	return sha256.Sum256(b[:])
}

// depois: a posição seguinte a um bloco (calculada antes de o bloco ser mascarado).
func after(prev mask.Position, v any) mask.Position {
	h := sha256.New()
	h.Write(prev[:])
	hashJSON(h, v)
	var out mask.Position
	copy(out[:], h.Sum(nil))
	return out
}

// hashJSON escreve v em h numa forma estável: chaves em ordem e sem cache_control (o
// marcador de cache muda de lugar a cada mensagem e não faz parte do conteúdo).
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

// unidade: processa v como um bloco da conversa, com a posição encadeada. Só os blocos de
// primeiro nível encadeiam: o conteúdo de um tool_result é parte do bloco dele.
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

// chaves: as chaves de um objeto JSON em ordem (a de um mapa em Go é aleatória, e a ordem
// dos textos de um bloco faz parte da posição de cada um).
func keys(x map[string]any) []string {
	ks := make([]string, 0, len(x))
	for k := range x {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// walker percorre o JSON de uma requisição e mascara as strings que podem levar
// dado do usuário, guardando as entradas pseudônimo -> real usadas.
type walker struct {
	cfg config.Config
	// ponte: a requisição veio de outro formato (Gemini, OpenAI) e é vista na forma da Anthropic.
	// Só liga o que é desses formatos (bridgeUntouchableKeys, bridgeWebTools).
	bridge bool
	m      *mask.Masker // só para as palavras traduzidas de um tool_use (commandHints)
	batch  *mask.Batch
	md     *Media
	ents   *[]mask.Entry
	err    *error // primeiro erro ao tratar mídia (a requisição inteira é recusada)
	// col != nil: só junta os textos, sem mudar nada (1ª passada)
	col *collection
	// webIDSet: tool_use_id das chamadas de WebFetch/WebSearch desta conversa
	webIDSet map[string]bool
	// fromWeb: o texto atual veio da internet (mascara, mas não aprende)
	fromWeb bool
	// dicas: tool_use_id -> o que o comando da chamada diz do resultado (mask.Commands.Hint)
	hints map[string]string
	// usageHints: tool_use id -> o que se sabe da entrada da chamada (eco, palavras traduzidas)
	usageHints map[string]string
	// dica: a do resultado atual
	hint string
	pos  *position
	// dentro: já dentro de um bloco (não encadeia de novo)
	inside bool
	// assist: mensagem do assistente (texto e entrada de ferramenta escritos pelo modelo)
	assist bool
	// mensagem: dentro do conteúdo de uma mensagem (fora de um resultado de ferramenta)
	message bool
	// prosa: texto da mensagem, do usuário ou do assistente (não entrada nem saída de ferramenta),
	// e texto do cliente
	prose bool
	// cliente: system ou definição de ferramenta, o texto que o Claude Code escreve (instruções,
	// descrições, schema). É mascarado como os outros, mas a palavra comum que a conversa
	// aprendeu como nome fica como palavra (como na prosa): "billing" é tabela no dado e palavra
	// na instrução.
	client bool
	// fixo: bloco de texto do system, onde o texto fixo do cliente (clientFixedText) passa
	// intacto
	fixed bool
}

// midia trata um bloco de imagem/PDF; devolve os blocos que o substituem.
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
		w.pos.ofText() // não é dado nem fonte de decisão
		return v
	}
	if f, _ := mask.SplitSource(w.hint); w.col != nil && f == mask.SystemSource {
		w.pos.ofText() // catálogo de sistema: não é fonte de decisão (mask/memoria_rastreamento.go)
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

// escrito: texto do assistente. Se o proxy o desmascarou (quem escreveu), volta como a API o
// mandou; as palavras traduzidas entram na memória da conversa.
func (w walker) written(v string) string {
	if w.col != nil {
		if d, ok := w.batch.Written(v); ok {
			w.pos.ofText()
			w.col.extras = append(w.col.extras, d...)
			return v
		}
		if !w.fromWeb {
			w.pos.ofText() // o texto do assistente não é fonte de decisão (mask/memoria_rastreamento.go)
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

// Text fixo do cliente: os dois blocos do system com que o Claude Code se identifica à API. A
// API só aceita a requisição como do Claude Code com eles intactos; com uma palavra trocada, ela
// responde 429 a toda mensagem da conversa. Não levam dado do usuário, e só passam com prova:
//
//   - o cabeçalho ("x-anthropic-billing-header: cc_version=2.1.296.c17; cc_entrypoint=cli;") pela
//     forma exata: o nome dele e pares nome=valor curtos, sem espaço nem texto livre;
//   - a frase de identidade por ser, inteira, uma das frases públicas do Claude Code.
//
// O mesmo texto com qualquer coisa a mais é um texto como os outros.
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

// web: as ferramentas da lista ferramentas_sem_desmascarar (WebFetch, WebSearch) trazem
// conteúdo da internet. Um exemplo de documentação (a senha de exemplo numa URL) não é
// segredo do usuário: é mascarado onde aparece, mas não é lembrado. Os conectores do
// claude.ai (mcp__claude_ai_*) ficam de fora: trazem dado do próprio usuário.
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

// webIDs junta os ids das chamadas de ferramentas da web feitas na conversa e das leituras de
// arquivo de dependência pública: o resultado delas é mascarado, mas não ensina.
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
			// arquivo lido de uma dependência pública (site-packages/pandas/..., node_modules/
			// express/...): código do fornecedor, mascarado mas sem ensinar a memória (como a web)
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

// commandHints: para cada resultado de ferramenta, o que o comando da chamada (o tool_use
// do mesmo id) diz dele: as colunas de um SELECT, o tipo pedido numa listagem, a coluna de um
// arquivo cujo cabeçalho já passou, as palavras do programa (ver mask/chamada_comando.go e
// mask/chamada.go). E, para cada tool_use, o que se sabe da entrada (eco de uma saída anterior,
// palavras que o proxy traduziu de um pseudônimo). Calculado em ordem, antes de mascarar (a
// entrada do tool_use ainda é a original), e igual nas duas passadas.
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
	// os argumentos de todas as chamadas (o eco procura só por eles nas saídas)
	writtenSet := map[string]string{} // arquivo que a conversa escreveu -> as fontes que ele cita
	for _, mm := range msgs {
		_, blocks := blocksOf(mm)
		for _, b := range blocks {
			if bl, _ := b.(map[string]any); bl != nil && bl["type"] == "tool_use" {
				if id, _ := bl["id"].(string); id != "" {
					cmds[id] = commandOf(bl["input"])
					cs.ArgumentText(cmds[id])
					if in, _ := bl["input"].(map[string]any); in != nil && m != nil {
						if c, ok := in["command"].(string); ok {
							m.RecordCommand(c) // o programa rodado prova software (mask/memoria_software.go)
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
					d = mask.WithSource(sourceWith(writtenSet, cmd), d) // a fonte do resultado
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

// noReminders: o texto do usuário sem os blocos <system-reminder> que o Claude Code acrescenta
// (não são o pedido do usuário).
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

// translatedIn: as palavras que o proxy traduziu de um pseudônimo nas strings da entrada de um
// tool_use (ver mask.Masker.RecordResponse).
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

// commandOf: o texto da entrada de uma ferramenta que diz o que ela faz (o comando do shell,
// a consulta, o caminho). A descrição livre fica de fora.
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

// textOf: o texto de um conteúdo de tool_result (string ou blocos de texto).
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

// Chaves que nunca carregam dado do usuário (ou que não podem mudar).
var untouchableKeys = map[string]bool{"type": true, "id": true, "tool_use_id": true, "signature": true,
	"media_type": true, "cache_control": true, "model": true, "role": true, "stop_reason": true,
	"stop_sequence": true, "citations": false}

// bridgeUntouchableKeys: as de untouchableKeys e, só nas requisições vistas por uma ponte
// (Gemini, OpenAI), as que esses formatos têm a mais: a assinatura do raciocínio, o tipo do
// arquivo e o formato de um schema. Nunca valem para a API da Anthropic: lá um campo "format"
// numa entrada de ferramenta continua sendo mascarado.
var bridgeUntouchableKeys = map[string]bool{"thoughtSignature": true, "thought_signature": true,
	"mimeType": true, "mime_type": true, "format": true}

// bridgeWebTools: os nomes que o Gemini CLI, o opencode e o Copilot dão às ferramentas de
// busca e leitura na internet (as do Claude Code estão em ferramentas_sem_desmascarar). Só valem
// nas requisições vistas por uma ponte.
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
	// ordem fixa (a de um mapa em Go é aleatória): a mesma em que a API monta o cache
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
						// quem fala também faz parte da posição
						w.pos.current = after(w.pos.current, msg["role"])
						wm := w
						wm.assist = msg["role"] == "assistant"
						wm.message = true
						msg["content"] = wm.content(msg["content"])
					}
				}
			}
		case "tools":
			// descrições podem citar dado (ex.: ferramenta de MCP); o nome não pode mudar
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
			// parâmetros e identificadores: não carregam dado do usuário e não podem mudar
		default:
			// campo que não conheço: mascara por precaução (falha para o lado seguro)
			w.unit(val, func(w walker) { req[k] = w.generic(val) })
		}
	}
	return req
}

// conteudo: string ou lista de blocos.
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
		// uma imagem/PDF pode virar mais de um bloco (imagem coberta + nota, páginas)
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
			w.prose = w.message || w.client
			b["text"] = w.s(s)
		}
		return b
	case "tool_use":
		// a entrada de WebFetch/WebSearch é escrita pelo modelo a partir do que leu na web
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
		w.message = false // saída de ferramenta: dado, não prosa
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
	// tipo desconhecido: mascara toda string fora das chaves intocáveis
	return w.generic(b).(map[string]any)
}

// tudo mascara todas as strings (entrada de ferramenta: comando, caminho, conteúdo...).
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

// generico mascara strings de qualquer JSON, exceto chaves intocáveis e blocos de raciocínio.
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

// noteWritten: um arquivo que a conversa escreveu (Write, Edit) cita as fontes que lê; rodá-lo
// depois tem essas fontes também ("python3 gerar_tags.py" lê o catalogo.csv que o script cita).
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

// sourceWith: a fonte do comando, com as fontes herdadas dos arquivos que a conversa escreveu.
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
