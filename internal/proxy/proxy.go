// Package proxy fica entre o cliente (Claude Code) e a API da LLM: mascara tudo que
// sai e desmascara tudo que volta. Do cliente para o usuário, tudo é real; só o
// trecho até a API é mascarado.
package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// O servidor HTTP: recebe a requisição do Claude Code, manda mascarada para a API e devolve
// a resposta desmascarada.

const Version = version.Version

type Proxy struct {
	cfg     config.Config
	m       *mask.Masker
	media   *Media
	seenSet *mask.Seen
	client  *http.Client
	key     []byte // para as provas da porta local (WithKey)
	swap    bool   // a porta é de um supervisor que troca o proxy sem fechá-la (WithSwap)
	log     *log.Logger
	start   time.Time
	reqs    atomic.Int64
	// a última requisição de mensagens, já mascarada (só em memória): é o que o comando
	// "llm-dlp last" mostra, para conferir o que a API recebeu
	lastConv atomic.Pointer[[]byte]
}

// destino: para onde a requisição vai e em que formato ela vem. "/gemini/..." vai para o
// provedor "gemini" da configuração, sem o "/gemini"; o que não começa com o nome de um provedor
// vai para o upstream (a API da Anthropic).
func (p *Proxy) destination(r *http.Request) (format, upstream, uri string) {
	uri = r.URL.RequestURI()
	name, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if pv, ok := p.cfg.Providers[name]; ok && name != "" && pv.Upstream != "" {
		format = pv.Format
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			format = "openai" // o endereço compatível com a OpenAI de qualquer provedor (o do Gemini também)
		case strings.HasSuffix(r.URL.Path, "/responses"):
			format = "responses" // o Responses da OpenAI (o Codex, com chave ou com a assinatura do ChatGPT)
		case format == "codeassist":
			if i := strings.LastIndexByte(r.URL.Path, ':'); i >= 0 && accountMethods[r.URL.Path[i+1:]] {
				format = accountFormat
			}
		}
		return format, pv.Upstream, strings.TrimPrefix(uri, "/"+name)
	}
	if strings.HasPrefix(r.URL.Path, "/v1/messages") {
		format = "anthropic"
	}
	return format, p.cfg.Upstream, uri
}

// bridgeNoUnmask: na volta do Gemini e da OpenAI, a ferramenta segue com os pseudônimos
// (vai para a internet): as da configuração e as de mesmo papel nesses clientes.
func (p *Proxy) bridgeNoUnmask(name string) bool {
	return p.cfg.NoUnmask(name) || bridgeWebTools[name]
}

// requiredError: falha que bloqueia a requisição independentemente de falhar_fechado.
type requiredError struct{ error }

func New(cfg config.Config, m *mask.Masker, seenSet *mask.Seen, logger *log.Logger) *Proxy {
	media := NewMedia(cfg, m)
	media.Warning = func(msg string) { logger.Printf("AVISO %s", msg) }
	return &Proxy{cfg: cfg, m: m, media: media, seenSet: seenSet, log: logger, start: time.Now(),
		client: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableCompression: true,
			DialContext:       (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2: true, TLSHandshakeTimeout: 15 * time.Second,
			MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second}}}
}

var hopByHop = map[string]bool{"connection": true, "keep-alive": true, "proxy-connection": true, "te": true,
	"trailer": true, "transfer-encoding": true, "upgrade": true, "host": true, "content-length": true,
	"accept-encoding": true, "content-encoding": true}

// WithKey liga as provas da porta local (ver local.go). Sem chave, a última conversa não é
// servida e o proxy não prova quem é.
func (p *Proxy) WithKey(key []byte) *Proxy {
	p.key = key
	return p
}

