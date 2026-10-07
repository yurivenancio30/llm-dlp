package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"sort"
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
	wc := walker{cfg: p.cfg, m: p.m, col: col, lote: lote, pos: &posicao{}}
	if anthropic {
		wc.requisicaoAnthropic(v)
	} else {
		wc.pos.bloco = depois(mask.Posicao{}, v)
		wc.generico(v)
	}
	if anthropic {
		lote.UsarPublicos(publicosDoModelo(v, p.m)) // anterioridade (anterioridade.go)
	}
	lote.Aquecer(col.itens)
	// memória da conversa: os nomes decididos em todos os textos valem para os textos ainda
	// não enviados (ver mask/memoria.go)
	lote.Memoria(col.itens, col.extras)

	// 2ª passada: monta, em ordem
	var ents []mask.Entrada
	var errMidia error
	// as posições já foram calculadas na 1ª passada (os blocos ainda estavam intactos)
	w := walker{cfg: p.cfg, m: p.m, lote: lote, md: p.midia, ents: &ents, err: &errMidia, pos: &posicao{seq: wc.pos.seq}}
	if anthropic {
		v = w.requisicaoAnthropic(v)
	} else {
		// fora de /v1/messages não há conversa: congela só o reenvio do corpo inteiro igual
		w.pos.bloco = depois(mask.Posicao{}, v)
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

// coleta: os textos que a montagem vai mascarar, com a posição e a origem de cada um, e as
// decisões dos trechos traduzidos dos textos do assistente que voltam como a API os mandou.
type coleta struct {
	itens  []mask.ItemLote
	extras []mask.Decisao
}

// posicao: onde o walker está na conversa. Cada bloco (ferramenta, bloco do system, bloco
// de mensagem) encadeia o hash do anterior, na ordem em que a API monta o cache: tools,
// system, messages. Os textos de um bloco têm a posição de tudo o que vem antes dele. Assim
// o congelamento (ver mask.Lote) vale só para o reenvio exato daquele ponto da conversa.
//
// A posição de um texto é o hash do seu bloco (que cobre tudo o que vem antes e o bloco
// inteiro) mais a ordem do texto dentro do bloco, num percurso em ordem fixa. Assim o texto
// de um bloco que já saiu é reconhecido sem calcular o hash dele de novo.
type posicao struct {
	atual mask.Posicao
	// seq: a posição depois de cada bloco, na ordem; a 1ª passada calcula, a 2ª reaproveita
	seq []mask.Posicao
	i   int
	// bloco e n: o bloco atual e quantos textos dele já foram vistos
	bloco mask.Posicao
	n     uint64
}

// doTexto: a posição do próximo texto do bloco atual.
func (p *posicao) doTexto() mask.Posicao {
	var b [40]byte
	copy(b[:], p.bloco[:])
	binary.LittleEndian.PutUint64(b[32:], p.n)
	p.n++
	return sha256.Sum256(b[:])
}

// depois: a posição seguinte a um bloco (calculada antes de o bloco ser mascarado).
func depois(ant mask.Posicao, v any) mask.Posicao {
	h := sha256.New()
	h.Write(ant[:])
	hashJSON(h, v)
	var out mask.Posicao
	copy(out[:], h.Sum(nil))
	return out
}

// hashJSON escreve v em h numa forma estável: chaves em ordem e sem cache_control (o
// marcador de cache muda de lugar a cada mensagem e não faz parte do conteúdo).
func hashJSON(h hash.Hash, v any) {
	var n [8]byte
	escrever := func(tag byte, s string) {
		binary.LittleEndian.PutUint64(n[:], uint64(len(s)))
		h.Write([]byte{tag})
		h.Write(n[:])
		io.WriteString(h, s)
	}
	switch x := v.(type) {
	case string:
		escrever('s', x)
	case json.Number:
		escrever('n', string(x))
	case bool:
		escrever('b', fmt.Sprint(x))
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
			escrever('k', k)
			hashJSON(h, x[k])
		}
		h.Write([]byte{'}'})
	default:
		escrever('?', fmt.Sprint(x))
	}
}

