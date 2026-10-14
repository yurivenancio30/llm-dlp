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
	"github.com/yurivenancio30/llm-dlp/internal/versao"
)

// O servidor HTTP: recebe a requisição do Claude Code, manda mascarada para a API e devolve
// a resposta desmascarada.

const Versao = versao.Versao

type Proxy struct {
	cfg     config.Config
	m       *mask.Masker
	midia   *Midia
	vistos  *mask.Vistos
	cliente *http.Client
	chave   []byte // para as provas da porta local (ComChave)
	troca   bool   // a porta é de um supervisor que troca o proxy sem fechá-la (ComTroca)
	log     *log.Logger
	inicio  time.Time
	reqs    atomic.Int64
	// a última requisição de mensagens, já mascarada (só em memória): é o que o comando
	// "llm-dlp last" mostra, para conferir o que a API recebeu
	ultima atomic.Pointer[[]byte]
}

// destino: para onde a requisição vai e em que formato ela vem. "/gemini/..." vai para o
// provedor "gemini" da configuração, sem o "/gemini"; o que não começa com o nome de um provedor
// vai para o upstream (a API da Anthropic).
func (p *Proxy) destino(r *http.Request) (formato, upstream, uri string) {
	uri = r.URL.RequestURI()
	nome, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if pv, ok := p.cfg.Provedores[nome]; ok && nome != "" && pv.Upstream != "" {
		formato = pv.Formato
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			formato = "openai" // o endereço compatível com a OpenAI de qualquer provedor (o do Gemini também)
		case strings.HasSuffix(r.URL.Path, "/responses"):
			formato = "responses" // o Responses da OpenAI (o Codex, com chave ou com a assinatura do ChatGPT)
		case formato == "codeassist":
			if i := strings.LastIndexByte(r.URL.Path, ':'); i >= 0 && metodosDaConta[r.URL.Path[i+1:]] {
				formato = formatoConta
			}
		}
		return formato, pv.Upstream, strings.TrimPrefix(uri, "/"+nome)
	}
	if strings.HasPrefix(r.URL.Path, "/v1/messages") {
		formato = "anthropic"
	}
	return formato, p.cfg.Upstream, uri
}

// semDesmascararPonte: na volta do Gemini e da OpenAI, a ferramenta segue com os pseudônimos
// (vai para a internet): as da configuração e as de mesmo papel nesses clientes.
func (p *Proxy) semDesmascararPonte(nome string) bool {
	return p.cfg.SemDesmascarar(nome) || ferramentasWebPonte[nome]
}

// erroObrigatorio: falha que bloqueia a requisição independentemente de falhar_fechado.
type erroObrigatorio struct{ error }

func Novo(cfg config.Config, m *mask.Masker, vistos *mask.Vistos, logger *log.Logger) *Proxy {
	midia := NovaMidia(cfg, m)
	midia.Aviso = func(msg string) { logger.Printf("AVISO %s", msg) }
	return &Proxy{cfg: cfg, m: m, midia: midia, vistos: vistos, log: logger, inicio: time.Now(),
		cliente: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableCompression: true,
			DialContext:       (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2: true, TLSHandshakeTimeout: 15 * time.Second,
			MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second}}}
}

var hopByHop = map[string]bool{"connection": true, "keep-alive": true, "proxy-connection": true, "te": true,
	"trailer": true, "transfer-encoding": true, "upgrade": true, "host": true, "content-length": true,
	"accept-encoding": true, "content-encoding": true}

// ComChave liga as provas da porta local (ver local.go). Sem chave, a última conversa não é
// servida e o proxy não prova quem é.
func (p *Proxy) ComChave(chave []byte) *Proxy {
	p.chave = chave
	return p
}