// WithSwap: a saúde diz que este proxy está num supervisor que segura a porta e o troca sem
// fechá-la (cmd/llm-dlp/swap.go); é o que o atualizar pergunta antes de pedir a troca.
func (p *Proxy) WithSwap(swap bool) *Proxy {
	p.swap = swap
	return p
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !Local(r) {
		p.log.Printf("RECUSADO %s %s: não veio de um programa local (Host %q, Origin %q)", r.Method, cleaned(r.URL.Path),
			cleaned(r.Host), cleaned(r.Header.Get("Origin")))
		refuseNonLocal(w)
		return
	}
	if r.URL.Path == "/__llm-dlp/saude" {
		w.Header().Set("content-type", "application/json")
		info := map[string]any{"ok": true, "servico": "llm-dlp", "versao": Version, "commit": version.Commit, "pid": os.Getpid(),
			"requisicoes": p.reqs.Load(), "no_ar_desde": p.start.Format(time.RFC3339),
			"troca": p.swap}
		if d := r.URL.Query().Get("desafio"); len(p.key) > 0 && validChallenge(d) {
			info["prova"] = Proof(p.key, "saude\x00"+d)
		}
		json.NewEncoder(w).Encode(info)
		return
	}
	if r.URL.Path == "/__llm-dlp/ultima" {
		w.Header().Set("content-type", "application/json")
		if !ProofMatches(p.key, "ultima", r.Header.Get(ProofHeader)) {
			// a conversa (mesmo mascarada) é só de quem lê a chave: use o comando "llm-dlp last"
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"erro":"use o comando: llm-dlp last"}` + "\n"))
			return
		}
		if b := p.lastConv.Load(); b != nil {
			w.Write(*b)
		} else {
			w.Write([]byte("{}"))
		}
		return
	}
	p.reqs.Add(1)
	t0 := time.Now()

	body, err := io.ReadAll(io.LimitReader(r.Body, 512<<20))
	if err != nil {
		p.refuse(w, r, "não consegui ler a requisição")
		return
	}
	format, upstream, uri := p.destination(r)
	var entries []mask.Entry
	var batch *mask.Batch
	unmasked := false
	if len(bytes.TrimSpace(body)) > 0 && format != accountFormat {
		newOne, ents, l, err := p.maskBody(format, r.Header.Get("content-type"), body)
		if err != nil {
			var required requiredError
			if p.cfg.FailClosed || errors.As(err, &required) {
				p.refuse(w, r, err.Error())
				return
			}
			p.log.Printf("AVISO %s %s: enviado sem máscara (%v)", r.Method, cleaned(r.URL.Path), err)
			unmasked = true
		} else {
			body, entries, batch = newOne, ents, l
			if (format == "anthropic" || format == "gemini" || format == "openai" || format == "codeassist" || format == "responses") &&
				!strings.Contains(r.URL.Path, "count_tokens") &&
				!strings.Contains(r.URL.Path, ":countTokens") && len(body) > 2000 {
				c := body
				p.lastConv.Store(&c)
			}
		}
	}
	tab := mask.NewTable(entries)
	tMask := time.Since(t0)

	target := strings.TrimRight(upstream, "/") + uri
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		p.refuse(w, r, "url inválida")
		return
	}
	for k, vs := range r.Header {
		if !hopByHop[strings.ToLower(k)] {
			req.Header[k] = vs
		}
	}
	req.Header.Set("accept-encoding", "identity")
	req.ContentLength = int64(len(body))

	resp, err := p.client.Do(req)
	if err != nil {
		// só a causa: a mensagem inteira traz o endereço, que pode levar a chave da API (?key=...)
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		http.Error(w, "llm-dlp: falha ao contatar a API: "+err.Error(), http.StatusBadGateway)
		p.log.Printf("ERRO %s %s: upstream: %v", r.Method, cleaned(r.URL.Path), err)
		return
	}
	if batch != nil {
		// a API recebeu: daqui em diante, estes textos saem sempre iguais (cache)
		batch.Freeze()
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if !hopByHop[strings.ToLower(k)] {
			w.Header()[k] = vs
		}
	}
	w.WriteHeader(resp.StatusCode)

	ct := resp.Header.Get("content-type")
	apiError := ""
	switch {
	case strings.HasPrefix(ct, "text/event-stream") && (format == "gemini" || format == "codeassist"):
		err = p.unmaskSSEGemini(w, resp.Body, tab)
	case strings.HasPrefix(ct, "text/event-stream") && format == "responses":
		err = p.unmaskSSEResponses(w, resp.Body, tab)
	case strings.HasPrefix(ct, "text/event-stream") && format == "openai":
		err = p.unmaskSSEOpenAI(w, resp.Body, tab)
	case strings.HasPrefix(ct, "text/event-stream"):
		err = p.unmaskSSE(w, resp.Body, tab)
	case strings.Contains(ct, "json") && (!tab.Empty() || resp.StatusCode >= 400):
		var b []byte
		b, err = io.ReadAll(resp.Body)
		if err == nil {
			if resp.StatusCode >= 400 && !unmasked {
				apiError = errorSummary(b)
			}
			if !tab.Empty() && (format == "gemini" || format == "codeassist") {
				b = p.unmaskGeminiJSON(b, tab)
			} else if !tab.Empty() && format == "responses" {
				b = p.unmaskResponsesJSON(b, tab)
			} else if !tab.Empty() && format == "openai" {
				b = p.unmaskOpenAIJSON(b, tab)
			} else if !tab.Empty() {
				b = unmaskJSONResponse(b, tab, p.cfg.NoUnmask,
					func(s string) string { return p.m.RecordResponse(s, tab) })
			}
			_, err = w.Write(b)
		}
	default:
		_, err = io.Copy(w, resp.Body)
	}
	if p.seenSet != nil {
		p.seenSet.SaveIfDirty()
	}
	p.m.Persist()
	p.log.Printf("%s %s -> %d | %d substituições | %d colisões | mascarar %s | total %s%s%s%s%s%s", r.Method, cleaned(r.URL.Path), resp.StatusCode,
		len(entries), tab.Conflicts, tMask.Round(time.Millisecond), time.Since(t0).Round(time.Millisecond), byType(entries),
		collided(tab), notUndone(tab), apiError, errSuffix(err))
}

// byType: quantos valores distintos de cada tipo foram mascarados (" | tipos: cpf 2, ip 1,
// tabela 5"), do mais frequente ao menos. Só contagens: nenhum valor vai para o log.
func byType(es []mask.Entry) string {
	seenSet, n := map[string]bool{}, map[string]int{}
	for _, e := range es {
		t := strings.TrimPrefix(e.Type, "obj.")
		if k := t + "\x00" + strings.ToLower(e.Real); !seenSet[k] {
			seenSet[k] = true
			n[t]++
		}
	}
	if len(n) == 0 {
		return ""
	}
	ts := make([]string, 0, len(n))
	for t := range n {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return n[ts[i]] > n[ts[j]] || n[ts[i]] == n[ts[j]] && ts[i] < ts[j] })
	var b strings.Builder
	b.WriteString(" | tipos:")
	for i, t := range ts {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, " %s %d", t, n[t])
	}
	return b.String()
}

// notUndone: os pseudônimos nossos que sobraram na resposta entregue ao cliente (o cliente,
// e o que ele gravar em arquivo, ficaram com o pseudônimo no lugar do valor real). Pseudônimo
// não é dado real: vai inteiro para o log, para dar para procurar onde ficou.
func notUndone(tab *mask.Table) string {
	nd := tab.NotUndone()
	if len(nd) == 0 {
		return ""
	}
	return fmt.Sprintf(" | NÃO DESFEITOS %d: %s", len(nd), strings.Join(nd, ", "))
}

// colididos: os pseudônimos que colidiram (" | colididos: CPF-x, 240.1.2.x"): a forma de cada um
// diz o tipo do dado que ficou sem desmascarar.
func collided(tab *mask.Table) string {
	c := tab.Collided()
	if len(c) == 0 {
		return ""
	}
	return " | colididos: " + strings.Join(c, ", ")
}

// errorSummary: o tipo e o começo da mensagem de um erro da API, como ela mandou, para o log
// (um 429 de limite da conta e um 429 de requisição recusada só se distinguem pela mensagem).
// A API só recebeu pseudônimos, e a mensagem não passa pela volta: não há dado real nela.
func errorSummary(b []byte) string {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Status  string `json:"status"` // Gemini
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) != nil {
		// o Gemini manda uma lista com um erro só
		var list []json.RawMessage
		if json.Unmarshal(b, &list) != nil || len(list) == 0 || json.Unmarshal(list[0], &e) != nil {
			return ""
		}
	}
	if e.Error.Type == "" {
		e.Error.Type = e.Error.Status
	}
	if e.Error.Type == "" && e.Error.Message == "" {
		return ""
	}
	msg := []rune(strings.Join(strings.Fields(e.Error.Message), " "))
	if len(msg) > 200 {
		msg = append(msg[:200], '…')
	}
	return " | API: " + e.Error.Type + ": " + string(msg)
}

func errSuffix(err error) string {
	if err != nil {
		return " | erro: " + err.Error()
	}
	return ""
}

func (p *Proxy) refuse(w http.ResponseWriter, r *http.Request, reason string) {
	p.log.Printf("RECUSADO %s %s: %s", r.Method, cleaned(r.URL.Path), reason)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{
		"type": "llm_dlp_error", "message": "llm-dlp: requisição bloqueada por segurança (" + reason + "). Nada foi enviado à API."}})
}
