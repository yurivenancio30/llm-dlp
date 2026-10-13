package main

// Comandos para conferir o llm-dlp: "ultima" (o que a API recebeu na última mensagem) e
// "simular" (reproduz uma sessão antiga do Claude Code pelo llm-dlp, sem falar com a API).

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

// ultima mostra as últimas mensagens da última requisição, do jeito que foram para a API.
// Serve para conferir, durante o uso, o que saiu mascarado.
func ultima(args []string) error {
	n := 2
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			n = v
		}
	}
	cfg, err := config.Carregar()
	if err != nil {
		return err
	}
	// a conversa, mesmo mascarada, só sai para quem lê a chave (proxy/local.go)
	chave, err := mask.Chave(config.Caminho("chave"))
	if err != nil {
		return err
	}
	pedido, _ := http.NewRequest("GET", "http://"+endereco(cfg)+"/__llm-dlp/ultima", nil)
	pedido.Header.Set(proxy.CabecalhoProva, proxy.Prova(chave, "ultima"))
	resp, err := http.DefaultClient.Do(pedido)
	if err != nil {
		return fmt.Errorf("o llm-dlp não está no ar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return errors.New("o llm-dlp no ar não aceitou a prova: ele é de outra versão ou usa outra chave (rode llm-dlp status)")
	}
	corpo, _ := io.ReadAll(resp.Body)
	msgs := mensagensDaUltima(corpo)
	if len(msgs) == 0 {
		fmt.Println("Ainda não passou nenhuma mensagem desde que o llm-dlp subiu.")
		return nil
	}
	if n > len(msgs) {
		n = len(msgs)
	}
	fmt.Printf("Últimas %d de %d mensagens, como a API recebeu (os pseudônimos estão no lugar dos valores reais):\n", n, len(msgs))
	for _, m := range msgs[len(msgs)-n:] {
		for _, t := range m.textos {
			if len(t) > 3000 {
				t = t[:3000] + fmt.Sprintf("\n… (mais %d caracteres)", len(t)-3000)
			}
			fmt.Printf("\n── %s ──\n%s\n", m.papel, t)
		}
	}
	return nil
}

// mensagemVista: uma mensagem da última requisição, com os textos que "llm-dlp last" mostra.
type mensagemVista struct {
	papel  string
	textos []string
}

// mensagensDaUltima lê as mensagens da última requisição mascarada, no formato da Anthropic
// (messages) ou do Gemini (contents).
func mensagensDaUltima(corpo []byte) []mensagemVista {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
			// formato da OpenAI: as chamadas de ferramenta ficam fora do conteúdo
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
		// Gemini CLI com a conta Google: a conversa do Gemini vai dentro de "request"
		Request struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []any  `json:"parts"`
			} `json:"contents"`
		} `json:"request"`
		// Responses (o Codex): instruções e itens
		Instructions string `json:"instructions"`
		Input        any    `json:"input"`
	}
	if json.Unmarshal(corpo, &req) != nil {
		return nil
	}
	var out []mensagemVista
	for _, m := range req.Messages {
		ts := textosDoConteudo(m.Content)
		for _, tc := range m.ToolCalls {
			ts = append(ts, fmt.Sprintf("[ferramenta %s] %s", tc.Function.Name, tc.Function.Arguments))
		}
		out = append(out, mensagemVista{m.Role, ts})
	}
	for _, c := range append(req.Contents, req.Request.Contents...) {
		out = append(out, mensagemVista{c.Role, textosDasPartes(c.Parts)})
	}
	if req.Instructions != "" {
		out = append(out, mensagemVista{"instructions", []string{req.Instructions}})
	}
	return append(out, itensDoResponses(req.Input)...)
}

// itensDoResponses: as mensagens do "input" do Responses (texto, chamada e resultado de ferramenta).
func itensDoResponses(entrada any) []mensagemVista {
	if s, ok := entrada.(string); ok {
		return []mensagemVista{{"user", []string{s}}}
	}
	itens, _ := entrada.([]any)
	textos := func(c any) []string {
		if s, ok := c.(string); ok {
			return []string{s}
		}
		var out []string
		partes, _ := c.([]any)
		for _, x := range partes {
			pm, _ := x.(map[string]any)
			if t, ok := pm["text"].(string); ok {
				out = append(out, t)
			} else if pm != nil {
				out = append(out, "[arquivo]")
			}
		}
		return out
	}
	var out []mensagemVista
	for _, x := range itens {
		it, _ := x.(map[string]any)
		papel, _ := it["role"].(string)
		switch it["type"] {
		case "message", nil:
			out = append(out, mensagemVista{papel, textos(it["content"])})
		case "function_call":
			out = append(out, mensagemVista{"assistant", []string{fmt.Sprintf("[ferramenta %v] %v", it["name"], it["arguments"])}})
		case "custom_tool_call":
			out = append(out, mensagemVista{"assistant", []string{fmt.Sprintf("[ferramenta %v] %v", it["name"], it["input"])}})
		case "function_call_output", "custom_tool_call_output":
			var ts []string
			for _, t := range textos(it["output"]) {
				ts = append(ts, "[resultado de ferramenta]\n"+t)
			}
			out = append(out, mensagemVista{"user", ts})
		}
	}
	return out
}