// unidade: processa v como um bloco da conversa, com a posição encadeada. Só os blocos de
// primeiro nível encadeiam: o conteúdo de um tool_result é parte do bloco dele.
func (w walker) unidade(v any, f func(w walker)) {
	if w.dentro {
		f(w)
		return
	}
	var prox mask.Posicao
	if w.col != nil || w.pos.i >= len(w.pos.seq) {
		prox = depois(w.pos.atual, v)
		if w.col != nil {
			w.pos.seq = append(w.pos.seq, prox)
		}
	} else {
		prox = w.pos.seq[w.pos.i]
	}
	w.pos.i++
	w.pos.bloco, w.pos.n = prox, 0
	w.dentro = true
	f(w)
	w.pos.atual = prox
}

// chaves: as chaves de um objeto JSON em ordem (a de um mapa em Go é aleatória, e a ordem
// dos textos de um bloco faz parte da posição de cada um).
func chaves(x map[string]any) []string {
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
	cfg  config.Config
	m    *mask.Masker // só para as palavras traduzidas de um tool_use (dicasDosComandos)
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
	// dicas: tool_use_id -> o que o comando da chamada diz do resultado (mask.Comandos.Dica)
	dicas map[string]string
	// dicasUso: tool_use id -> o que se sabe da entrada da chamada (eco, palavras traduzidas)
	dicasUso map[string]string
	// dica: a do resultado atual
	dica string
	pos  *posicao
	// dentro: já dentro de um bloco (não encadeia de novo)
	dentro bool
	// assist: mensagem do assistente (texto e entrada de ferramenta escritos pelo modelo)
	assist bool
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
	if w.assist && !w.daWeb {
		return w.escrito(v)
	}
	if f, _ := mask.SepararFonte(w.dica); w.col != nil && f == mask.FonteSistema {
		w.pos.doTexto() // catálogo de sistema: não é fonte de decisão (mask/rastreamento.go)
		return v
	}
	if w.col != nil {
		w.col.itens = append(w.col.itens, mask.ItemLote{S: v, DaWeb: w.daWeb, Pos: w.pos.doTexto(), Dica: w.dica})
		return v
	}
	out, e := w.lote.MascararDica(v, w.daWeb, w.pos.doTexto(), w.dica)
	*w.ents = append(*w.ents, e...)
	return out
}