// ComTroca: a saúde diz que este proxy está num supervisor que segura a porta e o troca sem
// fechá-la (cmd/llm-dlp/troca.go); é o que o atualizar pergunta antes de pedir a troca.
func (p *Proxy) ComTroca(troca bool) *Proxy {
	p.troca = troca
	return p
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !Local(r) {
		p.log.Printf("RECUSADO %s %s: não veio de um programa local (Host %q, Origin %q)", r.Method, limpo(r.URL.Path),
			limpo(r.Host), limpo(r.Header.Get("Origin")))
		recusarNaoLocal(w)
		return
	}
	if r.URL.Path == "/__llm-dlp/saude" {
		w.Header().Set("content-type", "application/json")
		info := map[string]any{"ok": true, "servico": "llm-dlp", "versao": Versao, "commit": versao.Commit, "pid": os.Getpid(),
			"requisicoes": p.reqs.Load(), "no_ar_desde": p.inicio.Format(time.RFC3339),
			"troca": p.troca}
		if d := r.URL.Query().Get("desafio"); len(p.chave) > 0 && desafioValido(d) {
			info["prova"] = Prova(p.chave, "saude\x00"+d)
		}
		json.NewEncoder(w).Encode(info)
		return
	}
	if r.URL.Path == "/__llm-dlp/ultima" {
		w.Header().Set("content-type", "application/json")
		if !ProvaConfere(p.chave, "ultima", r.Header.Get(CabecalhoProva)) {
			// a conversa (mesmo mascarada) é só de quem lê a chave: use o comando "llm-dlp last"
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"erro":"use o comando: llm-dlp last"}` + "\n"))
			return
		}
		if b := p.ultima.Load(); b != nil {
			w.Write(*b)
		} else {
			w.Write([]byte("{}"))
		}
		return
	}
	p.reqs.Add(1)
	t0 := time.Now()

	corpo, err := io.ReadAll(io.LimitReader(r.Body, 512<<20))
	if err != nil {
		p.recusar(w, r, "não consegui ler a requisição")
		return
	}
	formato, upstream, uri := p.destino(r)
	var entradas []mask.Entrada
	var lote *mask.Lote
	semMascara := false
	if len(bytes.TrimSpace(corpo)) > 0 && formato != formatoConta {
		novo, ents, l, err := p.mascararCorpo(formato, r.Header.Get("content-type"), corpo)
		if err != nil {
			var obrig erroObrigatorio
			if p.cfg.FalharFechado || errors.As(err, &obrig) {
				p.recusar(w, r, err.Error())
				return
			}
			p.log.Printf("AVISO %s %s: enviado sem máscara (%v)", r.Method, limpo(r.URL.Path), err)
			semMascara = true
		} else {
			corpo, entradas, lote = novo, ents, l
			if (formato == "anthropic" || formato == "gemini" || formato == "openai" || formato == "codeassist" || formato == "responses") &&
				!strings.Contains(r.URL.Path, "count_tokens") &&
				!strings.Contains(r.URL.Path, ":countTokens") && len(corpo) > 2000 {
				c := corpo
				p.ultima.Store(&c)
			}
		}
	}
	tab := mask.NovaTabela(entradas)
	tMascara := time.Since(t0)

	alvo := strings.TrimRight(upstream, "/") + uri
	req, err := http.NewRequestWithContext(r.Context(), r.Method, alvo, bytes.NewReader(corpo))
	if err != nil {
		p.recusar(w, r, "url inválida")
		return
	}
	for k, vs := range r.Header {
		if !hopByHop[strings.ToLower(k)] {
			req.Header[k] = vs
		}
	}
	req.Header.Set("accept-encoding", "identity")
	req.ContentLength = int64(len(corpo))

	resp, err := p.cliente.Do(req)
	if err != nil {
		// só a causa: a mensagem inteira traz o endereço, que pode levar a chave da API (?key=...)
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		http.Error(w, "llm-dlp: falha ao contatar a API: "+err.Error(), http.StatusBadGateway)
		p.log.Printf("ERRO %s %s: upstream: %v", r.Method, limpo(r.URL.Path), err)
		return
	}
	if lote != nil {
		// a API recebeu: daqui em diante, estes textos saem sempre iguais (cache)
		lote.Congelar()
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if !hopByHop[strings.ToLower(k)] {
			w.Header()[k] = vs
		}
	}
	w.WriteHeader(resp.StatusCode)

	ct := resp.Header.Get("content-type")
	erroAPI := ""
	switch {
	case strings.HasPrefix(ct, "text/event-stream") && (formato == "gemini" || formato == "codeassist"):
		err = p.desmascararSSEGemini(w, resp.Body, tab)
	case strings.HasPrefix(ct, "text/event-stream") && formato == "responses":
		err = p.desmascararSSEResponses(w, resp.Body, tab)
	case strings.HasPrefix(ct, "text/event-stream") && formato == "openai":
		err = p.desmascararSSEOpenAI(w, resp.Body, tab)
	case strings.HasPrefix(ct, "text/event-stream"):
		err = p.desmascararSSE(w, resp.Body, tab)
	case strings.Contains(ct, "json") && (!tab.Vazia() || resp.StatusCode >= 400):
		var b []byte
		b, err = io.ReadAll(resp.Body)
		if err == nil {
			if resp.StatusCode >= 400 && !semMascara {
				erroAPI = resumoDoErro(b)
			}
			if !tab.Vazia() && (formato == "gemini" || formato == "codeassist") {
				b = p.desmascararGeminiJSON(b, tab)
			} else if !tab.Vazia() && formato == "responses" {
				b = p.desmascararResponsesJSON(b, tab)
			} else if !tab.Vazia() && formato == "openai" {
				b = p.desmascararOpenAIJSON(b, tab)
			} else if !tab.Vazia() {
				b = desmascararJSONResposta(b, tab, p.cfg.SemDesmascarar,
					func(s string) string { return p.m.RegistrarResposta(s, tab) })
			}
			_, err = w.Write(b)
		}
	default:
		_, err = io.Copy(w, resp.Body)
	}
	if p.vistos != nil {
		p.vistos.SalvarSeSujo()
	}
	p.m.Persistir()
	p.log.Printf("%s %s -> %d | %d substituições | %d colisões | mascarar %s | total %s%s%s%s%s%s", r.Method, limpo(r.URL.Path), resp.StatusCode,
		len(entradas), tab.Conflitos, tMascara.Round(time.Millisecond), time.Since(t0).Round(time.Millisecond), porTipo(entradas),
		colididos(tab), naoDesfeitos(tab), erroAPI, errSufixo(err))
}

// porTipo: quantos valores distintos de cada tipo foram mascarados (" | tipos: cpf 2, ip 1,
// tabela 5"), do mais frequente ao menos. Só contagens: nenhum valor vai para o log.
func porTipo(es []mask.Entrada) string {
	vistos, n := map[string]bool{}, map[string]int{}
	for _, e := range es {
		t := strings.TrimPrefix(e.Tipo, "obj.")
		if k := t + "\x00" + strings.ToLower(e.Real); !vistos[k] {
			vistos[k] = true
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

// naoDesfeitos: os pseudônimos nossos que sobraram na resposta entregue ao cliente (o cliente,
// e o que ele gravar em arquivo, ficaram com o pseudônimo no lugar do valor real). Pseudônimo
// não é dado real: vai inteiro para o log, para dar para procurar onde ficou.
func naoDesfeitos(tab *mask.Tabela) string {
	nd := tab.NaoDesfeitos()
	if len(nd) == 0 {
		return ""
	}
	return fmt.Sprintf(" | NÃO DESFEITOS %d: %s", len(nd), strings.Join(nd, ", "))
}

// colididos: os pseudônimos que colidiram (" | colididos: CPF-x, 240.1.2.x"): a forma de cada um
// diz o tipo do dado que ficou sem desmascarar.
func colididos(tab *mask.Tabela) string {
	c := tab.Colididos()
	if len(c) == 0 {
		return ""
	}
	return " | colididos: " + strings.Join(c, ", ")
}

// resumoDoErro: o tipo e o começo da mensagem de um erro da API, como ela mandou, para o log
// (um 429 de limite da conta e um 429 de requisição recusada só se distinguem pela mensagem).
// A API só recebeu pseudônimos, e a mensagem não passa pela volta: não há dado real nela.
func resumoDoErro(b []byte) string {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Status  string `json:"status"` // Gemini
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) != nil {
		// o Gemini manda uma lista com um erro só
		var lista []json.RawMessage
		if json.Unmarshal(b, &lista) != nil || len(lista) == 0 || json.Unmarshal(lista[0], &e) != nil {
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

func errSufixo(err error) string {
	if err != nil {
		return " | erro: " + err.Error()
	}
	return ""
}

func (p *Proxy) recusar(w http.ResponseWriter, r *http.Request, motivo string) {
	p.log.Printf("RECUSADO %s %s: %s", r.Method, limpo(r.URL.Path), motivo)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{
		"type": "llm_dlp_error", "message": "llm-dlp: requisição bloqueada por segurança (" + motivo + "). Nada foi enviado à API."}})
}