// textosDasPartes devolve os textos de uma mensagem do Gemini (texto, chamada e resultado de
// ferramenta).
func textosDasPartes(partes []any) []string {
	var out []string
	for _, p := range partes {
		pt, _ := p.(map[string]any)
		if t, _ := pt["text"].(string); t != "" {
			out = append(out, t)
		}
		if fc, _ := pt["functionCall"].(map[string]any); fc != nil {
			b, _ := json.Marshal(fc["args"])
			out = append(out, fmt.Sprintf("[ferramenta %v] %s", fc["name"], b))
		}
		if fr, _ := pt["functionResponse"].(map[string]any); fr != nil {
			out = append(out, "[resultado de ferramenta]\n"+textoDaResposta(fr["response"]))
		}
		for _, k := range []string{"inlineData", "inline_data", "fileData", "file_data"} {
			if d, _ := pt[k].(map[string]any); d != nil {
				out = append(out, "[arquivo]")
			}
		}
	}
	return out
}

// textoDaResposta: o resultado de uma ferramenta do Gemini. Os textos saem como texto (um por
// linha, com o nome do campo quando há mais de um); o resto, em JSON.
func textoDaResposta(r any) string {
	m, _ := r.(map[string]any)
	var nomes []string
	for k, v := range m {
		if _, ok := v.(string); !ok {
			b, _ := json.Marshal(r)
			return string(b)
		}
		nomes = append(nomes, k)
	}
	if len(nomes) == 0 {
		b, _ := json.Marshal(r)
		return string(b)
	}
	sort.Strings(nomes)
	var linhas []string
	for _, k := range nomes {
		if len(nomes) == 1 {
			return m[k].(string)
		}
		linhas = append(linhas, k+": "+m[k].(string))
	}
	return strings.Join(linhas, "\n")
}

// textosDoConteudo devolve os textos de uma mensagem (texto, resultado e entrada de ferramenta).
func textosDoConteudo(c any) []string {
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
				for _, t := range textosDoConteudo(bl["content"]) {
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

// simular reproduz uma sessão antiga do Claude Code: remonta as requisições que o Claude
// Code teria feito, passa cada uma pelo llm-dlp e as entrega a uma API falsa local, que só
// devolve o último texto que recebeu. Nada vai para a internet. Confere, a cada requisição:
// o proxy aceitou; nenhum valor que ele mesmo detectou sobrou em outro ponto da requisição;
// a resposta voltou igual ao texto original; e as mensagens antigas saíram idênticas às da
// requisição anterior (o cache da API depende disso). Só imprime contagens.
func simular(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ContinueOnError)
	max := fs.Int("max", 400, "número máximo de requisições por sessão")
	janela := fs.Int("window", 600, "tamanho máximo da conversa, em KB (como o limite de contexto)")
	fs.IntVar(janela, "janela", 600, "nome antigo de --window")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("uso: llm-dlp simulate [--max N] [--window KB] SESSAO.jsonl...")
	}
	cfg, chave, _, _, _, err := carregarTudo()
	if err != nil {
		return err
	}
	for _, arq := range fs.Args() {
		// cópia do registro de pessoas e nenhum arquivo de hashes: a simulação não muda nada
		tmp, err := os.MkdirTemp("", "llm-dlp-simular-")
		if err != nil {
			return err
		}
		if b, err := os.ReadFile(config.Caminho("pessoas.json")); err == nil {
			os.WriteFile(tmp+"/pessoas.json", b, 0o600)
		}
		ps, _ := mask.CarregarPessoas(tmp + "/pessoas.json")
		m, err := mask.NovoMasker(cfg, chave, ps, nil)
		if err != nil {
			os.RemoveAll(tmp)
			return err
		}
		err = simularSessao(arq, cfg, m, *max, *janela<<10)
		os.RemoveAll(tmp)
		if err != nil {
			return err
		}
	}
	return nil
}