// escrito: texto do assistente. Se o proxy o desmascarou (quem escreveu), volta como a API o
// mandou; as palavras traduzidas entram na memória da conversa.
func (w walker) escrito(v string) string {
	if w.col != nil {
		if d, ok := w.lote.Escrito(v); ok {
			w.pos.doTexto()
			w.col.extras = append(w.col.extras, d...)
			return v
		}
		if !w.daWeb {
			w.pos.doTexto() // o texto do assistente não é fonte de decisão (mask/rastreamento.go)
			return v
		}
		w.col.itens = append(w.col.itens, mask.ItemLote{S: v, DaWeb: w.daWeb, Pos: w.pos.doTexto(), Dica: w.dica})
		return v
	}
	out, e := w.lote.MascararEscrito(v, w.daWeb, w.pos.doTexto(), w.dica)
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

// dicasDosComandos: para cada resultado de ferramenta, o que o comando da chamada (o tool_use
// do mesmo id) diz dele: as colunas de um SELECT, o tipo pedido numa listagem, a coluna de um
// arquivo cujo cabeçalho já passou, as palavras do programa (ver mask/comando.go e
// mask/chamada.go). E, para cada tool_use, o que se sabe da entrada (eco de uma saída anterior,
// palavras que o proxy traduziu de um pseudônimo). Calculado em ordem, antes de mascarar (a
// entrada do tool_use ainda é a original), e igual nas duas passadas.
func dicasDosComandos(msgs []any, m *mask.Masker) (dicas, dicasUso map[string]string) {
	cs := mask.NovosComandos()
	cmds := map[string]string{}
	chs := map[string]*mask.Chamada{}
	dicas, dicasUso = map[string]string{}, map[string]string{}
	blocosDe := func(mm any) (string, []any) {
		msg, _ := mm.(map[string]any)
		papel, _ := msg["role"].(string)
		if t, ok := msg["content"].(string); ok {
			return papel, []any{map[string]any{"type": "text", "text": t}}
		}
		blocos, _ := msg["content"].([]any)
		return papel, blocos
	}
	// os argumentos de todas as chamadas (o eco procura só por eles nas saídas)
	escritos := map[string]string{} // arquivo que a conversa escreveu -> as fontes que ele cita
	for _, mm := range msgs {
		_, blocos := blocosDe(mm)
		for _, b := range blocos {
			if bl, _ := b.(map[string]any); bl != nil && bl["type"] == "tool_use" {
				if id, _ := bl["id"].(string); id != "" {
					cmds[id] = comandoDe(bl["input"])
					cs.Argumentos(cmds[id])
				}
				anotarEscrito(escritos, bl["input"])
			}
		}
	}
	for _, mm := range msgs {
		papel, blocos := blocosDe(mm)
		for _, b := range blocos {
			bl, _ := b.(map[string]any)
			if bl == nil {
				continue
			}
			id, _ := bl["id"].(string)
			switch bl["type"] {
			case "text":
				if papel == "user" {
					if t, ok := bl["text"].(string); ok {
						cs.Usuario(semLembretes(t))
					}
				}
			case "tool_use":
				if id != "" {
					ch := cs.Uso(cmds[id], traduzidasEm(m, bl["input"]))
					chs[id] = ch
					if e := ch.ExtUso(); e != "" {
						dicasUso[id] = mask.ComExtensao("", e)
					}
				}
			case "tool_result":
				rid, _ := bl["tool_use_id"].(string)
				saida := textoDe(bl["content"])
				d := ""
				if cmd := cmds[rid]; cmd != "" {
					d = cs.Dica(cmd, saida)
				}
				d = mask.ComExtensao(d, cs.Resultado(chs[rid], saida))
				if cmd := cmds[rid]; cmd != "" {
					d = mask.ComFonte(fonteCom(escritos, cmd), d) // a fonte do resultado
					if mask.AlvoDeSistema(cmd) {
						d = mask.ComFonte(mask.FonteSistema, "")
					}
				}
				if d != "" {
					dicas[rid] = d
				}
			}
		}
		if papel == "assistant" {
			cs.FimTurno()
		}
	}
	return dicas, dicasUso
}

// semLembretes: o texto do usuário sem os blocos <system-reminder> que o Claude Code acrescenta
// (não são o pedido do usuário).
func semLembretes(t string) string {
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

// traduzidasEm: as palavras que o proxy traduziu de um pseudônimo nas strings da entrada de um
// tool_use (ver mask.Masker.RegistrarResposta).
func traduzidasEm(m *mask.Masker, v any) []mask.PalavraTraduzida {
	if m == nil {
		return nil
	}
	var out []mask.PalavraTraduzida
	var f func(v any)
	f = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, m.PalavrasTraduzidas(x)...)
		case []any:
			for _, e := range x {
				f(e)
			}
		case map[string]any:
			for _, k := range chaves(x) {
				f(x[k])
			}
		}
	}
	f(v)
	return out
}

