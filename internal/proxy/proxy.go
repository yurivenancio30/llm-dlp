// Package proxy fica entre o cliente (Claude Code) e a API da LLM: mascara tudo que
// sai e desmascara tudo que volta. Do cliente para o usuário, tudo é real; só o
// trecho até a API é mascarado.
package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
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
	log     *log.Logger
	inicio  time.Time
	reqs    atomic.Int64
	// a última requisição de mensagens, já mascarada (só em memória): é o que o comando
	// "llm-dlp ultima" mostra, para conferir o que a API recebeu
	ultima atomic.Pointer[[]byte]
}

// erroObrigatorio: falha que bloqueia a requisição independentemente de falhar_fechado.
type erroObrigatorio struct{ error }

func Novo(cfg config.Config, m *mask.Masker, vistos *mask.Vistos, logger *log.Logger) *Proxy {
	return &Proxy{cfg: cfg, m: m, midia: NovaMidia(cfg, m), vistos: vistos, log: logger, inicio: time.Now(),
		cliente: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableCompression: true,
			DialContext:       (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2: true, TLSHandshakeTimeout: 15 * time.Second,
			MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second}}}
}

var hopByHop = map[string]bool{"connection": true, "keep-alive": true, "proxy-connection": true, "te": true,
	"trailer": true, "transfer-encoding": true, "upgrade": true, "host": true, "content-length": true,
	"accept-encoding": true, "content-encoding": true}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__llm-dlp/saude" {
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "versao": Versao, "commit": versao.Commit, "pid": os.Getpid(),
			"requisicoes": p.reqs.Load(), "no_ar_desde": p.inicio.Format(time.RFC3339)})
		return
	}
	if r.URL.Path == "/__llm-dlp/ultima" {
		w.Header().Set("content-type", "application/json")
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
	var entradas []mask.Entrada
	var lote *mask.Lote
	if len(bytes.TrimSpace(corpo)) > 0 {
		novo, ents, l, err := p.mascararCorpo(r, corpo)
		if err != nil {
			var obrig erroObrigatorio
			if p.cfg.FalharFechado || errors.As(err, &obrig) {
				p.recusar(w, r, err.Error())
				return
			}
			p.log.Printf("AVISO %s %s: enviado sem máscara (%v)", r.Method, r.URL.Path, err)
		} else {
			corpo, entradas, lote = novo, ents, l
			if strings.HasPrefix(r.URL.Path, "/v1/messages") && !strings.Contains(r.URL.Path, "count_tokens") && len(corpo) > 2000 {
				c := corpo
				p.ultima.Store(&c)
			}
		}
	}
	tab := mask.NovaTabela(entradas)
	tMascara := time.Since(t0)

	alvo := strings.TrimRight(p.cfg.Upstream, "/") + r.URL.RequestURI()
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
		http.Error(w, "llm-dlp: falha ao contatar a API: "+err.Error(), http.StatusBadGateway)
		p.log.Printf("ERRO %s %s: upstream: %v", r.Method, r.URL.Path, err)
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
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		err = p.desmascararSSE(w, resp.Body, tab)
	case strings.Contains(ct, "json") && !tab.Vazia():
		var b []byte
		b, err = io.ReadAll(resp.Body)
		if err == nil {
			_, err = w.Write(desmascararJSONResposta(b, tab, p.cfg.SemDesmascarar))
		}
	default:
		_, err = io.Copy(w, resp.Body)
	}
	if p.vistos != nil {
		p.vistos.SalvarSeSujo()
	}
	p.m.Persistir()
	p.log.Printf("%s %s -> %d | %d substituições | %d colisões | mascarar %s | total %s%s", r.Method, r.URL.Path, resp.StatusCode,
		len(entradas), tab.Conflitos, tMascara.Round(time.Millisecond), time.Since(t0).Round(time.Millisecond), errSufixo(err))
}

func errSufixo(err error) string {
	if err != nil {
		return " | erro: " + err.Error()
	}
	return ""
}

func (p *Proxy) recusar(w http.ResponseWriter, r *http.Request, motivo string) {
	p.log.Printf("RECUSADO %s %s: %s", r.Method, r.URL.Path, motivo)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{
		"type": "llm_dlp_error", "message": "llm-dlp: requisição bloqueada por segurança (" + motivo + "). Nada foi enviado à API."}})
}