func simularSessao(arq string, cfg config.Config, m *mask.Masker, limite, janela int) error {
	// a API falsa: guarda o corpo que recebeu e devolve, em pedaços, o último texto
	var recebido []byte
	ecoCortado := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebido, _ = io.ReadAll(r.Body)
		ecoCortado = false
		var req struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(recebido, &req)
		eco := ""
		if n := len(req.Messages); n > 0 {
			if ts := textosDoConteudo(req.Messages[n-1].Content); len(ts) > 0 {
				eco = ts[len(ts)-1]
			}
		}
		if len(eco) > 4000 {
			ecoCortado = true
			eco = eco[:4000]
			for len(eco) > 0 && eco[len(eco)-1]&0xC0 == 0x80 {
				eco = eco[:len(eco)-1]
			}
		}
		w.Header().Set("content-type", "text/event-stream")
		ev := func(nome string, v any) {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", nome, b)
		}
		ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		rs := []rune(eco)
		for i := 0; i < len(rs); i += 9 {
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0,
				"delta": map[string]any{"type": "text_delta", "text": string(rs[i:min(i+9, len(rs))])}})
		}
		ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		// a escrita: a última chamada de ferramenta da conversa volta como se o modelo a tivesse
		// escrito agora, com os pseudônimos que recebeu, em pedaços (como a API manda)
		var conteudos []any
		for _, m := range req.Messages {
			conteudos = append(conteudos, m.Content)
		}
		if uso := ultimoUso(conteudos); uso != nil {
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": 1,
				"content_block": map[string]any{"type": "tool_use", "id": "toolu_simulacao", "name": uso["name"], "input": map[string]any{}}})
			var in bytes.Buffer
			enc := json.NewEncoder(&in)
			enc.SetEscapeHTML(false)
			enc.Encode(uso["input"])
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
	px := httptest.NewServer(proxy.Novo(cfg, m, nil, log.New(io.Discard, "", 0)))
	defer px.Close()

	f, err := os.Open(arq)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 512<<20)

	type msg struct {
		id  int
		raw map[string]any
		tam int
	}
	var conversa []msg
	total, prox := 0, 0
	anteriores := map[int][32]byte{} // id da mensagem -> como saiu mascarada da última vez
	var (
		reqs, recusadas, erros, idaVoltaDif, comSobra, sobras, mudouCache, imagens int
		tempos                                                                     []time.Duration
		motivos                                                                    = map[string]int{}
		tiposSobra                                                                 = map[string]int{}
	)
	depurar := map[string]int{}
	var (
		escritas, escritasMascaradas, escritasDif, escritasComSobra, escritasWeb int
		difPorFerramenta                                                         = map[string]int{}
	)
	// a conversa de um subagente: no arquivo da sessão, vem marcada como lateral (isSidechain) e é
	// pulada; no arquivo do próprio subagente (subagents/agent-*.jsonl), é toda lateral e é ela
	// que se repete. Quem decide é a primeira mensagem do arquivo.
	lateral, primeira := false, true
	for sc.Scan() && reqs < limite {
		var e struct {
			Type        string         `json:"type"`
			IsSidechain bool           `json:"isSidechain"`
			Message     map[string]any `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || (e.Type != "user" && e.Type != "assistant") || e.Message == nil {
			continue
		}
		if primeira {
			lateral, primeira = e.IsSidechain, false
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
		conversa = append(conversa, msg{prox, mm, len(b)})
		prox++
		total += len(b)
		for total > janela && len(conversa) > 1 {
			total -= conversa[0].tam
			conversa = conversa[1:]
		}
		if role != "user" {
			continue
		}
		// monta e envia a requisição, como o Claude Code faria neste ponto
		msgs := make([]any, len(conversa))
		for i, c := range conversa {
			msgs[i] = c.raw
		}
		corpo, _ := json.Marshal(map[string]any{"model": "simulacao", "stream": true, "max_tokens": 1000, "messages": msgs})
		imagens += bytes.Count(corpo, []byte(`"type":"image"`))
		esperado := ""
		if ts := textosDoConteudo(mm["content"]); len(ts) > 0 {
			esperado = ts[len(ts)-1]
		}
		recebido = nil
		t0 := time.Now()
		resp, err := http.Post(px.URL+"/v1/messages", "application/json", bytes.NewReader(corpo))
		if err != nil {
			erros++
			continue
		}
		corpoResp, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		resposta := lerTextoSSE(bytes.NewReader(corpoResp))
		tempos = append(tempos, time.Since(t0))
		reqs++
		if resp.StatusCode != 200 {
			recusadas++
			var er struct {
				Error struct{ Message string } `json:"error"`
			}
			json.Unmarshal([]byte(resposta), &er)
			mot := "outro motivo"
			switch {
			case strings.Contains(resposta, "OCR") || strings.Contains(resposta, "image") || strings.Contains(resposta, "verificar"):
				mot = "imagem ou PDF que não deu para verificar (falta o OCR)"
			}
			motivos[mot]++
			continue
		}
		// 1) ida e volta. O eco cortado (o texto mascarado passou de 4000 bytes, mesmo com o
		// original abaixo disso: o pseudônimo pode ser maior que o valor) não dá para comparar.
		if esperado != "" && !ecoCortado && resposta != esperado {
			idaVoltaDif++
		}
		// 1b) a escrita: a chamada de ferramenta que "o modelo escreveu" com pseudônimos voltou
		// igual à original? É o que o Claude Code grava no arquivo ou roda no terminal.
		var conteudos []any
		for _, c := range conversa {
			conteudos = append(conteudos, c.raw["content"])
		}
		if uso := ultimoUso(conteudos); uso != nil {
			nome, _ := uso["name"].(string)
			voltou, ok := entradaDaResposta(corpoResp)
			switch {
			case cfg.SemDesmascarar(nome):
				escritasWeb++ // vai para a internet: fica com os pseudônimos, de propósito
			case ok:
				escritas++
				orig, _ := json.Marshal(uso["input"])
				var env struct {
					Messages []struct {
						Content any `json:"content"`
					} `json:"messages"`
				}
				json.Unmarshal(recebido, &env)
				var enviados []any
				for _, m := range env.Messages {
					enviados = append(enviados, m.Content)
				}
				if e := ultimoUso(enviados); e != nil {
					if b, _ := json.Marshal(e["input"]); !bytes.Equal(b, orig) {
						escritasMascaradas++
					}
				}
				var a, b any
				json.Unmarshal(orig, &a)
				json.Unmarshal(voltou, &b)
				if !reflect.DeepEqual(a, b) {
					escritasDif++
					difPorFerramenta[nome]++
					if len(reCaraDePseudo.FindAll(voltou, -1)) > len(reCaraDePseudo.FindAll(orig, -1)) {
						escritasComSobra++
					}
				}
			}
		}
		// 2) algo que o próprio llm-dlp detectou sobrou em outro ponto do que foi enviado?
		var env struct {
			Messages []json.RawMessage `json:"messages"`
		}
		json.Unmarshal(recebido, &env)
		enviado := semRaciocinio(env.Messages)
		reais := map[string]string{}
		for _, t := range todosOsTextos(msgs) {
			if os.Getenv("LLM_DLP_DEPURAR") == "2" {
				for _, a := range m.Detectar(t) {
					if a.Tipo == "usuario" || a.Tipo == "nome" {
						i0 := max(0, a.Ini-40)
						for i0 < a.Ini && t[i0]&0xC0 == 0x80 {
							i0++
						}
						antes := strings.ToLower(strings.ReplaceAll(t[i0:a.Ini], "\n", "⏎"))
						var sb strings.Builder
						for _, r := range antes {
							if r >= '0' && r <= '9' {
								sb.WriteByte('9')
							} else {
								sb.WriteRune(r)
							}
						}
						depurar["ORIGEM "+a.Tipo+" | antes "+sb.String()]++
					}
				}
			}
			_, ents := m.Mascarar(t)
			for _, en := range ents {
				if len(en.Real) >= 6 {
					reais[en.Real] = en.Tipo
				}
			}
		}
		achou := false
		for real, tipo := range reais {
			if strings.Contains(enviado, real) {
				if os.Getenv("LLM_DLP_DEPURAR") != "" {
					i := strings.Index(enviado, real)
					a, b := max(0, i-24), min(len(enviado), i+len(real)+12)
					forma := func(t string) string {
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
					depurar[tipo+" | valor "+forma(real)+" | antes "+strings.ToLower(enviado[a:i])+"▮"+forma(enviado[i+len(real):b])]++
				}
				sobras++
				tiposSobra[tipo]++
				achou = true
			}
		}
		if achou {
			comSobra++
		}
		// 3) as mensagens antigas saíram iguais às da requisição anterior?
		if len(env.Messages) == len(conversa) {
			mudou := false
			for i, c := range conversa {
				h := sha256.Sum256(env.Messages[i])
				if ant, ok := anteriores[c.id]; ok && ant != h {
					mudou = true
				}
				anteriores[c.id] = h
			}
			if mudou {
				mudouCache++
			}
		}
	}
	if len(depurar) > 0 {
		var ks []string
		for k := range depurar {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool { return depurar[ks[i]] > depurar[ks[j]] })
		for i, k := range ks {
			if i < 25 {
				fmt.Printf("    DEP %5d  %s\n", depurar[k], strings.ReplaceAll(k, "\n", "⏎"))
			}
		}
	}
	sort.Slice(tempos, func(i, j int) bool { return tempos[i] < tempos[j] })
	q := func(p float64) time.Duration {
		if len(tempos) == 0 {
			return 0
		}
		return tempos[int(float64(len(tempos)-1)*p)].Round(time.Millisecond)
	}
	nome := arq
	if i := strings.LastIndexByte(arq, '/'); i >= 0 {
		nome = arq[i+1:]
	}
	fmt.Printf("### %.8s: %d requisições simuladas (conversa de até %d KB), %d com imagem\n", nome, reqs, janela>>10, imagens)
	if reqs == 0 {
		fmt.Println("    ATENÇÃO: nenhuma requisição repetida: o arquivo não tem mensagem do usuário que o simular leia. Nada foi conferido.")
	}
	fmt.Printf("    aceitas: %d | recusadas pelo llm-dlp: %d %v | erros de conexão: %d\n", reqs-recusadas, recusadas, motivos, erros)
	fmt.Printf("    resposta voltou diferente do original: %d\n", idaVoltaDif)
	fmt.Printf("    escrita (chamada de ferramenta que o modelo escreve com pseudônimos): %d conferidas, %d tinham valor mascarado\n", escritas, escritasMascaradas)
	fmt.Printf("    escrita que voltou diferente da original: %d %v, das quais %d com pseudônimo sobrando | para a internet (ficam mascaradas): %d\n",
		escritasDif, difPorFerramenta, escritasComSobra, escritasWeb)
	fmt.Printf("    valor detectado que sobrou em outro ponto da requisição: %d ocorrências em %d requisições %v\n", sobras, comSobra, tiposSobra)
	fmt.Printf("    requisições em que uma mensagem antiga mudou (cache refeito): %d\n", mudouCache)
	fmt.Printf("    tempo por requisição (ida, API falsa e volta): mediana %s | 95%% abaixo de %s | máximo %s\n", q(.5), q(.95), q(1))
	return nil
}

// reCaraDePseudo: o que tem a forma de um pseudônimo do llm-dlp (para contar o que sobrou).
var reCaraDePseudo = regexp.MustCompile(`p\.[a-z2-7]{8}@d[a-z2-7]{4}\.invalid|\bh[a-z2-7]{8}\.invalid|\b[a-z]{1,5}_[a-z2-7]{8}\b|\b(?:[A-Z]{2,12}|[a-z]{2,12}|[A-Z][a-z]{1,11})-[a-z2-7]{8}\b|[Pp]essoa [a-z2-7]{8}|PESSOA [A-Z2-7]{8}`)

// ultimoUso: a última chamada de ferramenta (tool_use) dos conteúdos das mensagens.
func ultimoUso(conteudos []any) map[string]any {
	for i := len(conteudos) - 1; i >= 0; i-- {
		blocos, _ := conteudos[i].([]any)
		for j := len(blocos) - 1; j >= 0; j-- {
			if b, _ := blocos[j].(map[string]any); b != nil && b["type"] == "tool_use" {
				return b
			}
		}
	}
	return nil
}

// entradaDaResposta junta a entrada da chamada de ferramenta de uma resposta em streaming.
func entradaDaResposta(b []byte) ([]byte, bool) {
	var out bytes.Buffer
	achou := false
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
			achou = true
			out.WriteString(ev.Delta.PartialJSON)
		}
	}
	return out.Bytes(), achou
}

// lerTextoSSE junta o texto de uma resposta em streaming (ou devolve o corpo, se for um erro).
func lerTextoSSE(body io.Reader) string {
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

// semRaciocinio devolve as mensagens como texto, sem os blocos de raciocínio (que por regra
// da API não podem ser alterados; numa sessão que já passou pelo llm-dlp eles só têm pseudônimos).
func semRaciocinio(msgs []json.RawMessage) string {
	var b strings.Builder
	var tirar func(v any) any
	tirar = func(v any) any {
		switch x := v.(type) {
		case []any:
			out := x[:0:0]
			for _, e := range x {
				if mm, ok := e.(map[string]any); ok && (mm["type"] == "thinking" || mm["type"] == "redacted_thinking") {
					continue
				}
				out = append(out, tirar(e))
			}
			return out
		case map[string]any:
			for k, e := range x {
				x[k] = tirar(e)
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
		enc.Encode(tirar(v))
	}
	return b.String()
}

// todosOsTextos junta os textos de todas as mensagens (menos raciocínio e imagens).
func todosOsTextos(msgs []any) []string {
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