// comandoDe: o texto da entrada de uma ferramenta que diz o que ela faz (o comando do shell,
// a consulta, o caminho). A descrição livre fica de fora.
func comandoDe(v any) string {
	in, _ := v.(map[string]any)
	if c, ok := in["command"].(string); ok {
		return c
	}
	var b strings.Builder
	for _, k := range chaves(in) {
		if s, ok := in[k].(string); ok && k != "description" && len(s) <= 4<<10 {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// textoDe: o texto de um conteúdo de tool_result (string ou blocos de texto).
func textoDe(v any) string {
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
var chavesIntocaveis = map[string]bool{"type": true, "id": true, "tool_use_id": true, "signature": true,
	"media_type": true, "cache_control": true, "model": true, "role": true, "stop_reason": true,
	"stop_sequence": true, "citations": false}

// requisicaoAnthropic: /v1/messages e /v1/messages/count_tokens.

func (w walker) requisicaoAnthropic(v any) any {
	req, ok := v.(map[string]any)
	if !ok {
		return w.generico(v)
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
			req[k] = w.conteudo(val)
		case "messages":
			if msgs, ok := val.([]any); ok {
				w.idsWeb = w.idsDaWeb(msgs)
				w.dicas, w.dicasUso = dicasDosComandos(msgs, w.m)
				for _, mm := range msgs {
					if msg, ok := mm.(map[string]any); ok {
						// quem fala também faz parte da posição
						w.pos.atual = depois(w.pos.atual, msg["role"])
						wm := w
						wm.assist = msg["role"] == "assistant"
						msg["content"] = wm.conteudo(msg["content"])
					}
				}
			}
		case "tools":
			// descrições podem citar dado (ex.: ferramenta de MCP); o nome não pode mudar
			if ts, ok := val.([]any); ok {
				for _, t := range ts {
					if tm, ok := t.(map[string]any); ok {
						w.unidade(tm, func(w walker) {
							for _, tk := range chaves(tm) {
								if tk != "name" && tk != "type" {
									tm[tk] = w.generico(tm[tk])
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
			w.unidade(val, func(w walker) { req[k] = w.generico(val) })
		}
	}
	return req
}

// conteudo: string ou lista de blocos.
func (w walker) conteudo(v any) any {
	switch c := v.(type) {
	case string:
		var out string
		w.unidade(c, func(w walker) { out = w.s(c) })
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
			w.unidade(bl, func(w walker) {
				if (bl["type"] == "image" || bl["type"] == "document") && ehBase64(bl) {
					out = append(out, w.midia(bl)...)
					return
				}
				out = append(out, w.bloco(bl))
			})
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
		id, _ := b["id"].(string)
		w.dica = w.dicasUso[id]
		b["input"] = w.tudo(b["input"])
		return b
	case "tool_result":
		id, _ := b["tool_use_id"].(string)
		if w.idsWeb[id] {
			w.daWeb = true
		}
		w.dica = w.dicas[id]
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
		for _, k := range chaves(x) {
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
		for _, k := range chaves(x) {
			if !chavesIntocaveis[k] && k != "data" {
				x[k] = w.generico(x[k])
			}
		}
	}
	return v
}

// anotarEscrito: um arquivo que a conversa escreveu (Write, Edit) cita as fontes que lê; rodá-lo
// depois tem essas fontes também ("python3 gerar_tags.py" lê o catalogo.csv que o script cita).
func anotarEscrito(escritos map[string]string, v any) {
	in, _ := v.(map[string]any)
	p, _ := in["file_path"].(string)
	if p == "" {
		return
	}
	var corpo string
	for _, k := range []string{"content", "new_string"} {
		if c, ok := in[k].(string); ok {
			corpo += c + "\n"
		}
	}
	if corpo == "" {
		return
	}
	nome := strings.ToLower(p[strings.LastIndexAny(p, "/\\")+1:])
	if f := mask.FonteDoComando(corpo); f != "" && !strings.HasPrefix(f, "cmd:") {
		escritos[nome] = f
	}
}

// fonteCom: a fonte do comando, com as fontes herdadas dos arquivos que a conversa escreveu.
func fonteCom(escritos map[string]string, cmd string) string {
	f := mask.FonteDoComando(cmd)
	var extra []string
	for _, x := range strings.Split(f, ",") {
		if e := escritos[x]; e != "" {
			extra = append(extra, e)
		}
	}
	if len(extra) == 0 {
		return f
	}
	return f + "," + strings.Join(extra, ",")
}
